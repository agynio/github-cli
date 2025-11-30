package review

import (
	"context"
	"strings"

	"github.com/cli/cli/v2/pkg/cmd/pr/reviewapi"
	"github.com/cli/cli/v2/pkg/cmd/pr/shared"
	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/spf13/cobra"
)

func NewCmdReviewPending(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pending",
		Short: "Manage pending pull request reviews",
		Long:  "Open, update, and submit pending pull request reviews.",
	}

	cmd.AddCommand(NewCmdReviewOpen(f))
	cmd.AddCommand(NewCmdReviewAdd(f))
	cmd.AddCommand(NewCmdReviewSubmit(f))

	return cmd
}

type reviewOpenOptions struct {
	shared PendingReviewSharedOptions
	Commit string
}

func NewCmdReviewOpen(f *cmdutil.Factory) *cobra.Command {
	opts := &reviewOpenOptions{
		shared: PendingReviewSharedOptions{
			IO:         f.IOStreams,
			HttpClient: f.HttpClient,
			Config:     f.Config,
		},
	}

	cmd := &cobra.Command{
		Use:   "open [<number> | <url> | <owner>/<repo>#<number>]",
		Short: "Open a pending review",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.shared.Finder = shared.NewFinder(f)
			if len(args) > 0 {
				opts.shared.SelectorArg = args[0]
			}
			if err := opts.shared.ResolvePullRequest(); err != nil {
				return err
			}
			return runReviewOpen(cmd.Context(), opts)
		},
	}

	opts.shared.RegisterFlags(cmd)
	cmd.Flags().StringVar(&opts.Commit, "commit", "", "Commit SHA to anchor the review (defaults to PR head)")

	return cmd
}

func runReviewOpen(ctx context.Context, opts *reviewOpenOptions) error {
	service, err := opts.shared.BuildService()
	if err != nil {
		return err
	}

	review, err := service.OpenReview(ctx, opts.shared.Repo.RepoOwner(), opts.shared.Repo.RepoName(), opts.shared.Pull, opts.Commit)
	if err != nil {
		return FormatReviewRunError(err, "failed to open review")
	}

	return EncodeJSON(opts.shared.IO, map[string]interface{}{
		"id":           review.ID,
		"state":        review.State,
		"submitted_at": FormatTime(review.SubmittedAt),
	})
}

type reviewAddOptions struct {
	shared    PendingReviewSharedOptions
	ReviewID  string
	Path      string
	Line      int
	Side      string
	StartLine int
	StartSide string
	Body      string
}

func NewCmdReviewAdd(f *cmdutil.Factory) *cobra.Command {
	opts := &reviewAddOptions{
		shared: PendingReviewSharedOptions{
			IO:         f.IOStreams,
			HttpClient: f.HttpClient,
			Config:     f.Config,
		},
		Side: "RIGHT",
	}

	cmd := &cobra.Command{
		Use:   "add [<number> | <url> | <owner>/<repo>#<number>]",
		Short: "Add an inline comment thread to a pending review",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.shared.Finder = shared.NewFinder(f)
			if len(args) > 0 {
				opts.shared.SelectorArg = args[0]
			}
			if err := opts.shared.ResolvePullRequest(); err != nil {
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

	opts.shared.RegisterFlags(cmd)
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
	service, err := opts.shared.BuildService()
	if err != nil {
		return err
	}

	side, err := NormalizeSide(opts.Side)
	if err != nil {
		return cmdutil.FlagErrorf("%s", err)
	}

	var startLine *int
	var startSide *string
	if opts.StartLine != 0 {
		if opts.StartLine <= 0 {
			return cmdutil.FlagErrorf("invalid value for --start-line: %d", opts.StartLine)
		}
		normalized, err := NormalizeSide(opts.StartSide)
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

	thread, err := service.AddReviewThread(ctx, opts.shared.Repo.RepoOwner(), opts.shared.Repo.RepoName(), opts.shared.Pull, input)
	if err != nil {
		return FormatReviewRunError(err, "failed to add review thread")
	}

	return EncodeJSON(opts.shared.IO, map[string]interface{}{
		"id":          thread.ID,
		"path":        thread.Path,
		"is_outdated": thread.IsOutdated,
	})
}

type reviewSubmitOptions struct {
	shared   PendingReviewSharedOptions
	ReviewID string
	Event    string
	Body     string
}

func NewCmdReviewSubmit(f *cmdutil.Factory) *cobra.Command {
	opts := &reviewSubmitOptions{
		shared: PendingReviewSharedOptions{
			IO:         f.IOStreams,
			HttpClient: f.HttpClient,
			Config:     f.Config,
		},
		Event: "COMMENT",
	}

	cmd := &cobra.Command{
		Use:   "submit [<number> | <url> | <owner>/<repo>#<number>]",
		Short: "Submit a pending review",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.shared.Finder = shared.NewFinder(f)
			if len(args) > 0 {
				opts.shared.SelectorArg = args[0]
			}
			if err := opts.shared.ResolvePullRequest(); err != nil {
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

	opts.shared.RegisterFlags(cmd)
	cmd.Flags().StringVar(&opts.ReviewID, "review-id", "", "GraphQL review identifier")
	cmd.Flags().StringVar(&opts.Event, "event", "COMMENT", "Submit event (APPROVE, COMMENT, REQUEST_CHANGES)")
	cmd.Flags().StringVar(&opts.Body, "body", "", "Optional review summary body")

	return cmd
}

func runReviewSubmit(ctx context.Context, opts *reviewSubmitOptions) error {
	service, err := opts.shared.BuildService()
	if err != nil {
		return err
	}

	event, err := NormalizeEvent(opts.Event)
	if err != nil {
		return cmdutil.FlagErrorf("%s", err)
	}

	review, err := service.SubmitReview(ctx, opts.shared.Repo.RepoOwner(), opts.shared.Repo.RepoName(), opts.shared.Pull, opts.ReviewID, event, opts.Body)
	if err != nil {
		return FormatReviewRunError(err, "failed to submit review")
	}

	return EncodeJSON(opts.shared.IO, map[string]interface{}{
		"id":           review.ID,
		"state":        review.State,
		"submitted_at": FormatTime(review.SubmittedAt),
	})
}
