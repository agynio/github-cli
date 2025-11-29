package api

import (
	"fmt"

	"github.com/MakeNowJust/heredoc"
	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/spf13/cobra"
)

func newSeeCommentsCmd(f *cmdutil.Factory) *cobra.Command {
	return newDeprecatedReviewCmd(f.IOStreams, "see-comments", heredoc.Doc(`
        The "gh api see-comments" command has moved and is now available as "gh pr see-comments".
        Please update your scripts to use the new location.
    `))
}

func newReplyCommentCmd(f *cmdutil.Factory) *cobra.Command {
	return newDeprecatedReviewCmd(f.IOStreams, "reply-comment", heredoc.Doc(`
        The "gh api reply-comment" command has moved and is now available as "gh pr reply-comment".
        Please update your scripts to use the new location.
    `))
}

func newDeprecatedReviewCmd(io *iostreams.IOStreams, use, message string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   use,
		Short: fmt.Sprintf("Deprecated: use gh pr %s", use),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(io.ErrOut, message)
			return cmdutil.SilentError
		},
	}

	cmd.Long = message
	cmd.SilenceUsage = true
	return cmd
}
