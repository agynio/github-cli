package see_comments

import (
	"bytes"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/cli/cli/v2/internal/config"
	"github.com/cli/cli/v2/internal/gh"
	"github.com/cli/cli/v2/internal/ghrepo"
	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/cli/cli/v2/pkg/httpmock"
	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestFactory(t *testing.T, rt http.RoundTripper, repo ghrepo.Interface, repoErr error) (*cmdutil.Factory, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	ios, _, stdout, stderr := iostreams.Test()
	ios.SetStdoutTTY(false)
	ios.SetStdinTTY(false)
	ios.SetStderrTTY(false)

	cfg := config.NewBlankConfig()
	cfg.Authentication().SetDefaultHost("github.com", "default")

	factory := &cmdutil.Factory{
		IOStreams: ios,
		HttpClient: func() (*http.Client, error) {
			return &http.Client{Transport: rt}, nil
		},
		Config: func() (gh.Config, error) {
			return cfg, nil
		},
	}

	factory.BaseRepo = func() (ghrepo.Interface, error) {
		if repoErr != nil {
			return nil, repoErr
		}
		if repo != nil {
			return repo, nil
		}
		return ghrepo.FromFullName("ORG/REPO")
	}

	return factory, stdout, stderr
}

func TestSeeComments_ByReviewID(t *testing.T) {
	reg := &httpmock.Registry{}
	defer reg.Verify(t)

	comment := map[string]interface{}{
		"id":                     101,
		"pull_request_review_id": 456,
		"in_reply_to_id":         nil,
		"path":                   "main.go",
		"line":                   12,
		"side":                   "RIGHT",
		"body":                   "Looks good",
		"user": map[string]interface{}{
			"login": "octocat",
		},
		"created_at": time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC),
	}

	matcher := httpmock.WithHost(
		httpmock.QueryMatcher("GET", "repos/ORG/REPO/pulls/123/reviews/456/comments", url.Values{"per_page": {"100"}}),
		"api.github.com",
	)
	reg.Register(matcher, httpmock.JSONResponse([]interface{}{comment}))

	repo, err := ghrepo.FromFullName("ORG/REPO")
	require.NoError(t, err)
	f, stdout, stderr := newTestFactory(t, reg, repo, nil)
	cmd := NewCmdSeeComments(f)
	cmd.SetArgs([]string{"123", "--review-id", "456"})

	_, err = cmd.ExecuteC()
	require.NoError(t, err)
	assert.Equal(t, "", stderr.String())
	assert.JSONEq(t, `[{"id":101,"pull_request_review_id":456,"in_reply_to_id":null,"path":"main.go","line":12,"side":"RIGHT","body":"Looks good","user":{"login":"octocat"},"created_at":"2024-01-02T03:04:05Z"}]`, stdout.String())

	if len(reg.Requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reg.Requests))
	}
	req := reg.Requests[0]
	assert.Equal(t, "application/vnd.github+json", req.Header.Get("Accept"))
	assert.Equal(t, "2022-11-28", req.Header.Get("X-GitHub-Api-Version"))
}

func TestSeeComments_RepoOverrideWithoutGit(t *testing.T) {
	reg := &httpmock.Registry{}
	defer reg.Verify(t)

	matcher := httpmock.WithHost(
		httpmock.QueryMatcher("GET", "repos/octo/demo/pulls/55/reviews/77/comments", url.Values{"per_page": {"100"}}),
		"api.github.com",
	)
	reg.Register(matcher, httpmock.JSONResponse([]interface{}{}))

	// Simulate missing git repository by returning an error unless override is used.
	noRepoErr := errors.New("no repository context")
	f, stdout, stderr := newTestFactory(t, reg, nil, noRepoErr)
	// Simulate passing -R by overriding BaseRepo before command execution.
	f.BaseRepo = cmdutil.OverrideBaseRepoFunc(f, "octo/demo")

	cmd := NewCmdSeeComments(f)
	cmd.SetArgs([]string{"55", "--review-id", "77"})

	_, err := cmd.ExecuteC()
	require.NoError(t, err)
	assert.Equal(t, "", stderr.String())
	assert.JSONEq(t, "null", stdout.String())
}
