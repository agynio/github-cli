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
	"github.com/cli/cli/v2/pkg/cmd/pr/shared"
	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/spf13/cobra"
)

const autoSubmitSummary = "Auto-submitting pending review to unblock threaded reply via gh CLI."

type ReplyCommentOptions struct {
	IO         *iostreams.IOStreams
	HttpClient func() (*http.Client, error)
	Config     func() (gh.Config, error)

	Finder shared.PRFinder

	SelectorArg       string
	PullFlag          int
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
		Use:   "reply-comment [<number> | <url> | <owner>/<repo>#<number>]",
		Short: "Reply to a pull request review comment",
		Long: heredoc.Doc(`
            Reply to an existing pull request review comment by comment identifier.
        `),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Finder = shared.NewFinder(f)
			if len(args) > 0 {
				opts.SelectorArg = args[0]
			}
			if opts.CommentID <= 0 {
				return cmdutil.FlagErrorf("invalid value for --comment-id: %d", opts.CommentID)
			}
			if opts.Body == "" {
				return cmdutil.FlagErrorf("--body is required")
			}

			selector, err := shared.NormalizePullRequestSelector(opts.SelectorArg, opts.PullFlag)
			if err != nil {
				return err
			}
			opts.SelectorArg = selector

			return runReplyComment(cmd.Context(), opts)
		},
	}

	cmd.Flags().IntVar(&opts.PullFlag, "pr", 0, "Pull request number")
	cmd.Flags().Int64Var(&opts.CommentID, "comment-id", 0, "Review comment identifier to reply to")
	cmd.Flags().StringVar(&opts.Body, "body", "", "Reply text")
	cmd.Flags().BoolVar(&opts.AutoSubmitPending, "auto-submit-pending", false, "Submit pending reviews before replying")
	cmd.Flags().StringVar(&opts.Hostname, "hostname", "", "GitHub hostname (default to authenticated host)")
	_ = cmd.MarkFlagRequired("comment-id")
	_ = cmd.MarkFlagRequired("body")

	return cmd
}

func runReplyComment(ctx context.Context, opts *ReplyCommentOptions) error {
	if opts.Finder == nil {
		return errors.New("pull request finder is not configured")
	}

	findOptions := shared.FindOptions{
		Selector:        opts.SelectorArg,
		Fields:          []string{"number"},
		DisableProgress: true,
	}
	pr, repo, err := opts.Finder.Find(findOptions)
	if err != nil {
		return err
	}
	pullNumber := pr.Number

	cfg, err := opts.Config()
	if err != nil {
		return err
	}

	host := ""
	if repo != nil {
		host = repo.RepoHost()
	}
	if host == "" {
		host, _ = cfg.Authentication().DefaultHost()
	}
	if opts.Hostname != "" {
		host = opts.Hostname
	}

	httpClient, err := opts.HttpClient()
	if err != nil {
		return err
	}

	service := reviewapi.NewService(httpClient, host)
	owner := repo.RepoOwner()
	name := repo.RepoName()

	reply, err := service.ReplyToComment(ctx, owner, name, pullNumber, opts.CommentID, opts.Body)
	if err != nil {
		var pendingErr *reviewapi.PendingReviewError
		if errors.As(err, &pendingErr) {
			if !opts.AutoSubmitPending {
				return fmt.Errorf("pending review detected for %s/%s#%d. Submit the review or re-run with --auto-submit-pending, or use the GraphQL review thread mutation.", owner, name, pullNumber)
			}

			reviewer, loginErr := service.CurrentLogin(ctx)
			if loginErr != nil {
				return formatReplyError(loginErr, "failed to resolve authenticated user")
			}

			submitted, submitErr := service.SubmitPendingReviews(ctx, owner, name, pullNumber, reviewer, autoSubmitSummary)
			if submitErr != nil {
				return formatReplyError(submitErr, "failed to submit pending review")
			}
			if submitted == 0 {
				return fmt.Errorf("no pending reviews owned by %s found on pull request #%d", reviewer, pullNumber)
			}

			reply, err = service.ReplyToComment(ctx, owner, name, pullNumber, opts.CommentID, opts.Body)
			if err != nil {
				return formatReplyError(err, "failed to post reply after submitting pending review")
			}
		} else {
			return formatReplyError(err, "failed to post reply")
		}
	}

	encoder := json.NewEncoder(opts.IO.Out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(reply); err != nil {
		return err
	}

	return nil
}

func formatReplyError(err error, prefix string) error {
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
