package review

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/MakeNowJust/heredoc"
	"github.com/cli/cli/v2/api"
	"github.com/cli/cli/v2/internal/ghrepo"
	prshared "github.com/cli/cli/v2/pkg/cmd/pr/shared"
	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/spf13/cobra"
)

type openOptions struct {
	HttpClient func() (*http.Client, error)
	IO         *iostreams.IOStreams
	Finder     prshared.PRFinder

	Selector string
	Body     string
	CommitID string
	Exporter cmdutil.Exporter
}

func NewCmdReviewOpen(f *cmdutil.Factory, runF func(*openOptions) error) *cobra.Command {
	opts := &openOptions{
		IO:         f.IOStreams,
		HttpClient: f.HttpClient,
	}

	var bodyFile string

	cmd := &cobra.Command{
		Use:   "open [<number> | <url> | <branch>]",
		Short: "Open a pending review on a pull request",
		Long: heredoc.Doc(`
			Create a pending pull request review that can be populated with inline comments
			prior to submission.
		`),
		Example: heredoc.Doc(`
			# Open a pending review for PR 42 with an initial note
			$ gh pr review open 42 --body "Initial notes"

			# Open a pending review pinned to a specific commit
			$ gh pr review open --commit-sha abc123def
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Finder = prshared.NewFinder(f)

			if repoOverride, _ := cmd.Flags().GetString("repo"); repoOverride != "" && len(args) == 0 {
				return cmdutil.FlagErrorf("argument required when using the --repo flag")
			}

			if len(args) > 0 {
				opts.Selector = args[0]
			}

			bodyProvided := cmd.Flags().Changed("body")
			bodyFileProvided := bodyFile != ""
			if err := cmdutil.MutuallyExclusive("specify only one of `--body` or `--body-file`", bodyProvided, bodyFileProvided); err != nil {
				return err
			}
			if bodyFileProvided {
				content, err := cmdutil.ReadFile(bodyFile, opts.IO.In)
				if err != nil {
					return err
				}
				opts.Body = string(content)
			}

			if runF != nil {
				return runF(opts)
			}
			return openRun(opts)
		},
	}

	cmd.Flags().StringVarP(&opts.Body, "body", "b", "", "Optional body for the pending review")
	cmd.Flags().StringVarP(&bodyFile, "body-file", "F", "", "Read body text from `file` (use \"-\" for standard input)")
	cmd.Flags().StringVar(&opts.CommitID, "commit-sha", "", "Commit SHA to associate with the pending review")
	cmdutil.AddJSONFlags(cmd, &opts.Exporter, prshared.ReviewFields)

	return cmd
}

func openRun(opts *openOptions) error {
	pr, repo, err := findPullRequest(opts.Finder, opts.Selector)
	if err != nil {
		return err
	}

	httpClient, err := opts.HttpClient()
	if err != nil {
		return err
	}
	client := api.NewClientFromHTTP(httpClient)

	review, err := api.CreatePendingReviewREST(client, repo, pr.Number, api.PendingReviewInput{Body: opts.Body, CommitID: opts.CommitID})
	if err != nil {
		return err
	}

	output := prshared.NewReviewOutput(*review)

	if opts.Exporter != nil {
		return opts.Exporter.Write(opts.IO, output)
	}

	return writeJSON(opts.IO, output)
}

type addCommentOptions struct {
	HttpClient func() (*http.Client, error)
	IO         *iostreams.IOStreams
	Finder     prshared.PRFinder

	Selector      string
	ReviewID      int64
	CommentInputs []string
	CommentsFile  string
	Exporter      cmdutil.Exporter
}

type commentInput struct {
	Path      string  `json:"path"`
	Body      string  `json:"body"`
	Position  *int    `json:"position,omitempty"`
	Line      *int    `json:"line,omitempty"`
	Side      *string `json:"side,omitempty"`
	StartLine *int    `json:"start_line,omitempty"`
	StartSide *string `json:"start_side,omitempty"`
	CommitID  *string `json:"commit_id,omitempty"`
}

type resolvedComment struct {
	path     string
	position int
	body     string
	commitID *string
}

func NewCmdReviewAddComment(f *cmdutil.Factory, runF func(*addCommentOptions) error) *cobra.Command {
	opts := &addCommentOptions{
		IO:         f.IOStreams,
		HttpClient: f.HttpClient,
	}

	cmd := &cobra.Command{
		Use:   "add-comment [<number> | <url> | <branch>]",
		Short: "Add inline comments to a pending review",
		Long: heredoc.Doc(`
			Attach one or more inline comments to an existing pending review. Comments are
			provided as JSON objects via repeated --add-comment flags or a JSON file.

		Each comment JSON must provide "path" and "body", plus either a diff
		"position" integer or a combination of "line" and "side". When using
		"line"/"side", the CLI maps the requested line to the correct diff position
		for the pull request patch.
		`),
		Example: heredoc.Doc(`
			# Add a single position-based comment
			$ gh pr review add-comment 42 --review-id 123 --add-comment '{"path":"src/app.go","position":5,"body":"nit"}'

			# Add comments from a JSON file
			$ gh pr review add-comment --review-id 456 --comments-file comments.json
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Finder = prshared.NewFinder(f)

			if repoOverride, _ := cmd.Flags().GetString("repo"); repoOverride != "" && len(args) == 0 {
				return cmdutil.FlagErrorf("argument required when using the --repo flag")
			}

			if len(args) > 0 {
				opts.Selector = args[0]
			}

			if opts.ReviewID <= 0 {
				return cmdutil.FlagErrorf("--review-id must be provided")
			}

			if len(opts.CommentInputs) == 0 && opts.CommentsFile == "" {
				return cmdutil.FlagErrorf("provide at least one --add-comment or --comments-file")
			}

			if runF != nil {
				return runF(opts)
			}
			return addCommentRun(opts)
		},
	}

	cmd.Flags().Int64Var(&opts.ReviewID, "review-id", 0, "Pending review ID")
	cmd.Flags().StringArrayVar(&opts.CommentInputs, "add-comment", nil, "Review comment JSON (requires \"path\" and \"body\" plus \"position\" or \"line\"/\"side\")")
	cmd.Flags().StringVar(&opts.CommentsFile, "comments-file", "", "Path to JSON file containing review comments")
	cmdutil.AddJSONFlags(cmd, &opts.Exporter, prshared.ReviewCommentFields)

	return cmd
}

