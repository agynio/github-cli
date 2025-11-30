package see_comments

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

type SeeCommentsOptions struct {
	IO         *iostreams.IOStreams
	HttpClient func() (*http.Client, error)
	Config     func() (gh.Config, error)

	Org      string
	Repo     string
	Pull     int
	ReviewID int64
	Latest   bool
	Reviewer string
	Hostname string
}

func NewCmdSeeComments(f *cmdutil.Factory) *cobra.Command {
	opts := &SeeCommentsOptions{
		IO:         f.IOStreams,
		HttpClient: f.HttpClient,
		Config:     f.Config,
	}

	cmd := &cobra.Command{
		Use:   "see-comments",
		Short: "List review comments for a pull request review",
		Long: heredoc.Doc(`
			Fetch inline review comments for a pull request review by identifier or by resolving
			the latest submitted review from a reviewer.
		`),
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.ReviewID == 0 && !opts.Latest {
				return cmdutil.FlagErrorf("must specify --review-id or --latest")
			}
			if opts.ReviewID != 0 && opts.Latest {
				return cmdutil.FlagErrorf("flags --review-id and --latest are mutually exclusive")
			}
			if opts.Reviewer != "" && !opts.Latest {
				return cmdutil.FlagErrorf("--reviewer is only valid with --latest")
			}
			if opts.Pull <= 0 {
				return cmdutil.FlagErrorf("invalid value for --pr: %d", opts.Pull)
			}

			return runSeeComments(cmd.Context(), opts)
		},
	}

	cmd.Flags().StringVar(&opts.Org, "org", "", "Organization that owns the repository")
	cmd.Flags().StringVar(&opts.Repo, "repo", "", "Repository name")
	cmd.Flags().IntVar(&opts.Pull, "pr", 0, "Pull request number")
	cmd.Flags().Int64Var(&opts.ReviewID, "review-id", 0, "Pull request review identifier")
	cmd.Flags().BoolVar(&opts.Latest, "latest", false, "Resolve the latest submitted review")
	cmd.Flags().StringVar(&opts.Reviewer, "reviewer", "", "Reviewer login when using --latest")
	cmd.Flags().StringVar(&opts.Hostname, "hostname", "", "GitHub hostname (default to authenticated host)")

	_ = cmd.MarkFlagRequired("org")
	_ = cmd.MarkFlagRequired("repo")
	_ = cmd.MarkFlagRequired("pr")

	return cmd
}

func runSeeComments(ctx context.Context, opts *SeeCommentsOptions) error {
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

	reviewID := opts.ReviewID
	if opts.Latest {
		reviewer := opts.Reviewer
		if reviewer == "" {
			reviewer, err = service.CurrentLogin(ctx)
			if err != nil {
				return formatAPIError(err, opts, "failed to resolve authenticated user")
			}
		}

		reviewID, err = service.LatestReviewID(ctx, opts.Org, opts.Repo, opts.Pull, reviewer)
		if err != nil {
			return formatAPIError(err, opts, "failed to locate latest review")
		}
	}

	comments, err := service.ReviewComments(ctx, opts.Org, opts.Repo, opts.Pull, reviewID)
	if err != nil {
		return formatAPIError(err, opts, "failed to fetch review comments")
	}

	encoder := json.NewEncoder(opts.IO.Out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(comments); err != nil {
		return err
	}

	return nil
}

func formatAPIError(err error, opts *SeeCommentsOptions, prefix string) error {
	switch e := err.(type) {
	case *reviewapi.PullRequestNotFoundError:
		return fmt.Errorf("%s: %w", prefix, e)
	case *reviewapi.ReviewNotFoundError:
		return fmt.Errorf("%s: %w", prefix, e)
	case *reviewapi.NoSubmittedReviewError:
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
