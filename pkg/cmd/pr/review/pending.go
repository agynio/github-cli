package review

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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

	payload := api.PendingReviewInput{Body: opts.Body, CommitID: opts.CommitID}
	review, err := api.CreatePendingReviewREST(client, repo, pr.Number, payload)
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

			Each comment JSON must provide "path" and "body", plus either a "position"
			integer or a combination of "line" and "side" (optionally "start_line" and
			"start_side" for ranges).
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

	review, err := api.GetPullRequestReviewREST(client, repo, pr.Number, opts.ReviewID)
	if err != nil {
		return translateReviewLookupError(err, opts.ReviewID)
	}
	if !strings.EqualFold(review.State, "PENDING") {
		return cmdutil.FlagErrorf("review %d is no longer pending; open a new review before adding comments", opts.ReviewID)
	}

	diffPaths, err := api.ListPullRequestFilePaths(client, repo, pr.Number)
	if err != nil {
		return fmt.Errorf("failed to list pull request files: %w", err)
	}

	inputs, err := collectPendingCommentInputs(opts)
	if err != nil {
		return err
	}

	outputs := make([]prshared.ReviewCommentOutput, 0, len(inputs))

	for idx, input := range inputs {
		normalized, err := normalizePendingCommentInput(input)
		if err != nil {
			return fmt.Errorf("comment %d: %w", idx+1, err)
		}
		if _, ok := diffPaths[normalized.Path]; !ok {
			return fmt.Errorf("comment %d: path %q is not part of the pull request diff", idx+1, normalized.Path)
		}
		created, err := api.AddPendingReviewCommentREST(client, repo, pr.Number, review, normalized)
		if err != nil {
			return fmt.Errorf("comment %d: %w", idx+1, interpretAddCommentError(err, normalized.Path, opts.ReviewID))
		}
		outputs = append(outputs, prshared.NewReviewCommentOutput(*created))
	}

	if opts.Exporter != nil {
		return opts.Exporter.Write(opts.IO, outputs)
	}

	return writeJSON(opts.IO, outputs)
}

func collectPendingCommentInputs(opts *addCommentOptions) ([]api.PendingReviewCommentInput, error) {
	var inputs []api.PendingReviewCommentInput

	for _, raw := range opts.CommentInputs {
		var input api.PendingReviewCommentInput
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

		data := strings.TrimSpace(string(content))
		if data == "" {
			return nil, cmdutil.FlagErrorf("comments file is empty")
		}

		if strings.HasPrefix(data, "[") {
			var fileInputs []api.PendingReviewCommentInput
			if err := json.Unmarshal([]byte(data), &fileInputs); err != nil {
				return nil, fmt.Errorf("invalid comments file: %w", err)
			}
			inputs = append(inputs, fileInputs...)
		} else {
			var single api.PendingReviewCommentInput
			if err := json.Unmarshal([]byte(data), &single); err != nil {
				return nil, fmt.Errorf("invalid comments file: %w", err)
			}
			inputs = append(inputs, single)
		}
	}

	return inputs, nil
}

func normalizePendingCommentInput(input api.PendingReviewCommentInput) (api.PendingReviewCommentInput, error) {
	input.Path = strings.TrimSpace(input.Path)
	bodyTrimmed := strings.TrimSpace(input.Body)

	if input.Path == "" {
		return api.PendingReviewCommentInput{}, cmdutil.FlagErrorf("comment path is required")
	}
	if bodyTrimmed == "" {
		return api.PendingReviewCommentInput{}, cmdutil.FlagErrorf("comment body cannot be blank")
	}

	if input.Position == nil && input.Line == nil {
		return api.PendingReviewCommentInput{}, cmdutil.FlagErrorf("specify either `position` or `line` with `side`")
	}
	if input.Position != nil && input.Line != nil {
		return api.PendingReviewCommentInput{}, cmdutil.FlagErrorf("`position` cannot be combined with `line`")
	}

	if input.Line != nil {
		if input.Side == nil {
			return api.PendingReviewCommentInput{}, cmdutil.FlagErrorf("`side` is required when `line` is provided")
		}
		side := strings.ToUpper(strings.TrimSpace(*input.Side))
		if side == "" {
			return api.PendingReviewCommentInput{}, cmdutil.FlagErrorf("`side` cannot be blank")
		}
		input.Side = &side

		if input.StartLine != nil {
			if input.StartSide == nil {
				return api.PendingReviewCommentInput{}, cmdutil.FlagErrorf("`start_side` is required when `start_line` is provided")
			}
			startSide := strings.ToUpper(strings.TrimSpace(*input.StartSide))
			if startSide == "" {
				return api.PendingReviewCommentInput{}, cmdutil.FlagErrorf("`start_side` cannot be blank")
			}
			input.StartSide = &startSide
		}
	}

	return input, nil
}

func translateReviewLookupError(err error, reviewID int64) error {
	var httpErr api.HTTPError
	if errors.As(err, &httpErr) {
		switch httpErr.StatusCode {
		case http.StatusNotFound:
			return cmdutil.FlagErrorf("pending review %d could not be found or is not accessible", reviewID)
		case http.StatusForbidden:
			if suggestion := httpErr.ScopesSuggestion(); suggestion != "" {
				return cmdutil.FlagErrorf("%s", suggestion)
			}
		}
		return fmt.Errorf("failed to load review %d: %w", reviewID, httpErr)
	}
	return err
}

func interpretAddCommentError(err error, path string, reviewID int64) error {
	var httpErr api.HTTPError
	if errors.As(err, &httpErr) {
		switch httpErr.StatusCode {
		case http.StatusNotFound:
			return cmdutil.FlagErrorf("review %d is no longer pending or owned by you", reviewID)
		case http.StatusUnprocessableEntity:
			if msg := firstValidationMessage(httpErr); msg != "" {
				return cmdutil.FlagErrorf("%s", msg)
			}
			return cmdutil.FlagErrorf("GitHub rejected the comment for %q: %s", path, httpErr.Error())
		case http.StatusForbidden:
			if suggestion := httpErr.ScopesSuggestion(); suggestion != "" {
				return cmdutil.FlagErrorf("%s", suggestion)
			}
		}
		return httpErr
	}

	var gqlErr api.GraphQLError
	if errors.As(err, &gqlErr) {
		if msg := firstGraphQLErrorMessage(gqlErr); msg != "" {
			return cmdutil.FlagErrorf("%s", msg)
		}
		return gqlErr
	}

	return err
}

func firstValidationMessage(httpErr api.HTTPError) string {
	if httpErr.HTTPError == nil {
		return ""
	}
	if len(httpErr.Errors) > 0 {
		if msg := strings.TrimSpace(httpErr.Errors[0].Message); msg != "" {
			return msg
		}
	}
	return strings.TrimSpace(httpErr.Message)
}

func firstGraphQLErrorMessage(gqlErr api.GraphQLError) string {
	if gqlErr.GraphQLError == nil {
		return ""
	}
	errs := gqlErr.Errors
	if len(errs) == 0 {
		return ""
	}
	return strings.TrimSpace(errs[0].Message)
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

	payload := api.SubmitReviewInput{Event: strings.ToUpper(opts.Event), Body: opts.Body}
	review, err := api.SubmitPendingReviewREST(client, repo, pr.Number, opts.ReviewID, payload)
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
		ReviewID int64  `json:"reviewId"`
		Status   string `json:"status"`
	}{
		ReviewID: opts.ReviewID,
		Status:   "aborted",
	}

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