func addCommentRun(opts *addCommentOptions) error {
	pr, repo, err := findPullRequest(opts.Finder, opts.Selector)
	if err != nil {
		return err
	}

	httpClient, err := opts.HttpClient()
	if err != nil {
		return err
	}
	client := api.NewClientFromHTTP(httpClient)

	rawInputs, err := collectPendingCommentInputs(opts)
	if err != nil {
		return err
	}

	normalizedInputs := make([]commentInput, len(rawInputs))
	for i, input := range rawInputs {
		normalized, err := normalizePendingCommentInput(input)
		if err != nil {
			return fmt.Errorf("comment %d: %w", i+1, err)
		}
		normalizedInputs[i] = normalized
	}

	review, err := api.GetPullRequestReviewREST(client, repo, pr.Number, opts.ReviewID)
	if err != nil {
		return err
	}
	if !strings.EqualFold(review.State, "PENDING") {
		return fmt.Errorf("review %d is %s; add-comment requires a pending review", opts.ReviewID, strings.ToLower(review.State))
	}

	files, err := api.ListPullRequestFilesREST(client, repo, pr.Number)
	if err != nil {
		return err
	}

	fileLookup := make(map[string]api.PullRequestFileREST, len(files))
	for _, file := range files {
		fileLookup[file.Filename] = file
	}

	var reviewCommit *string
	if review.CommitID != "" {
		commit := review.CommitID
		reviewCommit = &commit
	}

	diffCache := make(map[string]diffPositionIndex)
	outputs := make([]prshared.ReviewCommentOutput, 0, len(normalizedInputs))

	for idx, input := range normalizedInputs {
		file, ok := fileLookup[input.Path]
		if !ok {
			return fmt.Errorf("comment %d: path %q is not part of the changes for PR #%d", idx+1, input.Path, pr.Number)
		}

		comment := resolvedComment{
			path: input.Path,
			body: input.Body,
		}

		if input.CommitID != nil {
			if reviewCommit != nil && *input.CommitID != *reviewCommit {
				return fmt.Errorf("comment %d: commit_id %q does not match pending review commit %q", idx+1, *input.CommitID, *reviewCommit)
			}
			commitCopy := *input.CommitID
			comment.commitID = &commitCopy
		} else {
			comment.commitID = reviewCommit
		}

		if input.Position != nil {
			comment.position = *input.Position
		} else {
			if file.Patch == nil || *file.Patch == "" {
				return fmt.Errorf("comment %d: file %q has no diff available; provide --position explicitly", idx+1, input.Path)
			}

			index, ok := diffCache[input.Path]
			if !ok {
				parsed, err := buildDiffPositionIndex(*file.Patch)
				if err != nil {
					return fmt.Errorf("comment %d: could not parse diff for %q: %w", idx+1, input.Path, err)
				}
				diffCache[input.Path] = parsed
				index = parsed
			}

			line := *input.Line
			side := *input.Side
			var lookup map[int]int
			if side == "RIGHT" {
				lookup = index.right
			} else {
				lookup = index.left
			}

			position, ok := lookup[line]
			if !ok {
				commitLabel := "<unknown>"
				if comment.commitID != nil && *comment.commitID != "" {
					commitLabel = *comment.commitID
				}
				return fmt.Errorf("comment %d: line %d on %s is outside the diff at commit %s; choose a changed line or provide --position", idx+1, line, input.Path, commitLabel)
			}
			comment.position = position
		}

		created, err := api.AddPendingReviewCommentREST(client, repo, pr.Number, opts.ReviewID, comment.path, comment.position, comment.body, comment.commitID)
		if err != nil {
			var httpErr api.HTTPError
			if errors.As(err, &httpErr) && (httpErr.StatusCode == 404 || httpErr.StatusCode == 422) {
				return fmt.Errorf("comment %d: API %d error when posting %s at diff position %d; confirm the path and diff line are valid: %w", idx+1, httpErr.StatusCode, comment.path, comment.position, err)
			}
			return err
		}

		outputs = append(outputs, prshared.NewReviewCommentOutput(*created))
	}

	if opts.Exporter != nil {
		return opts.Exporter.Write(opts.IO, outputs)
	}

	return writeJSON(opts.IO, outputs)
}

