package reply_comment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/MakeNowJust/heredoc"
	"github.com/cli/cli/v2/api"
	"github.com/cli/cli/v2/internal/ghrepo"
	"github.com/cli/cli/v2/pkg/cmd/pr/reviewapi"
	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/spf13/cobra"
)

const autoSubmitSummary = "Auto-submitting pending review to unblock threaded reply via gh CLI."

type ReplyCommentOptions struct {
	IO         *iostreams.IOStreams
	HttpClient func() (*http.Client, error)
	BaseRepo   func() (ghrepo.Interface, error)

	Pull              int
	CommentID         int64
	Body              string
	AutoSubmitPending bool

	repo ghrepo.Interface
}

func NewCmdReplyComment(f *cmdutil.Factory) *cobra.Command {
	opts := &ReplyCommentOptions{
		IO:         f.IOStreams,
		HttpClient: f.HttpClient,
		BaseRepo:   f.BaseRepo,
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

	cmd.Flags().IntVar(&opts.Pull, "pr", 0, "Pull request number")
	cmd.Flags().Int64Var(&opts.CommentID, "comment-id", 0, "Review comment identifier to reply to")
	cmd.Flags().StringVar(&opts.Body, "body", "", "Reply text")
	cmd.Flags().BoolVar(&opts.AutoSubmitPending, "auto-submit-pending", false, "Submit pending reviews before replying")

	_ = cmd.MarkFlagRequired("pr")
	_ = cmd.MarkFlagRequired("comment-id")
	_ = cmd.MarkFlagRequired("body")

	return cmd
}

func runReplyComment(ctx context.Context, opts *ReplyCommentOptions) error {
	repo, err := opts.resolveRepo()
	if err != nil {
		return err
	}

	httpClient, err := opts.HttpClient()
	if err != nil {
		return err
	}

	service := reviewapi.NewService(httpClient, repo.RepoHost())
	owner := repo.RepoOwner()
	name := repo.RepoName()
	fullName := ghrepo.FullName(repo)

	reply, err := service.ReplyToComment(ctx, owner, name, opts.Pull, opts.CommentID, opts.Body)
	if err != nil {
		var pendingErr *reviewapi.PendingReviewError
		if errors.As(err, &pendingErr) {
			if !opts.AutoSubmitPending {
				return fmt.Errorf("pending review detected for %s#%d. Submit the review or re-run with --auto-submit-pending, or use the GraphQL review thread mutation.", fullName, opts.Pull)
			}

			reviewer, loginErr := service.CurrentLogin(ctx)
			if loginErr != nil {
				return formatReplyError(loginErr, fullName, "failed to resolve authenticated user")
			}

			submitted, submitErr := service.SubmitPendingReviews(ctx, owner, name, opts.Pull, reviewer, autoSubmitSummary)
			if submitErr != nil {
				return formatReplyError(submitErr, fullName, "failed to submit pending review")
			}
			if submitted == 0 {
				return fmt.Errorf("no pending reviews owned by %s found on pull request #%d", reviewer, opts.Pull)
			}

			reply, err = service.ReplyToComment(ctx, owner, name, opts.Pull, opts.CommentID, opts.Body)
			if err != nil {
				return formatReplyError(err, fullName, "failed to post reply after submitting pending review")
			}
		} else {
			return formatReplyError(err, fullName, "failed to post reply")
		}
	}

	encoder := json.NewEncoder(opts.IO.Out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(reply); err != nil {
		return err
	}

	return nil
}

func (o *ReplyCommentOptions) resolveRepo() (ghrepo.Interface, error) {
	if o.repo != nil {
		return o.repo, nil
	}
	if o.BaseRepo == nil {
		return nil, errors.New("repository resolver is not configured")
	}
	repo, err := o.BaseRepo()
	if err != nil {
		return nil, err
	}
	o.repo = repo
	return repo, nil
}

func formatReplyError(err error, fullName string, prefix string) error {
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
			return fmt.Errorf("%s: access denied for %s (%s)", prefix, fullName, httpErr.Message)
		case http.StatusNotFound:
			return fmt.Errorf("%s: resource not found in %s (%s)", prefix, fullName, httpErr.Message)
		case http.StatusUnprocessableEntity:
			return fmt.Errorf("%s: validation failed (%s)", prefix, httpErr.Message)
		default:
			return fmt.Errorf("%s: %s", prefix, httpErr.Error())
		}
	}

	return fmt.Errorf("%s: %w", prefix, err)
}
