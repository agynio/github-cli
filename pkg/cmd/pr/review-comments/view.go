package reviewcomments

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/MakeNowJust/heredoc"
	"github.com/cli/cli/v2/api"
	"github.com/cli/cli/v2/internal/ghrepo"
	"github.com/cli/cli/v2/internal/text"
	prshared "github.com/cli/cli/v2/pkg/cmd/pr/shared"
	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/spf13/cobra"
)

type ViewOptions struct {
	HttpClient func() (*http.Client, error)
	IO         *iostreams.IOStreams

	Finder prshared.PRFinder

	Selector  string
	ReviewID  int64
	UseLatest bool
	PerPage   int
	Page      int
	Human     bool
	Exporter  cmdutil.Exporter
}

func NewCmdView(f *cmdutil.Factory, runF func(*ViewOptions) error) *cobra.Command {
	opts := &ViewOptions{
		IO:         f.IOStreams,
		HttpClient: f.HttpClient,
		PerPage:    30,
		Page:       1,
	}

	cmd := &cobra.Command{
		Use:   "view [<number> | <url> | <branch>]",
		Short: "List comments from a pull request review",
		Long: heredoc.Doc(`
			Display inline review comments for a pull request review.

			A review can be specified by its ID, or you can request the latest
			submitted review on the pull request.
		`),
		Example: heredoc.Doc(`
			# View comments for review 12345 on pull request 42
			$ gh pr review-comments view 42 --review-id 12345

			# View the latest submitted review for the current branch PR
			$ gh pr review-comments view --latest

			# View comments for the latest review of PR 99 in human readable form
			$ gh pr review-comments view 99 --latest --human
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

			if opts.ReviewID > 0 && opts.UseLatest {
				return cmdutil.FlagErrorf("specify only one of `--review-id` or `--latest`")
			}
			if opts.ReviewID == 0 && !opts.UseLatest {
				return cmdutil.FlagErrorf("one of `--review-id` or `--latest` must be provided")
			}
			if opts.PerPage < 1 || opts.PerPage > 100 {
				return cmdutil.FlagErrorf("--per-page must be between 1 and 100")
			}
			if opts.Page < 1 {
				return cmdutil.FlagErrorf("--page must be at least 1")
			}
			if opts.Human && opts.Exporter != nil {
				return cmdutil.FlagErrorf("`--human` cannot be combined with `--json`")
			}

			if runF != nil {
				return runF(opts)
			}
			return viewRun(opts)
		},
	}

	cmd.Flags().Int64Var(&opts.ReviewID, "review-id", 0, "Target review ID")
	cmd.Flags().BoolVar(&opts.UseLatest, "latest", false, "Select the latest submitted review")
	cmd.Flags().IntVar(&opts.PerPage, "per-page", 30, "Number of comments per page")
	cmd.Flags().IntVar(&opts.Page, "page", 1, "Page number of review comments to fetch")
	cmd.Flags().BoolVar(&opts.Human, "human", false, "Render output in a human-readable format")
	cmdutil.AddJSONFlags(cmd, &opts.Exporter, prshared.ReviewCommentFields)

	return cmd
}

func viewRun(opts *ViewOptions) error {
	pr, repo, err := findPullRequest(opts)
	if err != nil {
		return err
	}

	httpClient, err := opts.HttpClient()
	if err != nil {
		return err
	}
	apiClient := api.NewClientFromHTTP(httpClient)

	reviewID, err := resolveReviewID(apiClient, repo, pr.Number, opts.ReviewID, opts.UseLatest)
	if err != nil {
		return err
	}

	comments, err := loadReviewComments(apiClient, repo, pr.Number, reviewID, api.ReviewCommentsListParams{PerPage: opts.PerPage, Page: opts.Page})
	if err != nil {
		return err
	}

	if opts.Exporter != nil {
		return opts.Exporter.Write(opts.IO, comments)
	}

	if opts.Human {
		return renderHumanComments(opts.IO, reviewID, comments)
	}

	return writeJSON(opts.IO, comments)
}

func findPullRequest(opts *ViewOptions) (*api.PullRequest, ghrepo.Interface, error) {
	findOpts := prshared.FindOptions{
		Selector: opts.Selector,
		Fields:   []string{"number"},
	}
	pr, repo, err := opts.Finder.Find(findOpts)
	if err != nil {
		return nil, nil, err
	}
	return pr, repo, nil
}

func resolveReviewID(client *api.Client, repo ghrepo.Interface, prNumber int, explicit int64, latest bool) (int64, error) {
	if explicit > 0 {
		return explicit, nil
	}

	reviews, err := api.ListPullRequestReviewsREST(client, repo, prNumber)
	if err != nil {
		return 0, err
	}

	var (
		chosen     *api.PullRequestReviewREST
		latestTime time.Time
	)
	for i := range reviews {
		review := &reviews[i]
		if strings.EqualFold(review.State, "PENDING") {
			continue
		}
		if review.SubmittedAt == nil {
			continue
		}
		submitted := review.SubmittedAt.UTC()
		if chosen == nil || submitted.After(latestTime) || (submitted.Equal(latestTime) && review.ID > chosen.ID) {
			chosen = review
			latestTime = submitted
		}
	}

	if chosen == nil {
		return 0, fmt.Errorf("no submitted reviews found for pull request %d", prNumber)
	}

	return chosen.ID, nil
}

func loadReviewComments(client *api.Client, repo ghrepo.Interface, prNumber int, reviewID int64, params api.ReviewCommentsListParams) ([]prshared.ReviewCommentOutput, error) {
	comments, err := api.ListReviewCommentsREST(client, repo, prNumber, reviewID, params)
	if err != nil {
		return nil, err
	}

	outputs := make([]prshared.ReviewCommentOutput, len(comments))
	for i, comment := range comments {
		outputs[i] = prshared.NewReviewCommentOutput(comment)
	}
	return outputs, nil
}

func writeJSON(io *iostreams.IOStreams, data interface{}) error {
	encoder := json.NewEncoder(io.Out)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(data)
}

type commentThread struct {
	Root      *prshared.ReviewCommentOutput
	Replies   []prshared.ReviewCommentOutput
	ParentID  int64
	HasParent bool
}

func renderHumanComments(io *iostreams.IOStreams, reviewID int64, comments []prshared.ReviewCommentOutput) error {
	out := io.Out
	cs := io.ColorScheme()

	header := fmt.Sprintf("Review %d — %d comment", reviewID, len(comments))
	if len(comments) != 1 {
		header += "s"
	}
	fmt.Fprintln(out, header)

	if len(comments) == 0 {
		return nil
	}

	fmt.Fprintln(out)

	threads := groupCommentThreads(comments)

	for idx, thread := range threads {
		fmt.Fprintf(out, "%d. %s\n", idx+1, formatThreadHeader(thread, cs))
		if thread.Root != nil {
			printComment(out, *thread.Root, "  ", cs)
		} else if thread.HasParent {
			fmt.Fprintf(out, "  (parent comment %d not included in this page)\n", thread.ParentID)
		}
		for _, reply := range thread.Replies {
			printComment(out, reply, "    ", cs)
		}
		fmt.Fprintln(out)
	}

	return nil
}

func groupCommentThreads(comments []prshared.ReviewCommentOutput) []commentThread {
	sort.SliceStable(comments, func(i, j int) bool {
		return comments[i].CreatedAt.Before(comments[j].CreatedAt)
	})

	threadsByRoot := map[int64]*commentThread{}
	order := make([]*commentThread, 0, len(comments))

	for _, comment := range comments {
		rootID := comment.ID
		if comment.InReplyToID != nil {
			rootID = *comment.InReplyToID
		}

		thread, exists := threadsByRoot[rootID]
		if !exists {
			thread = &commentThread{}
			threadsByRoot[rootID] = thread
			order = append(order, thread)
		}

		if comment.InReplyToID == nil {
			c := comment
			thread.Root = &c
			thread.HasParent = false
		} else {
			if thread.Root == nil {
				thread.ParentID = *comment.InReplyToID
				thread.HasParent = true
			}
			thread.Replies = append(thread.Replies, comment)
		}
	}

	result := make([]commentThread, len(order))
	for i, thread := range order {
		result[i] = *thread
	}
	return result
}

func formatThreadHeader(thread commentThread, cs *iostreams.ColorScheme) string {
	if thread.Root != nil {
		return formatLocation(*thread.Root, cs)
	}
	if thread.HasParent {
		return fmt.Sprintf("Replies to %d", thread.ParentID)
	}
	return "Review thread"
}

func formatLocation(comment prshared.ReviewCommentOutput, cs *iostreams.ColorScheme) string {
	path := comment.Path
	if path == "" {
		path = "(no path)"
	}
	if cs != nil {
		path = cs.Cyan(path)
	}

	var details []string
	if comment.Line != nil {
		line := fmt.Sprintf("line %d", *comment.Line)
		if comment.Side != nil && *comment.Side != "" {
			line = fmt.Sprintf("%s %s", line, strings.ToLower(*comment.Side))
		}
		details = append(details, line)
	} else if comment.Position != nil {
		details = append(details, fmt.Sprintf("position %d", *comment.Position))
	}

	if len(details) == 0 {
		return path
	}
	return fmt.Sprintf("%s — %s", path, strings.Join(details, ", "))
}

func printComment(out io.Writer, comment prshared.ReviewCommentOutput, indent string, cs *iostreams.ColorScheme) {
	author := comment.Author
	if author == "" {
		author = "(unknown)"
	}
	if cs != nil {
		author = cs.Bold(author)
	}
	timestamp := comment.CreatedAt.Format(time.RFC3339)
	fmt.Fprintf(out, "%s%s [%s]\n", indent, author, timestamp)

	body := comment.Body
	if body == "" {
		body = "(no body)"
	}
	fmt.Fprintln(out, text.Indent(body, indent+"  "))
}