func collectPendingCommentInputs(opts *addCommentOptions) ([]commentInput, error) {
	var inputs []commentInput

	for _, raw := range opts.CommentInputs {
		var input commentInput
		if err := json.Unmarshal([]byte(raw), &input); err != nil {
			return nil, fmt.Errorf("invalid comment JSON: %w", err)
		}
		inputs = append(inputs, input)
	}

	if opts.CommentsFile != "" {
		content, err := cmdutil.ReadFile(opts.CommentsFile, opts.IO.In)
		if err != nil {
			return nil, err
		}

		payload := strings.TrimSpace(string(content))
		if payload == "" {
			return nil, cmdutil.FlagErrorf("comments file is empty")
		}

		if strings.HasPrefix(payload, "[") {
			var fileInputs []commentInput
			if err := json.Unmarshal([]byte(payload), &fileInputs); err != nil {
				return nil, fmt.Errorf("invalid comments file: %w", err)
			}
			inputs = append(inputs, fileInputs...)
		} else {
			var single commentInput
			if err := json.Unmarshal([]byte(payload), &single); err != nil {
				return nil, fmt.Errorf("invalid comments file: %w", err)
			}
			inputs = append(inputs, single)
		}
	}

	return inputs, nil
}

func normalizePendingCommentInput(input commentInput) (commentInput, error) {
	normalized := commentInput{}

	pathTrimmed := strings.TrimSpace(input.Path)
	if pathTrimmed == "" {
		return commentInput{}, cmdutil.FlagErrorf("comment path is required")
	}
	if pathTrimmed != input.Path {
		return commentInput{}, cmdutil.FlagErrorf("comment path cannot include leading or trailing whitespace")
	}
	normalized.Path = pathTrimmed

	if strings.TrimSpace(input.Body) == "" {
		return commentInput{}, cmdutil.FlagErrorf("comment body cannot be blank")
	}
	normalized.Body = input.Body

	if input.StartLine != nil || input.StartSide != nil {
		return commentInput{}, cmdutil.FlagErrorf("line ranges are not supported; provide --position instead")
	}

	if input.Position == nil && input.Line == nil {
		return commentInput{}, cmdutil.FlagErrorf("specify either `position` or `line` with `side`")
	}
	if input.Position != nil && input.Line != nil {
		return commentInput{}, cmdutil.FlagErrorf("`position` cannot be combined with `line`")
	}

	if input.Position != nil {
		if *input.Position <= 0 {
			return commentInput{}, cmdutil.FlagErrorf("`position` must be greater than 0")
		}
		pos := *input.Position
		normalized.Position = &pos
	}

	if input.Line != nil {
		if *input.Line <= 0 {
			return commentInput{}, cmdutil.FlagErrorf("`line` must be greater than 0")
		}
		if input.Side == nil {
			return commentInput{}, cmdutil.FlagErrorf("`side` is required when `line` is provided")
		}
		normalizedSide, err := normalizeReviewSide("side", input.Side)
		if err != nil {
			return commentInput{}, err
		}
		line := *input.Line
		normalized.Line = &line
		normalized.Side = normalizedSide
	} else if input.Side != nil {
		return commentInput{}, cmdutil.FlagErrorf("`side` requires `line`")
	}

	if input.CommitID != nil {
		trimmed := strings.TrimSpace(*input.CommitID)
		if trimmed == "" {
			return commentInput{}, cmdutil.FlagErrorf("`commit_id` cannot be blank")
		}
		commitID := trimmed
		normalized.CommitID = &commitID
	}

	return normalized, nil
}

