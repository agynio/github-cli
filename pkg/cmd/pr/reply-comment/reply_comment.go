package reply_comment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/MakeNowJust/heredoc"
	"github.com/cli/cli/v2/api"
	"github.com/cli/cli/v2/internal/gh"
	"github.com/cli/cli/v2/pkg/cmd/pr/reviewapi"
	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/spf13/cobra"
)

const autoSubmitSummary = "Auto-submitting pending review to unblock threaded reply via gh CLI."

type ReplyCommentOptions struct {
	IO         *iostreams.IOStreams
	HttpClient func() (*http.Client, error)
	Config     func() (gh.Config, error)

	Org               string
	Repo              string
	Pull              int
	CommentID         int64
	Body              string
	AutoSubmitPending bool
	Hostname          string
}

func NewCmdReplyComment(f *cmdutil.Factory) *cobra.Command {
	opts := &ReplyCommentOptions{
		IO:         f.IOStreams,
		HttpClient: f.HttpClient,
		Config:     f.Config,
	}

	cmd := &cobra.Command{
		Use:   "reply-comment",
		Short: "Reply to a pull request review comment",
		Long: heredoc.Doc(`
			Reply to an existing pull request review comment by comment identifier.
		`),
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.Pull <= 0 {
				return cmdutil.FlagErrorf("invalid value for --pr: %d", opts.Pull)
			}
			if opts.CommentID <= 0 {
				return cmdutil.FlagErrorf("invalid value for --comment-id: %d", opts.CommentID)
			}
			if opts.Body == "" {
				return cmdutil.FlagErrorf("--body is required")
			}

			return runReplyComment(cmd.Context(), opts)
		},
	}

	cmd.Flags().StringVar(&opts.Org, "org", "", "Organization that owns the repository")
	cmd.Flags().StringVar(&opts.Repo, "repo", "", "Repository name")
	cmd.Flags().IntVar(&opts.Pull, "pr", 0, "Pull request number")
	cmd.Flags().Int64Var(&opts.CommentID, "comment-id", 0, "Review comment identifier to reply to")
	cmd.Flags().StringVar(&opts.Body, "body", "", "Reply text")
	cmd.Flags().BoolVar(&opts.AutoSubmitPending, "auto-submit-pending", false, "Submit pending reviews before replying")
	cmd.Flags().StringVar(&opts.Hostname, "hostname", "", "GitHub hostname (default to authenticated host)")

	_ = cmd.MarkFlagRequired("org")
	_ = cmd.MarkFlagRequired("repo")
	_ = cmd.MarkFlagRequired("pr")
	_ = cmd.MarkFlagRequired("comment-id")
	_ = cmd.MarkFlagRequired("body")

	return cmd
}

func runReplyComment(ctx context.Context, opts *ReplyCommentOptions) error {
	cfg, err := opts.Config()
	if err != nil {
		return err
	}

	host, _ := cfg.Authentication().DefaultHost()
	if opts.Hostname != "" {
		host = opts.Hostname
	}

	httpClient, err := opts.HttpClient()
	if err != nil {
		return err
	}

	service := reviewapi.NewService(httpClient, host)

	reply, err := service.ReplyToComment(ctx, opts.Org, opts.Repo, opts.Pull, opts.CommentID, opts.Body)
	if err != nil {
		var pendingErr *reviewapi.PendingReviewError
		if errors.As(err, &pendingErr) {
			if !opts.AutoSubmitPending {
				return fmt.Errorf("pending review detected for %s/%s#%d. Submit the review or re-run with --auto-submit-pending, or use the GraphQL review thread mutation.", opts.Org, opts.Repo, opts.Pull)
			}

			reviewer, loginErr := service.CurrentLogin(ctx)
			if loginErr != nil {
				return formatReplyError(loginErr, opts, "failed to resolve authenticated user")
			}

			submitted, submitErr := service.SubmitPendingReviews(ctx, opts.Org, opts.Repo, opts.Pull, reviewer, autoSubmitSummary)
			if submitErr != nil {
				return formatReplyError(submitErr, opts, "failed to submit pending review")
			}
			if submitted == 0 {
				return fmt.Errorf("no pending reviews owned by %s found on pull request #%d", reviewer, opts.Pull)
			}

			reply, err = service.ReplyToComment(ctx, opts.Org, opts.Repo, opts.Pull, opts.CommentID, opts.Body)
			if err != nil {
				return formatReplyError(err, opts, "failed to post reply after submitting pending review")
			}
		} else {
			return formatReplyError(err, opts, "failed to post reply")
		}
	}

	encoder := json.NewEncoder(opts.IO.Out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(reply); err != nil {
		return err
	}

	return nil
}

func formatReplyError(err error, opts *ReplyCommentOptions, prefix string) error {
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

	return fmt.Errorf("%s: %w", prefix, err)
}
