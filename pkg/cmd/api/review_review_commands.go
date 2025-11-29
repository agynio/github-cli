package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/MakeNowJust/heredoc"
	"github.com/cli/cli/v2/api"
	"github.com/cli/cli/v2/internal/gh"
	"github.com/cli/cli/v2/pkg/cmd/api/reviewapi"
	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/spf13/cobra"
)

func newReviewCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "review",
		Short: "Work with pull request reviews",
		Long: heredoc.Doc(`
			Manage pull request reviews using GraphQL pending review flows or REST one-shot submission.
		`),
	}

	cmd.AddCommand(newReviewOpenCmd(f))
	cmd.AddCommand(newReviewAddCmd(f))
	cmd.AddCommand(newReviewSubmitCmd(f))
	cmd.AddCommand(newReviewCreateCmd(f))

	return cmd
}

type reviewShared struct {
	IO         *iostreams.IOStreams
	HttpClient func() (*http.Client, error)
	Config     func() (gh.Config, error)
	Org        string
	Repo       string
	Pull       int
	Hostname   string
}

type reviewOpenOptions struct {
	reviewShared
	Commit string
}

func newReviewOpenCmd(f *cmdutil.Factory) *cobra.Command {
	opts := &reviewOpenOptions{
		reviewShared: reviewShared{
			IO:         f.IOStreams,
			HttpClient: f.HttpClient,
			Config:     f.Config,
		},
	}

	cmd := &cobra.Command{
		Use:   "open",
		Short: "Open a pending review",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateRepoArgs(opts.reviewShared); err != nil {
				return err
			}
			return runReviewOpen(cmd.Context(), opts)
		},
	}

	registerSharedFlags(cmd, &opts.reviewShared)
	cmd.Flags().StringVar(&opts.Commit, "commit", "", "Commit SHA to anchor the review (defaults to PR head)")

	return cmd
}

func runReviewOpen(ctx context.Context, opts *reviewOpenOptions) error {
	service, err := buildService(opts.reviewShared)
	if err != nil {
		return err
	}

	review, err := service.OpenReview(ctx, opts.Org, opts.Repo, opts.Pull, opts.Commit)
	if err != nil {
		return formatReviewRunError(err, "failed to open review")
	}

	return encodeJSON(opts.IO, map[string]interface{}{
		"id":           review.ID,
		"state":        review.State,
		"submitted_at": formatTime(review.SubmittedAt),
	})
}

type reviewAddOptions struct {
	reviewShared
	ReviewID  string
	Path      string
	Line      int
	Side      string
	StartLine int
	StartSide string
	Body      string
}

func newReviewAddCmd(f *cmdutil.Factory) *cobra.Command {
	opts := &reviewAddOptions{
		reviewShared: reviewShared{
			IO:         f.IOStreams,
			HttpClient: f.HttpClient,
			Config:     f.Config,
		},
	}

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add an inline comment thread to a pending review",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateRepoArgs(opts.reviewShared); err != nil {
				return err
			}
			if strings.TrimSpace(opts.ReviewID) == "" {
				return cmdutil.FlagErrorf("--review-id is required")
			}
			if strings.TrimSpace(opts.Path) == "" {
				return cmdutil.FlagErrorf("--path is required")
			}
			if opts.Line <= 0 {
				return cmdutil.FlagErrorf("invalid value for --line: %d", opts.Line)
			}
			if opts.Body == "" {
				return cmdutil.FlagErrorf("--body is required")
			}
			if opts.StartLine != 0 && opts.StartSide == "" {
				return cmdutil.FlagErrorf("--start-side is required when --start-line is provided")
			}
			return runReviewAdd(cmd.Context(), opts)
		},
	}

	registerSharedFlags(cmd, &opts.reviewShared)
	cmd.Flags().StringVar(&opts.ReviewID, "review-id", "", "GraphQL review identifier")
	cmd.Flags().StringVar(&opts.Path, "path", "", "File path for the comment thread")
	cmd.Flags().IntVar(&opts.Line, "line", 0, "Line number for the comment thread")
	cmd.Flags().StringVar(&opts.Side, "side", "RIGHT", "Diff side for the comment (LEFT or RIGHT)")
	cmd.Flags().IntVar(&opts.StartLine, "start-line", 0, "Optional start line for multi-line comments")
	cmd.Flags().StringVar(&opts.StartSide, "start-side", "", "Optional start side for multi-line comments (LEFT or RIGHT)")
	cmd.Flags().StringVar(&opts.Body, "body", "", "Comment body")

	return cmd
}

