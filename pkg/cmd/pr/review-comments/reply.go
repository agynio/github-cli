package reviewcomments

import (
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

type ReplyOptions struct {
	HttpClient func() (*http.Client, error)
	IO         *iostreams.IOStreams

	Finder prshared.PRFinder

	Selector  string
	CommentID int64
	Body      string
	Exporter  cmdutil.Exporter
}

func NewCmdReply(f *cmdutil.Factory, runF func(*ReplyOptions) error) *cobra.Command {
	opts := &ReplyOptions{
		IO:         f.IOStreams,
		HttpClient: f.HttpClient,
	}

	var bodyFile string

	cmd := &cobra.Command{
		Use:   "reply [<number> | <url> | <branch>]",
		Short: "Reply to an existing inline review comment",
		Long: heredoc.Doc(`
			Create a reply in an existing inline pull request review thread.

			The command requires the numeric review comment ID from the target thread.
		`),
		Example: heredoc.Doc(`
			# Reply to comment 98765 on pull request 42
			$ gh pr review-comments reply 42 --comment-id 98765 --body "Thanks, applied."

			# Reply with body read from file
			$ gh pr review-comments reply --comment-id 55555 --body-file reply.md
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

			if opts.CommentID <= 0 {
				return cmdutil.FlagErrorf("--comment-id must be provided")
			}

			bodyProvided := cmd.Flags().Changed("body")
			bodyFileProvided := bodyFile != ""
			if err := cmdutil.MutuallyExclusive("specify only one of `--body` or `--body-file`", bodyProvided, bodyFileProvided); err != nil {
				return err
			}

			if bodyFileProvided {
				b, err := cmdutil.ReadFile(bodyFile, opts.IO.In)
				if err != nil {
					return err
				}
				opts.Body = string(b)
			}

			if strings.TrimSpace(opts.Body) == "" {
				return cmdutil.FlagErrorf("reply body cannot be blank")
			}

			if runF != nil {
				return runF(opts)
			}
			return replyRun(opts)
		},
	}

	cmd.Flags().Int64Var(&opts.CommentID, "comment-id", 0, "Target review comment ID")
	cmd.Flags().StringVarP(&opts.Body, "body", "b", "", "Reply body text")
	cmd.Flags().StringVarP(&bodyFile, "body-file", "F", "", "Read body text from `file` (use \"-\" for standard input)")
	cmdutil.AddJSONFlags(cmd, &opts.Exporter, prshared.ReviewCommentFields)

	return cmd
}

func replyRun(opts *ReplyOptions) error {
	pr, repo, err := findPullRequestForReply(opts)
	if err != nil {
		return err
	}

	httpClient, err := opts.HttpClient()
	if err != nil {
		return err
	}
	apiClient := api.NewClientFromHTTP(httpClient)

	created, err := api.ReplyToReviewCommentREST(apiClient, repo, pr.Number, opts.CommentID, opts.Body)
	if err != nil {
		return err
	}

	output := prshared.NewReviewCommentOutput(*created)

	if opts.Exporter != nil {
		return opts.Exporter.Write(opts.IO, output)
	}

	return writeJSON(opts.IO, output)
}

func findPullRequestForReply(opts *ReplyOptions) (*api.PullRequest, ghrepo.Interface, error) {
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
