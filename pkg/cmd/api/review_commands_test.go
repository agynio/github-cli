package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

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

func TestSeeComments_Deprecated(t *testing.T) {
	f, _, stderr := newTestFactory(t, nil)
	cmd := newSeeCommentsCmd(f)

	_, err := cmd.ExecuteC()
	require.Error(t, err)
	assert.Equal(t, cmdutil.SilentError, err)
	assert.Contains(t, stderr.String(), "gh pr see-comments")
}

func TestReplyComment_Deprecated(t *testing.T) {
	f, _, stderr := newTestFactory(t, nil)
	cmd := newReplyCommentCmd(f)

	_, err := cmd.ExecuteC()
	require.Error(t, err)
	assert.Equal(t, cmdutil.SilentError, err)
	assert.Contains(t, stderr.String(), "gh pr reply-comment")
}

func TestReviewOpen_Deprecated(t *testing.T) {
	f, _, stderr := newTestFactory(t, nil)
	cmd := newReviewCmd(f)
	cmd.SetArgs([]string{"open"})

	_, err := cmd.ExecuteC()
	require.Error(t, err)
	assert.Equal(t, cmdutil.SilentError, err)
	assert.Contains(t, stderr.String(), "gh pr review open")
}

func TestReviewCreate(t *testing.T) {
	reg := &httpmock.Registry{}
	defer reg.Verify(t)

	reg.Register(
		httpmock.WithHost(httpmock.REST("GET", "repos/ORG/REPO/pulls/8"), "api.github.com"),
		httpmock.JSONResponse(map[string]interface{}{
			"head": map[string]interface{}{"sha": "def456"},
		}),
	)

	reg.Register(
		httpmock.WithHost(httpmock.REST("POST", "repos/ORG/REPO/pulls/8/reviews"), "api.github.com"),
		func(req *http.Request) (*http.Response, error) {
			data, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			var payload map[string]interface{}
			require.NoError(t, json.Unmarshal(data, &payload))
			assert.Equal(t, "def456", payload["commit_id"])
			assert.Equal(t, "APPROVE", payload["event"])
			assert.Equal(t, "Great work", payload["body"])
			assert.Nil(t, payload["comments"])

			return httpmock.JSONResponse(map[string]interface{}{
				"id":           77,
				"state":        "APPROVED",
				"submitted_at": "2024-06-02T09:10:11Z",
			})(req)
		},
	)

	f, stdout, stderr := newTestFactory(t, reg)
	cmd := newReviewCmd(f)
	cmd.SetArgs([]string{"create", "--org", "ORG", "--repo", "REPO", "--pr", "8", "--event", "approve", "--body", "Great work"})

	_, err := cmd.ExecuteC()
	require.NoError(t, err)
	assert.Equal(t, "", stderr.String())
	assert.JSONEq(t, `{"id":77,"state":"APPROVED","submitted_at":"2024-06-02T09:10:11Z"}`, stdout.String())
}
