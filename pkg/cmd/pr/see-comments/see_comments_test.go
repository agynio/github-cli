package see_comments

import (
	"bytes"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/cli/cli/v2/internal/config"
	"github.com/cli/cli/v2/internal/gh"
	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/cli/cli/v2/pkg/httpmock"
	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestFactory(t *testing.T, rt http.RoundTripper) (*cmdutil.Factory, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	ios, _, stdout, stderr := iostreams.Test()
	ios.SetStdoutTTY(false)
	ios.SetStdinTTY(false)
	ios.SetStderrTTY(false)

	cfg := config.NewBlankConfig()
	cfg.Authentication().SetDefaultHost("github.com", "default")

	return &cmdutil.Factory{
		IOStreams: ios,
		HttpClient: func() (*http.Client, error) {
			return &http.Client{Transport: rt}, nil
		},
		Config: func() (gh.Config, error) {
			return cfg, nil
		},
	}, stdout, stderr
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

	f, stdout, stderr := newTestFactory(t, reg)
	cmd := NewCmdSeeComments(f)
	cmd.SetArgs([]string{"--org", "ORG", "--repo", "REPO", "--pr", "123", "--review-id", "456"})

	_, err := cmd.ExecuteC()
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
