package shared

import (
	"errors"
	"fmt"
	"strings"

	"github.com/cli/cli/v2/api"
	"github.com/cli/cli/v2/internal/gh"
	"github.com/cli/cli/v2/internal/ghrepo"
	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/cli/cli/v2/pkg/surveyext"
)

type Action int

const (
	SubmitAction Action = iota
	PreviewAction
	CancelAction
	MetadataAction
	EditCommitMessageAction
	EditCommitSubjectAction
	SubmitDraftAction

	noMilestone = "(none)"

	submitLabel      = "Submit"
	submitDraftLabel = "Submit as draft"
	previewLabel     = "Continue in browser"
	metadataLabel    = "Add metadata"
	cancelLabel      = "Cancel"
)

type Prompt interface {
	Input(string, string) (string, error)
	Select(string, string, []string) (int, error)
	MarkdownEditor(string, string, bool) (string, error)
	Confirm(string, bool) (bool, error)
	MultiSelect(string, []string, []string) ([]int, error)
}

func ConfirmIssueSubmission(p Prompt, allowPreview bool, allowMetadata bool) (Action, error) {
	return confirmSubmission(p, allowPreview, allowMetadata, false, false)
}

func ConfirmPRSubmission(p Prompt, allowPreview, allowMetadata, isDraft bool) (Action, error) {
	return confirmSubmission(p, allowPreview, allowMetadata, true, isDraft)
}

func confirmSubmission(p Prompt, allowPreview, allowMetadata, allowDraft, isDraft bool) (Action, error) {
	var options []string
	if !isDraft {
		options = append(options, submitLabel)
	}
	if allowDraft {
		options = append(options, submitDraftLabel)
	}
	if allowPreview {
		options = append(options, previewLabel)
	}
	if allowMetadata {
		options = append(options, metadataLabel)
	}
	options = append(options, cancelLabel)

	result, err := p.Select("What's next?", "", options)
	if err != nil {
		return -1, fmt.Errorf("could not prompt: %w", err)
	}

	switch options[result] {
	case submitLabel:
		return SubmitAction, nil
	case submitDraftLabel:
		return SubmitDraftAction, nil
	case previewLabel:
		return PreviewAction, nil
	case metadataLabel:
		return MetadataAction, nil
	case cancelLabel:
		return CancelAction, nil
	default:
		return -1, fmt.Errorf("invalid index: %d", result)
	}
}

func BodySurvey(p Prompt, state *IssueMetadataState, templateContent string) error {
	if templateContent != "" {
		if state.Body != "" {
			// prevent excessive newlines between default body and template
			state.Body = strings.TrimRight(state.Body, "\n")
			state.Body += "\n\n"
		}
		state.Body += templateContent
	}

	result, err := p.MarkdownEditor("Body", state.Body, true)
	if err != nil {
		return err
	}

	if state.Body != result {
		state.MarkDirty()
	}

	state.Body = result

	return nil
}

func TitleSurvey(p Prompt, io *iostreams.IOStreams, state *IssueMetadataState) error {
	var err error
	result := ""
	for result == "" {
		result, err = p.Input("Title (required)", state.Title)
		if err != nil {
			return err
		}
		if result == "" {
			fmt.Fprintf(io.ErrOut, "%s Title cannot be blank\n", io.ColorScheme().FailureIcon())
		}
	}

	if result != state.Title {
		state.MarkDirty()
	}

	state.Title = result

	return nil
}

type MetadataFetcher struct {
	IO        *iostreams.IOStreams
	APIClient *api.Client
	Repo      ghrepo.Interface
	State     *IssueMetadataState
}

func (mf *MetadataFetcher) RepoMetadataFetch(input api.RepoMetadataInput) (*api.RepoMetadataResult, error) {
	mf.IO.StartProgressIndicator()
	metadataResult, err := api.RepoMetadata(mf.APIClient, mf.Repo, input)
	mf.IO.StopProgressIndicator()
	mf.State.MetadataResult = metadataResult
	return metadataResult, err
}