func runReviewAdd(ctx context.Context, opts *reviewAddOptions) error {
	service, err := buildService(opts.reviewShared)
	if err != nil {
		return err
	}

	side, err := normalizeSide(opts.Side)
	if err != nil {
		return cmdutil.FlagErrorf("%s", err)
	}

	var startLine *int
	var startSide *string
	if opts.StartLine != 0 {
		if opts.StartLine <= 0 {
			return cmdutil.FlagErrorf("invalid value for --start-line: %d", opts.StartLine)
		}
		normalized, err := normalizeSide(opts.StartSide)
		if err != nil {
			return cmdutil.FlagErrorf("%s", err)
		}
		startLine = &opts.StartLine
		startSide = &normalized
	}

	input := reviewapi.AddReviewThreadInput{
		ReviewID:  opts.ReviewID,
		Path:      opts.Path,
		Line:      opts.Line,
		Side:      side,
		StartLine: startLine,
		StartSide: startSide,
		Body:      opts.Body,
	}

	thread, err := service.AddReviewThread(ctx, opts.Org, opts.Repo, opts.Pull, input)
	if err != nil {
		return formatReviewRunError(err, "failed to add review thread")
	}

	return encodeJSON(opts.IO, map[string]interface{}{
		"id":          thread.ID,
		"path":        thread.Path,
		"is_outdated": thread.IsOutdated,
	})
}

type reviewSubmitOptions struct {
	reviewShared
	ReviewID string
	Event    string
	Body     string
}

func newReviewSubmitCmd(f *cmdutil.Factory) *cobra.Command {
	opts := &reviewSubmitOptions{
		reviewShared: reviewShared{
			IO:         f.IOStreams,
			HttpClient: f.HttpClient,
			Config:     f.Config,
		},
	}

	cmd := &cobra.Command{
		Use:   "submit",
		Short: "Submit a pending review",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateRepoArgs(opts.reviewShared); err != nil {
				return err
			}
			if strings.TrimSpace(opts.ReviewID) == "" {
				return cmdutil.FlagErrorf("--review-id is required")
			}
			if strings.TrimSpace(opts.Event) == "" {
				return cmdutil.FlagErrorf("--event is required")
			}
			return runReviewSubmit(cmd.Context(), opts)
		},
	}

	registerSharedFlags(cmd, &opts.reviewShared)
	cmd.Flags().StringVar(&opts.ReviewID, "review-id", "", "GraphQL review identifier")
	cmd.Flags().StringVar(&opts.Event, "event", "COMMENT", "Submit event (APPROVE, COMMENT, REQUEST_CHANGES)")
	cmd.Flags().StringVar(&opts.Body, "body", "", "Optional review summary body")

	return cmd
}

func runReviewSubmit(ctx context.Context, opts *reviewSubmitOptions) error {
	service, err := buildService(opts.reviewShared)
	if err != nil {
		return err
	}

	event, err := normalizeEvent(opts.Event)
	if err != nil {
		return cmdutil.FlagErrorf("%s", err)
	}

	review, err := service.SubmitReview(ctx, opts.Org, opts.Repo, opts.Pull, opts.ReviewID, event, opts.Body)
	if err != nil {
		return formatReviewRunError(err, "failed to submit review")
	}

	return encodeJSON(opts.IO, map[string]interface{}{
		"id":           review.ID,
		"state":        review.State,
		"submitted_at": formatTime(review.SubmittedAt),
	})
}

type reviewCreateOptions struct {
	reviewShared
	Commit       string
	Event        string
	Body         string
	CommentsFile string
}

func newReviewCreateCmd(f *cmdutil.Factory) *cobra.Command {
	opts := &reviewCreateOptions{
		reviewShared: reviewShared{
			IO:         f.IOStreams,
			HttpClient: f.HttpClient,
			Config:     f.Config,
		},
	}

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create and optionally submit a review using REST",
		Long: heredoc.Doc(`
			Create a review via the REST API in a single request, optionally including inline comments.
		`),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateRepoArgs(opts.reviewShared); err != nil {
				return err
			}
			return runReviewCreate(cmd.Context(), opts)
		},
	}

	registerSharedFlags(cmd, &opts.reviewShared)
	cmd.Flags().StringVar(&opts.Commit, "commit", "", "Commit SHA for the review (defaults to PR head)")
	cmd.Flags().StringVar(&opts.Event, "event", "", "Review event (APPROVE, COMMENT, REQUEST_CHANGES)")
	cmd.Flags().StringVar(&opts.Body, "body", "", "Review body")
	cmd.Flags().StringVar(&opts.CommentsFile, "comments-file", "", "Path to JSON file containing review comments [{\"path\",\"position\",\"body\"}]")

	return cmd
}