func normalizeReviewSide(label string, value *string) (*string, error) {
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil, cmdutil.FlagErrorf("`%s` cannot be blank", label)
	}
	if trimmed != *value {
		return nil, cmdutil.FlagErrorf("`%s` cannot include leading or trailing whitespace", label)
	}
	upper := strings.ToUpper(trimmed)
	if upper != "LEFT" && upper != "RIGHT" {
		return nil, cmdutil.FlagErrorf("`%s` must be LEFT or RIGHT", label)
	}
	return &upper, nil
}

type diffPositionIndex struct {
	right map[int]int
	left  map[int]int
}

func buildDiffPositionIndex(patch string) (diffPositionIndex, error) {
	index := diffPositionIndex{
		right: make(map[int]int),
		left:  make(map[int]int),
	}

	lines := strings.Split(patch, "\n")
	var leftLine, rightLine int
	position := 0

	for _, line := range lines {
		if strings.HasPrefix(line, "@@") {
			lStart, rStart, err := parseHunkHeader(line)
			if err != nil {
				return diffPositionIndex{}, err
			}
			leftLine = lStart - 1
			rightLine = rStart - 1
			continue
		}
		if line == "" || strings.HasPrefix(line, "diff ") || strings.HasPrefix(line, "index ") || strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") {
			continue
		}
		if strings.HasPrefix(line, "\\") {
			continue
		}

		position++
		switch line[0] {
		case '+':
			rightLine++
			index.right[rightLine] = position
		case '-':
			leftLine++
			index.left[leftLine] = position
		default:
			leftLine++
			rightLine++
			index.left[leftLine] = position
			index.right[rightLine] = position
		}
	}

	return index, nil
}