type RepoMetadataFetcher interface {
	RepoMetadataFetch(api.RepoMetadataInput) (*api.RepoMetadataResult, error)
}

func MetadataSurvey(p Prompt, io *iostreams.IOStreams, baseRepo ghrepo.Interface, fetcher RepoMetadataFetcher, state *IssueMetadataState, projectsV1Support gh.ProjectsV1Support) error {
	isChosen := func(m string) bool {
		for _, c := range state.Metadata {
			if m == c {
				return true
			}
		}
		return false
	}

	allowReviewers := state.Type == PRMetadata

	extraFieldsOptions := []string{}
	if allowReviewers {
		extraFieldsOptions = append(extraFieldsOptions, "Reviewers")
	}
	extraFieldsOptions = append(extraFieldsOptions, "Assignees", "Labels", "Projects", "Milestone")

	selected, err := p.MultiSelect("What would you like to add?", nil, extraFieldsOptions)
	if err != nil {
		return err
	}
	for _, i := range selected {
		state.Metadata = append(state.Metadata, extraFieldsOptions[i])
	}

	// Retrieve and process data for survey prompts based on the extra fields selected
	// We deliberately skip fetching full assignable users/actors when interactively selecting
	// Assignees or Reviewers. We only need the current login for reviewers and other metadata.
	metadataInput := api.RepoMetadataInput{
		Reviewers:      isChosen("Reviewers"),
		TeamReviewers:  false, // teams omitted in POC
		Assignees:      false,
		ActorAssignees: false,
		Labels:         isChosen("Labels"),
		ProjectsV1:     isChosen("Projects") && projectsV1Support == gh.ProjectsV1Supported,
		ProjectsV2:     isChosen("Projects"),
		Milestones:     isChosen("Milestone"),
	}
	metadataResult, err := fetcher.RepoMetadataFetch(metadataInput)
	if err != nil {
		return fmt.Errorf("error fetching metadata options: %w", err)
	}

	// Reviewer & Assignee interactive lists will be built via suggestedActors API later.
	var labels []string
	for _, l := range metadataResult.Labels {
		labels = append(labels, l.Name)
	}
	var projects []string
	for _, p := range metadataResult.Projects {
		projects = append(projects, p.Name)
	}
	for _, p := range metadataResult.ProjectsV2 {
		projects = append(projects, p.Title)
	}
	milestones := []string{noMilestone}
	for _, m := range metadataResult.Milestones {
		milestones = append(milestones, m.Title)
	}

	// Prompt user for additional metadata based on selected fields
	values := struct {
		Reviewers []string
		Assignees []string
		Labels    []string
		Projects  []string
		Milestone string
	}{}

	if isChosen("Reviewers") {
		mf, _ := fetcher.(*MetadataFetcher)
		// assignableID unavailable in create flow; pass empty string
		selectedLogins, actorMap, err := InteractiveSuggestedActorsSelection(p, mf.APIClient, baseRepo, "", metadataResult.CurrentLogin, state.Reviewers, "Reviewers")
		if err != nil {
			return err
		}
		values.Reviewers = selectedLogins
		EnsureMetadataActors(metadataResult, actorMap)
	}
	if isChosen("Assignees") {
		mf, _ := fetcher.(*MetadataFetcher)
		selectedLogins, actorMap, err := InteractiveSuggestedActorsSelection(p, mf.APIClient, baseRepo, "", "", state.Assignees, "Assignees")
		if err != nil {
			return err
		}
		values.Assignees = selectedLogins
		EnsureMetadataActors(metadataResult, actorMap)
	}
	if isChosen("Labels") {
		if len(labels) > 0 {
			selected, err := p.MultiSelect("Labels", state.Labels, labels)
			if err != nil {
				return err
			}
			for _, i := range selected {
				values.Labels = append(values.Labels, labels[i])
			}
		} else {
			fmt.Fprintln(io.ErrOut, "warning: no labels in the repository")
		}
	}
	if isChosen("Projects") {
		if len(projects) > 0 {
			selected, err := p.MultiSelect("Projects", state.ProjectTitles, projects)
			if err != nil {
				return err
			}
			for _, i := range selected {
				values.Projects = append(values.Projects, projects[i])
			}
		} else {
			fmt.Fprintln(io.ErrOut, "warning: no projects to choose from")
		}
	}
	if isChosen("Milestone") {
		if len(milestones) > 1 {
			var milestoneDefault string
			if len(state.Milestones) > 0 {
				milestoneDefault = state.Milestones[0]
			} else {
				milestoneDefault = milestones[1]
			}
			selected, err := p.Select("Milestone", milestoneDefault, milestones)
			if err != nil {
				return err
			}
			values.Milestone = milestones[selected]
		} else {
			fmt.Fprintln(io.ErrOut, "warning: no milestones in the repository")
		}
	}

	// Update issue / pull request metadata state
	if isChosen("Reviewers") {
		state.Reviewers = values.Reviewers
	}
	if isChosen("Assignees") {
		state.Assignees = values.Assignees
	}
	if isChosen("Labels") {
		state.Labels = values.Labels
	}
	if isChosen("Projects") {
		state.ProjectTitles = values.Projects
	}
	if isChosen("Milestone") {
		if values.Milestone != "" && values.Milestone != noMilestone {
			state.Milestones = []string{values.Milestone}
		} else {
			state.Milestones = []string{}
		}
	}

	return nil
}