func runReviewCreate(ctx context.Context, opts *reviewCreateOptions) error {
	service, err := buildService(opts.reviewShared)
	if err != nil {
		return err
	}

	commit := opts.Commit
	if commit == "" {
		sha, shaErr := service.PullRequestHeadSHA(ctx, opts.Org, opts.Repo, opts.Pull)
		if shaErr != nil {
			return formatReviewRunError(shaErr, "failed to resolve pull request head")
		}
		commit = sha
	}

	var comments []reviewapi.CreateReviewComment
	if opts.CommentsFile != "" {
		data, readErr := os.ReadFile(opts.CommentsFile)
		if readErr != nil {
			return fmt.Errorf("failed to read comments file: %w", readErr)
		}
		if err := json.Unmarshal(data, &comments); err != nil {
			return fmt.Errorf("failed to parse comments file: %w", err)
		}
		for i, c := range comments {
			if strings.TrimSpace(c.Path) == "" {
				return fmt.Errorf("comment %d missing path", i)
			}
			if c.Position <= 0 {
				return fmt.Errorf("comment %d missing valid position", i)
			}
			if strings.TrimSpace(c.Body) == "" {
				return fmt.Errorf("comment %d missing body", i)
			}
		}
	}

	event := strings.ToUpper(strings.TrimSpace(opts.Event))
	if event != "" {
		if event, err = normalizeEvent(event); err != nil {
			return cmdutil.FlagErrorf("%s", err)
		}
	}

	input := reviewapi.CreateReviewInput{
		CommitID: commit,
		Event:    event,
		Body:     opts.Body,
		Comments: comments,
	}

	review, err := service.CreateReviewREST(ctx, opts.Org, opts.Repo, opts.Pull, input)
	if err != nil {
		return formatReviewRunError(err, "failed to create review")
	}

	output := map[string]interface{}{
		"id":           review.ID,
		"state":        review.State,
		"submitted_at": formatTime(review.SubmittedAt),
	}
	return encodeJSON(opts.IO, output)
}

func validateRepoArgs(shared reviewShared) error {
	if strings.TrimSpace(shared.Org) == "" {
		return cmdutil.FlagErrorf("--org is required")
	}
	if strings.TrimSpace(shared.Repo) == "" {
		return cmdutil.FlagErrorf("--repo is required")
	}
	if shared.Pull <= 0 {
		return cmdutil.FlagErrorf("invalid value for --pr: %d", shared.Pull)
	}
	return nil
}

func buildService(shared reviewShared) (*reviewapi.Service, error) {
	cfg, err := shared.Config()
	if err != nil {
		return nil, err
	}

	host, _ := cfg.Authentication().DefaultHost()
	if shared.Hostname != "" {
		host = shared.Hostname
	}

	httpClient, err := shared.HttpClient()
	if err != nil {
		return nil, err
	}

	return reviewapi.NewService(httpClient, host), nil
}

func registerSharedFlags(cmd *cobra.Command, shared *reviewShared) {
	cmd.Flags().StringVar(&shared.Org, "org", "", "Organization that owns the repository")
	cmd.Flags().StringVar(&shared.Repo, "repo", "", "Repository name")
	cmd.Flags().IntVar(&shared.Pull, "pr", 0, "Pull request number")
	cmd.Flags().StringVar(&shared.Hostname, "hostname", "", "GitHub hostname (default to authenticated host)")
}

func normalizeSide(side string) (string, error) {
	upper := strings.ToUpper(strings.TrimSpace(side))
	switch upper {
	case "LEFT", "RIGHT":
		return upper, nil
	default:
		return "", fmt.Errorf("invalid side %q: must be LEFT or RIGHT", side)
	}
}

func normalizeEvent(event string) (string, error) {
	upper := strings.ToUpper(strings.TrimSpace(event))
	switch upper {
	case "APPROVE", "COMMENT", "REQUEST_CHANGES":
		return upper, nil
	default:
		return "", fmt.Errorf("invalid event %q: must be APPROVE, COMMENT, or REQUEST_CHANGES", event)
	}
}

func encodeJSON(ioStreams *iostreams.IOStreams, payload interface{}) error {
	encoder := json.NewEncoder(ioStreams.Out)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(payload)
}

func formatTime(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

func formatReviewRunError(err error, prefix string) error {
	switch e := err.(type) {
	case *reviewapi.PullRequestNotFoundError:
		return fmt.Errorf("%s: %w", prefix, e)
	case *reviewapi.ReviewNotFoundError:
		return fmt.Errorf("%s: %w", prefix, e)
	case *reviewapi.CommentNotFoundError:
		return fmt.Errorf("%s: %w", prefix, e)
	}

	var httpErr api.HTTPError
	if errors.As(err, &httpErr) {
		switch httpErr.StatusCode {
		case http.StatusForbidden:
			return fmt.Errorf("%s: access denied (%s)", prefix, httpErr.Message)
		case http.StatusNotFound:
			return fmt.Errorf("%s: resource not found (%s)", prefix, httpErr.Message)
		case http.StatusUnprocessableEntity:
			return fmt.Errorf("%s: validation failed (%s)", prefix, httpErr.Message)
		default:
			return fmt.Errorf("%s: %s", prefix, httpErr.Error())
		}
	}

	var gqlErr api.GraphQLError
	if errors.As(err, &gqlErr) {
		return fmt.Errorf("%s: %s", prefix, gqlErr.Error())
	}

	return fmt.Errorf("%s: %w", prefix, err)
}