func parseHunkHeader(header string) (int, int, error) {
	trimmed := strings.TrimSpace(header)
	if !strings.HasPrefix(trimmed, "@@") {
		return 0, 0, fmt.Errorf("invalid hunk header %q", header)
	}
	trimmed = strings.TrimPrefix(trimmed, "@@")
	trimmed = strings.TrimSuffix(trimmed, "@@")
	trimmed = strings.TrimSpace(trimmed)

	parts := strings.Split(trimmed, " ")
	if len(parts) < 2 {
		return 0, 0, fmt.Errorf("invalid hunk header %q", header)
	}

	leftStart, err := parseHunkRange(parts[0])
	if err != nil {
		return 0, 0, err
	}
	rightStart, err := parseHunkRange(parts[1])
	if err != nil {
		return 0, 0, err
	}

	return leftStart, rightStart, nil
}

func parseHunkRange(segment string) (int, error) {
	if segment == "" {
		return 0, fmt.Errorf("invalid hunk range")
	}
	if segment[0] == '-' || segment[0] == '+' {
		segment = segment[1:]
	}
	if segment == "" {
		return 0, fmt.Errorf("invalid hunk range")
	}
	if idx := strings.Index(segment, ","); idx != -1 {
		segment = segment[:idx]
	}
	return strconv.Atoi(segment)
}

type submitOptions struct {
	HttpClient func() (*http.Client, error)
	IO         *iostreams.IOStreams
	Finder     prshared.PRFinder

	Selector string
	ReviewID int64
	Event    string
	Body     string
	Exporter cmdutil.Exporter
}