// InteractiveSuggestedActorsSelection performs the multi-select loop using the suggestedActors API.
// Returns selected logins and a map of login->AssignableActor for later ID resolution.
// excludeLogin is skipped (e.g. current user when selecting reviewers).
// If assignableID is non-empty, suggestions are fetched for that specific Issue/PR node; otherwise
// an empty suggestion set is returned since the object does not yet exist (create flows).
// Exported for reuse in edit flows.
func InteractiveSuggestedActorsSelection(p Prompt, client *api.Client, repo ghrepo.Interface, assignableID string, excludeLogin string, initial []string, label string) ([]string, map[string]api.AssignableActor, error) {
	chosen := make([]string, 0, len(initial))
	chosenSet := make(map[string]struct{})
	for _, l := range initial {
		if excludeLogin != "" && strings.EqualFold(l, excludeLogin) {
			continue
		}
		if _, ok := chosenSet[l]; !ok {
			chosen = append(chosen, l)
			chosenSet[l] = struct{}{}
		}
	}
	actorMap := make(map[string]api.AssignableActor)

	fetch := func(q string) ([]api.AssignableActor, error) {
		var actors []api.AssignableActor
		var err error
		if assignableID == "" {
			actors, err = api.SuggestActorsForRepository(client, repo, q)
		} else {
			actors, err = api.SuggestActorsForAssignable(client, repo, assignableID, q)
		}
		if err != nil {
			return nil, err
		}
		for _, a := range actors {
			if _, exists := actorMap[a.Login()]; !exists {
				actorMap[a.Login()] = a
			}
		}
		return actors, nil
	}

	// initial fetch
	suggestions, err := fetch("")
	if err != nil {
		return nil, nil, err
	}

	for {
		// Build options: chosen first, then suggestions (excluding chosen), then "Search"
		var opts []string
		var defaults []string
		for _, login := range chosen {
			if a, ok := actorMap[login]; ok {
				opts = append(opts, a.DisplayName())
				defaults = append(defaults, a.DisplayName())
			} else {
				opts = append(opts, login)
				defaults = append(defaults, login)
			}
		}
		for _, a := range suggestions {
			l := a.Login()
			if _, exists := chosenSet[l]; exists {
				continue
			}
			opts = append(opts, a.DisplayName())
		}
		opts = append(opts, "Search")

		selectedIdxs, err := p.MultiSelect(label, defaults, opts)
		if err != nil {
			return nil, nil, err
		}

		pickedSearch := false
		newChosenSet := make(map[string]struct{})
		var newChosenOrdered []string
		for _, idx := range selectedIdxs {
			if idx < 0 || idx >= len(opts) {
				continue
			}
			val := opts[idx]
			if val == "Search" {
				pickedSearch = true
				continue
			}
			login := strings.Split(val, " ")[0]
			if excludeLogin != "" && strings.EqualFold(login, excludeLogin) {
				continue
			}
			if _, exists := newChosenSet[login]; !exists {
				newChosenSet[login] = struct{}{}
				newChosenOrdered = append(newChosenOrdered, login)
			}
			if _, ok := actorMap[login]; !ok {
				// create synthetic user actor if not present (ID empty)
				actorMap[login] = api.NewAssignableUser("", login, "")
			}
		}
		chosenSet = newChosenSet
		chosen = newChosenOrdered

		if !pickedSearch {
			break
		}

		// Prompt for search query; prevent blank input
		var q string
		for q == "" {
			var inpErr error
			q, inpErr = p.Input("Search", "")
			if inpErr != nil {
				return nil, nil, inpErr
			}
		}
		suggestions, err = fetch(q)
		if err != nil {
			return nil, nil, err
		}
	}

	return chosen, actorMap, nil
}

