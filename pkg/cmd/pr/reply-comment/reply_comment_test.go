package reply_comment

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
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
	ghapi "github.com/cli/go-gh/v2/pkg/api"
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

func TestReplyComment_AutoSubmitPending(t *testing.T) {
	reg := &httpmock.Registry{}
	defer reg.Verify(t)

	pendingErr := ghapi.HTTPError{
		Message: "Validation Failed",
		Errors: []ghapi.HTTPErrorItem{
			{Message: "\"user_id\" can only have one pending review per pull request."},
		},
	}
	reg.Register(
		httpmock.WithHost(httpmock.REST("POST", "repos/ORG/REPO/pulls/123/comments/456/replies"), "api.github.com"),
		httpmock.JSONErrorResponse(422, pendingErr),
	)

	reviews := []map[string]interface{}{
		{
			"id":           9001,
			"state":        "PENDING",
			"user":         map[string]interface{}{"login": "octocat"},
			"submitted_at": nil,
		},
	}
	reg.Register(
		httpmock.WithHost(httpmock.QueryMatcher("GET", "repos/ORG/REPO/pulls/123/reviews", url.Values{"per_page": {"100"}}), "api.github.com"),
		httpmock.JSONResponse(reviews),
	)

	reg.Register(
		httpmock.WithHost(httpmock.REST("GET", "user"), "api.github.com"),
		httpmock.JSONResponse(map[string]string{"login": "octocat"}),
	)

	reg.Register(
		httpmock.WithHost(httpmock.REST("POST", "repos/ORG/REPO/pulls/123/reviews/9001/events"), "api.github.com"),
		func(req *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			var payload map[string]string
			require.NoError(t, json.Unmarshal(body, &payload))
			assert.Equal(t, "COMMENT", payload["event"])
			assert.Equal(t, autoSubmitSummary, payload["body"])
			return httpmock.JSONResponse(map[string]interface{}{"id": 9001})(req)
		},
	)

	reply := map[string]interface{}{
		"id":             333,
		"in_reply_to_id": 456,
		"body":           "Thanks!",
		"user":           map[string]interface{}{"login": "octocat"},
		"created_at":     time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC),
	}
	reg.Register(
		httpmock.WithHost(httpmock.REST("POST", "repos/ORG/REPO/pulls/123/comments/456/replies"), "api.github.com"),
		httpmock.JSONResponse(reply),
	)

	repo, err := ghrepo.FromFullName("ORG/REPO")
	require.NoError(t, err)
	f, stdout, stderr := newTestFactory(t, reg, repo, nil)
	cmd := NewCmdReplyComment(f)
	cmd.SetArgs([]string{"123", "--comment-id", "456", "--body", "Thanks!", "--auto-submit-pending"})

	_, err = cmd.ExecuteC()
	require.NoError(t, err)
	assert.Equal(t, "", stderr.String())
	assert.JSONEq(t, `{"id":333,"in_reply_to_id":456,"body":"Thanks!","user":{"login":"octocat"},"created_at":"2024-05-06T07:08:09Z"}`, stdout.String())

	reqCount := 0
	for _, req := range reg.Requests {
		if req.URL.Path == "/repos/ORG/REPO/pulls/123/comments/456/replies" {
			reqCount++
		}
	}
	assert.Equal(t, 2, reqCount, "expected two requests to reply endpoint")
}

func TestReplyComment_RepoOverrideWithoutGit(t *testing.T) {
	reg := &httpmock.Registry{}
	defer reg.Verify(t)

	reg.Register(
		httpmock.WithHost(httpmock.REST("POST", "repos/octo/demo/pulls/10/comments/50/replies"), "api.github.com"),
		httpmock.JSONResponse(map[string]interface{}{"id": 99}),
	)

	noRepoErr := errors.New("no repository")
	f, stdout, stderr := newTestFactory(t, reg, nil, noRepoErr)
	f.BaseRepo = cmdutil.OverrideBaseRepoFunc(f, "octo/demo")

	cmd := NewCmdReplyComment(f)
	cmd.SetArgs([]string{"10", "--comment-id", "50", "--body", "Thanks"})

	_, err := cmd.ExecuteC()
	require.NoError(t, err)
	assert.Equal(t, "", stderr.String())

	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &payload))
	assert.Equal(t, float64(99), payload["id"])
}
