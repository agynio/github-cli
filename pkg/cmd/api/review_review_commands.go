package api

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/MakeNowJust/heredoc"
	prreview "github.com/cli/cli/v2/pkg/cmd/pr/review"
	"github.com/cli/cli/v2/pkg/cmd/pr/reviewapi"
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

	cmd.AddCommand(newDeprecatedReviewCommand(f.IOStreams, "open", "gh pr review open"))
	cmd.AddCommand(newDeprecatedReviewCommand(f.IOStreams, "add", "gh pr review add"))
	cmd.AddCommand(newDeprecatedReviewCommand(f.IOStreams, "submit", "gh pr review submit"))
	cmd.AddCommand(newReviewCreateCmd(f))

	return cmd
}

func newDeprecatedReviewCommand(io *iostreams.IOStreams, use, replacement string) *cobra.Command {
	message := heredoc.Docf(`
        The "gh api review %[1]s" command has moved and is now available as "%[2]s".
        Please update your scripts to use the new location.
    `, use, replacement)

	cmd := &cobra.Command{
		Use:   use,
		Short: fmt.Sprintf("Deprecated: use %s", replacement),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(io.ErrOut, message)
			return cmdutil.SilentError
		},
	}

	cmd.Long = message
	cmd.SilenceUsage = true
	return cmd
}

type reviewCreateOptions struct {
	shared       prreview.PendingReviewSharedOptions
	Commit       string
	Event        string
	Body         string
	CommentsFile string
}

func newReviewCreateCmd(f *cmdutil.Factory) *cobra.Command {
	opts := &reviewCreateOptions{
		shared: prreview.PendingReviewSharedOptions{
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
			if err := opts.shared.ValidateRepoArgs(); err != nil {
				return err
			}
			return runReviewCreate(cmd.Context(), opts)
		},
	}

	opts.shared.RegisterFlags(cmd)
	cmd.Flags().StringVar(&opts.Commit, "commit", "", "Commit SHA for the review (defaults to PR head)")
	cmd.Flags().StringVar(&opts.Event, "event", "", "Review event (APPROVE, COMMENT, REQUEST_CHANGES)")
	cmd.Flags().StringVar(&opts.Body, "body", "", "Review body")
	cmd.Flags().StringVar(&opts.CommentsFile, "comments-file", "", "Path to JSON file containing review comments [{\"path\",\"position\",\"body\"}]")

	return cmd
}

func runReviewCreate(ctx context.Context, opts *reviewCreateOptions) error {
	service, err := opts.shared.BuildService()
	if err != nil {
		return err
	}

	commit := opts.Commit
	if commit == "" {
		sha, shaErr := service.PullRequestHeadSHA(ctx, opts.shared.Org, opts.shared.Repo, opts.shared.Pull)
		if shaErr != nil {
			return prreview.FormatReviewRunError(shaErr, "failed to resolve pull request head")
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
		if event, err = prreview.NormalizeEvent(event); err != nil {
			return cmdutil.FlagErrorf("%s", err)
		}
	}

	input := reviewapi.CreateReviewInput{
		CommitID: commit,
		Event:    event,
		Body:     opts.Body,
		Comments: comments,
	}

	review, err := service.CreateReviewREST(ctx, opts.shared.Org, opts.shared.Repo, opts.shared.Pull, input)
	if err != nil {
		return prreview.FormatReviewRunError(err, "failed to create review")
	}

	output := map[string]interface{}{
		"id":           review.ID,
		"state":        review.State,
		"submitted_at": prreview.FormatTime(review.SubmittedAt),
	}
	return prreview.EncodeJSON(opts.shared.IO, output)
}