// ensureMetadataActors merges actors gathered interactively into metadataResult so that
// downstream ID resolution (MembersToIDs) works.
func EnsureMetadataActors(metadataResult *api.RepoMetadataResult, actors map[string]api.AssignableActor) {
	for _, a := range actors {
		// Add to AssignableActors if not present
		foundActor := false
		for _, existing := range metadataResult.AssignableActors {
			if existing.Login() == a.Login() {
				foundActor = true
				break
			}
		}
		if !foundActor {
			metadataResult.AssignableActors = append(metadataResult.AssignableActors, a)
		}
		// If user, also add to AssignableUsers for MembersToIDs
		if u, ok := a.(api.AssignableUser); ok {
			foundUser := false
			for _, existing := range metadataResult.AssignableUsers {
				if existing.Login() == u.Login() {
					foundUser = true
					break
				}
			}
			if !foundUser {
				metadataResult.AssignableUsers = append(metadataResult.AssignableUsers, u)
			}
		}
	}
}

type Editor interface {
	Edit(filename, initialValue string) (string, error)
}

type UserEditor struct {
	IO     *iostreams.IOStreams
	Config func() (gh.Config, error)
}

func (e *UserEditor) Edit(filename, initialValue string) (string, error) {
	editorCommand, err := cmdutil.DetermineEditor(e.Config)
	if err != nil {
		return "", err
	}
	return surveyext.Edit(editorCommand, filename, initialValue, e.IO.In, e.IO.Out, e.IO.ErrOut)
}

const editorHintMarker = "------------------------ >8 ------------------------"
const editorHint = `
Please Enter the title on the first line and the body on subsequent lines.
Lines below dotted lines will be ignored, and an empty title aborts the creation process.`

func TitledEditSurvey(editor Editor) func(string, string) (string, string, error) {
	return func(initialTitle, initialBody string) (string, string, error) {
		initialValue := strings.Join([]string{initialTitle, initialBody, editorHintMarker, editorHint}, "\n")
		titleAndBody, err := editor.Edit("*.md", initialValue)
		if err != nil {
			return "", "", err
		}

		titleAndBody = strings.ReplaceAll(titleAndBody, "\r\n", "\n")
		titleAndBody, _, _ = strings.Cut(titleAndBody, editorHintMarker)
		title, body, _ := strings.Cut(titleAndBody, "\n")
		return title, strings.TrimSuffix(body, "\n"), nil
	}
}

func InitEditorMode(f *cmdutil.Factory, editorMode bool, webMode bool, canPrompt bool) (bool, error) {
	if err := cmdutil.MutuallyExclusive(
		"specify only one of `--editor` or `--web`",
		editorMode,
		webMode,
	); err != nil {
		return false, err
	}

	config, err := f.Config()
	if err != nil {
		return false, err
	}

	editorMode = !webMode && (editorMode || config.PreferEditorPrompt("").Value == "enabled")

	if editorMode && !canPrompt {
		return false, errors.New("--editor or enabled prefer_editor_prompt configuration are not supported in non-tty mode")
	}

	return editorMode, nil
}
