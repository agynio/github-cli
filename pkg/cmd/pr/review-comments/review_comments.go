package reviewcomments

import (
	"github.com/MakeNowJust/heredoc"
	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/spf13/cobra"
)

// NewCmdReviewComments constructs the `gh pr review-comments` command grouping related subcommands.
func NewCmdReviewComments(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "review-comments",
		Short: "Inspect and manage inline review comments",
		Long: heredoc.Doc(`
			Work with inline pull request review comments.

			Use subcommands to list comments from a review or reply to existing threads.
		`),
	}

	cmdutil.EnableRepoOverride(cmd, f)

	cmd.AddCommand(NewCmdView(f, nil))
	cmd.AddCommand(NewCmdReply(f, nil))

	return cmd
}
