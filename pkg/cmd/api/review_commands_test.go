package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/cli/cli/v2/internal/config"
	"github.com/cli/cli/v2/internal/gh"
	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/cli/cli/v2/pkg/httpmock"
	"github.com/cli/cli/v2/pkg/iostreams"
	ghapi "github.com/cli/go-gh/v2/pkg/api"
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
	cmd := newSeeCommentsCmd(f)
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

func TestReplyComment_AutoSubmitPending(t *testing.T) {
	reg := &httpmock.Registry{}
	defer reg.Verify(t)

	// First attempt results in 422 pending review error.
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

	// Pending reviews lookup.
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

	// Resolve viewer login.
	reg.Register(
		httpmock.WithHost(httpmock.REST("GET", "user"), "api.github.com"),
		httpmock.JSONResponse(map[string]string{"login": "octocat"}),
	)

	// Submit pending review.
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

	// Successful retry.
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

	f, stdout, stderr := newTestFactory(t, reg)
	cmd := newReplyCommentCmd(f)
	cmd.SetArgs([]string{"--org", "ORG", "--repo", "REPO", "--pr", "123", "--comment-id", "456", "--body", "Thanks!", "--auto-submit-pending"})

	_, err := cmd.ExecuteC()
	require.NoError(t, err)
	assert.Equal(t, "", stderr.String())
	assert.JSONEq(t, `{"id":333,"in_reply_to_id":456,"body":"Thanks!","user":{"login":"octocat"},"created_at":"2024-05-06T07:08:09Z"}`, stdout.String())

	// ensure reply endpoint hit twice (initial 422 + retry)
	reqCount := 0
	for _, req := range reg.Requests {
		if req.URL.Path == "/repos/ORG/REPO/pulls/123/comments/456/replies" {
			reqCount++
		}
	}
	assert.Equal(t, 2, reqCount, "expected two requests to reply endpoint")
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

	f, stdout, stderr := newTestFactory(t, reg)
	cmd := newReviewCmd(f)
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

	f, stdout, stderr := newTestFactory(t, reg)
	cmd := newReviewCmd(f)
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

	f, stdout, stderr := newTestFactory(t, reg)
	cmd := newReviewCmd(f)
	cmd.SetArgs([]string{"submit", "--org", "ORG", "--repo", "REPO", "--pr", "5", "--review-id", "RV123", "--event", "approve"})

	_, err := cmd.ExecuteC()
	require.NoError(t, err)
	assert.Equal(t, "", stderr.String())
	assert.JSONEq(t, `{"id":"RV123","state":"COMMENTED","submitted_at":"2024-05-01T12:00:00Z"}`, stdout.String())
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
