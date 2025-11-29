package review

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

func newPendingTestFactory(t *testing.T, rt http.RoundTripper) (*cmdutil.Factory, *bytes.Buffer, *bytes.Buffer) {
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

func TestReviewOpen(t *testing.T) {
	reg := &httpmock.Registry{}
	defer reg.Verify(t)

	reg.Register(
		httpmock.WithHost(httpmock.GraphQL(`query PullRequestID`), "api.github.com"),
		httpmock.JSONResponse(map[string]interface{}{
			"data": map[string]interface{}{
				"repository": map[string]interface{}{
					"pullRequest": map[string]interface{}{"id": "PRID"},
				},
			},
		}),
	)

	reg.Register(
		httpmock.WithHost(httpmock.REST("GET", "repos/ORG/REPO/pulls/42"), "api.github.com"),
		httpmock.JSONResponse(map[string]interface{}{
			"head": map[string]interface{}{"sha": "abc123"},
		}),
	)

	reg.Register(
		httpmock.WithHost(httpmock.GraphQL(`mutation AddPullRequestReview`), "api.github.com"),
		httpmock.JSONResponse(map[string]interface{}{
			"data": map[string]interface{}{
				"addPullRequestReview": map[string]interface{}{
					"pullRequestReview": map[string]interface{}{
						"id":          "RV_review",
						"state":       "PENDING",
						"submittedAt": nil,
					},
				},
			},
		}),
	)

	f, stdout, stderr := newPendingTestFactory(t, reg)
	cmd := NewCmdReview(f, nil)
	cmd.SetArgs([]string{"open", "--org", "ORG", "--repo", "REPO", "--pr", "42"})

	_, err := cmd.ExecuteC()
	require.NoError(t, err)
	assert.Equal(t, "", stderr.String())
	assert.JSONEq(t, `{"id":"RV_review","state":"PENDING","submitted_at":null}`, stdout.String())
}

func TestReviewAdd(t *testing.T) {
	reg := &httpmock.Registry{}
	defer reg.Verify(t)

	reg.Register(
		httpmock.WithHost(httpmock.GraphQL(`mutation AddPullRequestReviewThread`), "api.github.com"),
		func(req *http.Request) (*http.Response, error) {
			data, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			var payload struct {
				Variables map[string]map[string]interface{} `json:"variables"`
			}
			require.NoError(t, json.Unmarshal(data, &payload))
			input := payload.Variables["input"]
			assert.Equal(t, "RV123", input["pullRequestReviewId"])
			assert.Equal(t, "file.go", input["path"])
			assert.EqualValues(t, 7, input["line"])
			assert.Equal(t, "RIGHT", input["side"])
			assert.Equal(t, "note", input["body"])

			return httpmock.JSONResponse(map[string]interface{}{
				"data": map[string]interface{}{
					"addPullRequestReviewThread": map[string]interface{}{
						"thread": map[string]interface{}{
							"id":         "THREAD123",
							"path":       "file.go",
							"isOutdated": false,
						},
					},
				},
			})(req)
		},
	)

	f, stdout, stderr := newPendingTestFactory(t, reg)
	cmd := NewCmdReview(f, nil)
	cmd.SetArgs([]string{"add", "--org", "ORG", "--repo", "REPO", "--pr", "9", "--review-id", "RV123", "--path", "file.go", "--line", "7", "--side", "right", "--body", "note"})

	_, err := cmd.ExecuteC()
	require.NoError(t, err)
	assert.Equal(t, "", stderr.String())
	assert.JSONEq(t, `{"id":"THREAD123","path":"file.go","is_outdated":false}`, stdout.String())
}

func TestReviewSubmit(t *testing.T) {
	reg := &httpmock.Registry{}
	defer reg.Verify(t)

	reg.Register(
		httpmock.WithHost(httpmock.GraphQL(`mutation SubmitPullRequestReview`), "api.github.com"),
		httpmock.JSONResponse(map[string]interface{}{
			"data": map[string]interface{}{
				"submitPullRequestReview": map[string]interface{}{
					"pullRequestReview": map[string]interface{}{
						"id":          "RV123",
						"state":       "COMMENTED",
						"submittedAt": "2024-05-01T12:00:00Z",
					},
				},
			},
		}),
	)

	f, stdout, stderr := newPendingTestFactory(t, reg)
	cmd := NewCmdReview(f, nil)
	cmd.SetArgs([]string{"submit", "--org", "ORG", "--repo", "REPO", "--pr", "5", "--review-id", "RV123", "--event", "approve"})

	_, err := cmd.ExecuteC()
	require.NoError(t, err)
	assert.Equal(t, "", stderr.String())
	assert.JSONEq(t, `{"id":"RV123","state":"COMMENTED","submitted_at":"2024-05-01T12:00:00Z"}`, stdout.String())
}
