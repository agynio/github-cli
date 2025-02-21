package list

import (
	"fmt"
	"strconv"

	"github.com/MakeNowJust/heredoc"
	"github.com/cli/cli/v2/internal/browser"
	"github.com/cli/cli/v2/internal/ghrepo"
	"github.com/cli/cli/v2/internal/tableprinter"
	"github.com/cli/cli/v2/internal/text"
	"github.com/cli/cli/v2/pkg/cmd/repo/autolink/shared"
	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/spf13/cobra"
)

type listOptions struct {
	Browser        browser.Browser
	AutolinkClient AutolinkListClient
	IO             *iostreams.IOStreams

	BaseRepo ghrepo.Interface

	WebMode bool

	Renderer autolinkListRenderer
}

type AutolinkListClient interface {
	List(repo ghrepo.Interface) ([]shared.Autolink, error)
}

func NewCmdList(f *cmdutil.Factory, runF func(*listOptions) error) *cobra.Command {
	opts := &listOptions{
		Browser: f.Browser,
		IO:      f.IOStreams,
	}

	var exporter cmdutil.Exporter

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List autolink references for a GitHub repository",
		Long: heredoc.Doc(`
			Gets all autolink references that are configured for a repository.

			Information about autolinks is only available to repository administrators.
		`),
		Aliases: []string{"ls"},
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			baseRepo, err := f.BaseRepo()
			if err != nil {
				return err
			}
			opts.BaseRepo = baseRepo

			httpClient, err := f.HttpClient()
			if err != nil {
				return err
			}
			opts.AutolinkClient = &AutolinkLister{HTTPClient: httpClient}

			if exporter != nil {
				opts.Renderer = &exporterRenderer{ios: opts.IO, exporter: exporter}
			} else {
				opts.Renderer = &tableRenderer{
					repo: opts.BaseRepo,
					ios:  opts.IO,
				}
			}

			if runF != nil {
				return runF(opts)
			}

			return listRun(opts)
		},
	}

	cmd.Flags().BoolVarP(&opts.WebMode, "web", "w", false, "List autolink references in the web browser")
	cmdutil.AddJSONFlags(cmd, &exporter, shared.AutolinkFields)

	return cmd
}

type autolinkListRenderer interface {
	render(autolinks []shared.Autolink) error
}

type exporterRenderer struct {
	ios      *iostreams.IOStreams
	exporter cmdutil.Exporter
}

func (r *exporterRenderer) render(autolinks []shared.Autolink) error {
	return r.exporter.Write(r.ios, autolinks)
}

type tableRenderer struct {
	repo ghrepo.Interface
	ios  *iostreams.IOStreams
}

func (r *tableRenderer) render(autolinks []shared.Autolink) error {
	if len(autolinks) == 0 {
		return cmdutil.NewNoResultsError(
			fmt.Sprintf(
				"no autolinks found in %s",
				r.ios.ColorScheme().Bold(ghrepo.FullName(r.repo))),
		)
	}

	if r.ios.IsStdoutTTY() {
		title := fmt.Sprintf(
			"Showing %s in %s",
			text.Pluralize(len(autolinks), "autolink reference"),
			r.ios.ColorScheme().Bold(ghrepo.FullName(r.repo)),
		)
		fmt.Fprintf(r.ios.Out, "\n%s\n\n", title)
	}

	tp := tableprinter.New(r.ios, tableprinter.WithHeader("ID", "KEY PREFIX", "URL TEMPLATE", "ALPHANUMERIC"))
	for _, autolink := range autolinks {
		tp.AddField(r.ios.ColorScheme().Cyanf("%d", autolink.ID))
		tp.AddField(autolink.KeyPrefix)
		tp.AddField(autolink.URLTemplate)
		tp.AddField(strconv.FormatBool(autolink.IsAlphanumeric))
		tp.EndRow()
	}

	return tp.Render()
}

func listRun(opts *listOptions) error {
	if opts.WebMode {
		autolinksListURL := ghrepo.GenerateRepoURL(opts.BaseRepo, "settings/key_links")

		if opts.IO.IsStdoutTTY() {
			fmt.Fprintf(opts.IO.ErrOut, "Opening %s in your browser.\n", text.DisplayURL(autolinksListURL))
		}

		return opts.Browser.Browse(autolinksListURL)
	}

	autolinks, err := opts.AutolinkClient.List(opts.BaseRepo)
	if err != nil {
		return err
	}

	return opts.Renderer.render(autolinks)
}