func NewCmdReviewSubmit(f *cmdutil.Factory, runF func(*submitOptions) error) *cobra.Command {
	opts := &submitOptions{
		IO:         f.IOStreams,
		HttpClient: f.HttpClient,
	}

	var bodyFile string

	cmd := &cobra.Command{
		Use:   "submit [<number> | <url> | <branch>]",
		Short: "Submit a pending review",
		Long: heredoc.Doc(`
			Finalize a pending review with the specified terminal state: COMMENT, APPROVE,
			or REQUEST_CHANGES.
		`),
		Example: heredoc.Doc(`
			# Submit a pending review as approval
			$ gh pr review submit --review-id 222 --event APPROVE

			# Submit with request for changes including a summary body
			$ gh pr review submit 42 --review-id 333 --event REQUEST_CHANGES --body "Please address the notes."
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Finder = prshared.NewFinder(f)

			if repoOverride, _ := cmd.Flags().GetString("repo"); repoOverride != "" && len(args) == 0 {
				return cmdutil.FlagErrorf("argument required when using the --repo flag")
			}

			if len(args) > 0 {
				opts.Selector = args[0]
			}

			if opts.ReviewID <= 0 {
				return cmdutil.FlagErrorf("--review-id must be provided")
			}

			bodyProvided := cmd.Flags().Changed("body")
			bodyFileProvided := bodyFile != ""
			if err := cmdutil.MutuallyExclusive("specify only one of `--body` or `--body-file`", bodyProvided, bodyFileProvided); err != nil {
				return err
			}
			if bodyFileProvided {
				content, err := cmdutil.ReadFile(bodyFile, opts.IO.In)
				if err != nil {
					return err
				}
				opts.Body = string(content)
			}

			if runF != nil {
				return runF(opts)
			}
			return submitRun(opts)
		},
	}

	cmd.Flags().Int64Var(&opts.ReviewID, "review-id", 0, "Pending review ID")
	cmdutil.StringEnumFlag(cmd, &opts.Event, "event", "", "", []string{"COMMENT", "APPROVE", "REQUEST_CHANGES"}, "Terminal state for the review")
	cmd.Flags().StringVarP(&opts.Body, "body", "b", "", "Optional body to include when submitting the review")
	cmd.Flags().StringVarP(&bodyFile, "body-file", "F", "", "Read body text from `file` (use \"-\" for standard input)")
	cmdutil.AddJSONFlags(cmd, &opts.Exporter, prshared.ReviewFields)

	return cmd
}

func submitRun(opts *submitOptions) error {
	if opts.Event == "" {
		return cmdutil.FlagErrorf("--event must be provided")
	}

	pr, repo, err := findPullRequest(opts.Finder, opts.Selector)
	if err != nil {
		return err
	}

	httpClient, err := opts.HttpClient()
	if err != nil {
		return err
	}
	client := api.NewClientFromHTTP(httpClient)

	review, err := api.SubmitPendingReviewREST(client, repo, pr.Number, opts.ReviewID, api.SubmitReviewInput{Event: strings.ToUpper(opts.Event), Body: opts.Body})
	if err != nil {
		return err
	}

	output := prshared.NewReviewOutput(*review)
	if opts.Exporter != nil {
		return opts.Exporter.Write(opts.IO, output)
	}

	return writeJSON(opts.IO, output)
}

type abortOptions struct {
	HttpClient func() (*http.Client, error)
	IO         *iostreams.IOStreams
	Finder     prshared.PRFinder

	Selector string
	ReviewID int64
}

func NewCmdReviewAbort(f *cmdutil.Factory, runF func(*abortOptions) error) *cobra.Command {
	opts := &abortOptions{
		IO:         f.IOStreams,
		HttpClient: f.HttpClient,
	}

	cmd := &cobra.Command{
		Use:   "abort [<number> | <url> | <branch>]",
		Short: "Abort a pending review",
		Long: heredoc.Doc(`
			Delete an existing pending review and discard any accumulated draft comments.
		`),
		Example: heredoc.Doc(`
			# Abort pending review 222 on PR 42
			$ gh pr review abort 42 --review-id 222
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Finder = prshared.NewFinder(f)

			if repoOverride, _ := cmd.Flags().GetString("repo"); repoOverride != "" && len(args) == 0 {
				return cmdutil.FlagErrorf("argument required when using the --repo flag")
			}

			if len(args) > 0 {
				opts.Selector = args[0]
			}

			if opts.ReviewID <= 0 {
				return cmdutil.FlagErrorf("--review-id must be provided")
			}

			if runF != nil {
				return runF(opts)
			}
			return abortRun(opts)
		},
	}

	cmd.Flags().Int64Var(&opts.ReviewID, "review-id", 0, "Pending review ID")

	return cmd
}

func abortRun(opts *abortOptions) error {
	pr, repo, err := findPullRequest(opts.Finder, opts.Selector)
	if err != nil {
		return err
	}

	httpClient, err := opts.HttpClient()
	if err != nil {
		return err
	}
	client := api.NewClientFromHTTP(httpClient)

	if err := api.DeletePendingReviewREST(client, repo, pr.Number, opts.ReviewID); err != nil {
		return err
	}

	payload := struct {
		ReviewID int64  `json:"review_id"`
		Status   string `json:"status"`
	}{ReviewID: opts.ReviewID, Status: "aborted"}

	encoder := json.NewEncoder(opts.IO.Out)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(payload)
}

func findPullRequest(finder prshared.PRFinder, selector string) (*api.PullRequest, ghrepo.Interface, error) {
	findOpts := prshared.FindOptions{
		Selector: selector,
		Fields:   []string{"number"},
	}
	pr, repo, err := finder.Find(findOpts)
	if err != nil {
		return nil, nil, err
	}
	return pr, repo, nil
}

func writeJSON(io *iostreams.IOStreams, data interface{}) error {
	encoder := json.NewEncoder(io.Out)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(data)
}
