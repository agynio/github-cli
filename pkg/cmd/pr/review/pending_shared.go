package review

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cli/cli/v2/api"
	"github.com/cli/cli/v2/internal/gh"
	"github.com/cli/cli/v2/pkg/cmd/pr/reviewapi"
	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/spf13/cobra"
)

// PendingReviewSharedOptions contains flags and dependencies shared by review subcommands
// that operate on pending reviews or REST helpers.
type PendingReviewSharedOptions struct {
	IO         *iostreams.IOStreams
	HttpClient func() (*http.Client, error)
	Config     func() (gh.Config, error)
	Org        string
	Repo       string
	Pull       int
	Hostname   string
}

// RegisterFlags adds the standard repository-related flags to the provided command.
func (o *PendingReviewSharedOptions) RegisterFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.Org, "org", "", "Organization that owns the repository")
	cmd.Flags().StringVar(&o.Repo, "repo", "", "Repository name")
	cmd.Flags().IntVar(&o.Pull, "pr", 0, "Pull request number")
	cmd.Flags().StringVar(&o.Hostname, "hostname", "", "GitHub hostname (default to authenticated host)")
}

// ValidateRepoArgs ensures repository flags are populated with valid values.
func (o *PendingReviewSharedOptions) ValidateRepoArgs() error {
	if strings.TrimSpace(o.Org) == "" {
		return cmdutil.FlagErrorf("--org is required")
	}
	if strings.TrimSpace(o.Repo) == "" {
		return cmdutil.FlagErrorf("--repo is required")
	}
	if o.Pull <= 0 {
		return cmdutil.FlagErrorf("invalid value for --pr: %d", o.Pull)
	}
	return nil
}

// BuildService constructs a review service using the configured HTTP client and hostname.
func (o *PendingReviewSharedOptions) BuildService() (*reviewapi.Service, error) {
	cfg, err := o.Config()
	if err != nil {
		return nil, err
	}

	host, _ := cfg.Authentication().DefaultHost()
	if o.Hostname != "" {
		host = o.Hostname
	}

	httpClient, err := o.HttpClient()
	if err != nil {
		return nil, err
	}

	return reviewapi.NewService(httpClient, host), nil
}

// NormalizeSide validates and normalizes a diff side identifier.
func NormalizeSide(side string) (string, error) {
	upper := strings.ToUpper(strings.TrimSpace(side))
	switch upper {
	case "LEFT", "RIGHT":
		return upper, nil
	default:
		return "", fmt.Errorf("invalid side %q: must be LEFT or RIGHT", side)
	}
}

// NormalizeEvent validates and normalizes a review submission event.
func NormalizeEvent(event string) (string, error) {
	upper := strings.ToUpper(strings.TrimSpace(event))
	switch upper {
	case "APPROVE", "COMMENT", "REQUEST_CHANGES":
		return upper, nil
	default:
		return "", fmt.Errorf("invalid event %q: must be APPROVE, COMMENT, or REQUEST_CHANGES", event)
	}
}

// EncodeJSON writes the provided payload as JSON to the command output stream.
func EncodeJSON(ioStreams *iostreams.IOStreams, payload interface{}) error {
	encoder := json.NewEncoder(ioStreams.Out)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(payload)
}

// FormatTime returns the RFC3339 representation of the provided timestamp or nil when absent.
func FormatTime(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

// FormatReviewRunError maps service errors to user-friendly messages.
func FormatReviewRunError(err error, prefix string) error {
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

	var gqlErr api.GraphQLError
	if errors.As(err, &gqlErr) {
		return fmt.Errorf("%s: %s", prefix, gqlErr.Error())
	}

	return fmt.Errorf("%s: %w", prefix, err)
}
