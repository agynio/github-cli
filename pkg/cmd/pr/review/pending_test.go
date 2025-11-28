package review

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	"github.com/cli/cli/v2/api"
	"github.com/cli/cli/v2/internal/ghrepo"
	prshared "github.com/cli/cli/v2/pkg/cmd/pr/shared"
	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/cli/cli/v2/pkg/httpmock"
	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/google/shlex"
	"github.com/stretchr/testify/require"
)

func setupFactory(t *testing.T, reg *httpmock.Registry) (*cmdutil.Factory, *bytes.Buffer) {
	t.Helper()
	httpClient := &http.Client{}
	httpmock.ReplaceTripper(httpClient, reg)

	ios, _, out, _ := iostreams.Test()
	ios.SetStdoutTTY(false)

	factory := &cmdutil.Factory{
		IOStreams:  ios,
		HttpClient: func() (*http.Client, error) { return httpClient, nil },
	}

	return factory, out
}

func TestReviewOpen(t *testing.T) {
	reg := &httpmock.Registry{}
	factory, out := setupFactory(t, reg)

	reg.Register(
		httpmock.REST("POST", "repos/OWNER/REPO/pulls/5/reviews"),
		func(req *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.Equal(t, "{\"body\":\"Initial note\",\"commit_id\":\"abc123\"}", string(bytes.TrimSpace(body)))

			payload := map[string]interface{}{
				"id":           300,
				"state":        "PENDING",
				"body":         "Initial note",
				"commit_id":    "abc123",
				"html_url":     "https://github.com/OWNER/REPO/pull/5#review-300",
				"submitted_at": nil,
				"user": map[string]interface{}{
					"login": "octocat",
				},
			}
			return httpmock.JSONResponse(payload)(req)
		},
	)

	pr := &api.PullRequest{Number: 5}
	repo := ghrepo.New("OWNER", "REPO")
	prshared.StubFinderForRunCommandStyleTests(t, "5", pr, repo)

	cmd := NewCmdReviewOpen(factory, nil)
	argv, err := shlex.Split("5 --body 'Initial note' --commit-sha abc123")
	require.NoError(t, err)
	cmd.SetArgs(argv)

	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	require.NoError(t, cmd.Execute())

	expected := "{\"id\":300,\"state\":\"PENDING\",\"body\":\"Initial note\",\"author\":\"octocat\",\"commit_id\":\"abc123\",\"url\":\"https://github.com/OWNER/REPO/pull/5#review-300\"}\n"
	require.Equal(t, expected, out.String())
}

func TestReviewAddComment(t *testing.T) {
	reg := &httpmock.Registry{}
	factory, out := setupFactory(t, reg)

	reg.Register(
		httpmock.REST("GET", "repos/OWNER/REPO/pulls/7/reviews/88"),
		httpmock.StatusJSONResponse(200, map[string]interface{}{
			"id":        88,
			"state":     "PENDING",
			"commit_id": "abc123",
		}),
	)

	reg.Register(
		httpmock.REST("GET", "repos/OWNER/REPO/commits/abc123"),
		httpmock.StatusJSONResponse(200, map[string]interface{}{
			"sha": "abc123",
			"files": []map[string]interface{}{
				{"filename": "src/app.go", "status": "modified", "patch": nil},
			},
		}),
	)

	reg.Register(
		httpmock.REST("POST", "repos/OWNER/REPO/pulls/7/reviews/88/comments"),
		func(req *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"body":"ping","path":"src/app.go","position":3}`, string(bytes.TrimSpace(body)))

			payload := map[string]interface{}{
				"id":                     901,
				"pull_request_review_id": 88,
				"body":                   "ping",
				"path":                   "src/app.go",
				"html_url":               "https://github.com/OWNER/REPO/pull/7#discussion_r901",
				"created_at":             "2024-10-01T12:00:00Z",
				"updated_at":             "2024-10-01T12:00:00Z",
				"user": map[string]interface{}{
					"login": "octocat",
				},
			}
			return httpmock.JSONResponse(payload)(req)
		},
	)

	pr := &api.PullRequest{Number: 7}
	repo := ghrepo.New("OWNER", "REPO")
	prshared.StubFinderForRunCommandStyleTests(t, "7", pr, repo)

	cmd := NewCmdReviewAddComment(factory, nil)
	argv, err := shlex.Split(`7 --review-id 88 --add-comment '{"path":"src/app.go","position":3,"body":"ping"}'`)
	require.NoError(t, err)
	cmd.SetArgs(argv)

	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	require.NoError(t, cmd.Execute())

	expected := "[{\"id\":901,\"pull_request_review_id\":88,\"body\":\"ping\",\"author\":\"octocat\",\"path\":\"src/app.go\",\"created_at\":\"2024-10-01T12:00:00Z\",\"updated_at\":\"2024-10-01T12:00:00Z\",\"url\":\"https://github.com/OWNER/REPO/pull/7#discussion_r901\"}]\n"
	require.Equal(t, expected, out.String())
}

func TestReviewAddCommentMapsLineToPosition(t *testing.T) {
	reg := &httpmock.Registry{}
	factory, out := setupFactory(t, reg)

	reg.Register(
		httpmock.REST("GET", "repos/OWNER/REPO/pulls/7/reviews/88"),
		httpmock.StatusJSONResponse(200, map[string]interface{}{
			"id":        88,
			"state":     "PENDING",
			"commit_id": "def456",
		}),
	)

	patch := "@@ -1,3 +1,4 @@\n line1\n-line2\n+line2 updated\n+line3\n line4\n"
	reg.Register(
		httpmock.REST("GET", "repos/OWNER/REPO/commits/def456"),
		httpmock.StatusJSONResponse(200, map[string]interface{}{
			"sha": "def456",
			"files": []map[string]interface{}{
				{"filename": "src/app.go", "status": "modified", "patch": patch},
			},
		}),
	)

	reg.Register(
		httpmock.REST("POST", "repos/OWNER/REPO/pulls/7/reviews/88/comments"),
		func(req *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"body":"mapped","path":"src/app.go","position":4}`, string(bytes.TrimSpace(body)))

			return httpmock.StatusJSONResponse(201, map[string]interface{}{
				"id":                     902,
				"pull_request_review_id": 88,
				"body":                   "mapped",
				"path":                   "src/app.go",
				"line":                   3,
				"side":                   "RIGHT",
				"html_url":               "https://github.com/OWNER/REPO/pull/7#discussion_r902",
				"created_at":             "2024-10-01T12:00:00Z",
				"updated_at":             "2024-10-01T12:00:00Z",
				"user": map[string]interface{}{
					"login": "octocat",
				},
			})(req)
		},
	)

	pr := &api.PullRequest{Number: 7}
	repo := ghrepo.New("OWNER", "REPO")
	prshared.StubFinderForRunCommandStyleTests(t, "7", pr, repo)

	cmd := NewCmdReviewAddComment(factory, nil)
	argv, err := shlex.Split(`7 --review-id 88 --add-comment '{"path":"src/app.go","line":3,"side":"RIGHT","body":"mapped"}'`)
	require.NoError(t, err)
	cmd.SetArgs(argv)

	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	require.NoError(t, cmd.Execute())

	expected := "[{\"id\":902,\"pull_request_review_id\":88,\"body\":\"mapped\",\"author\":\"octocat\",\"path\":\"src/app.go\",\"line\":3,\"side\":\"RIGHT\",\"created_at\":\"2024-10-01T12:00:00Z\",\"updated_at\":\"2024-10-01T12:00:00Z\",\"url\":\"https://github.com/OWNER/REPO/pull/7#discussion_r902\"}]\n"
	require.Equal(t, expected, out.String())
}

func TestReviewAddCommentMapsLeftDeletion(t *testing.T) {
	reg := &httpmock.Registry{}
	factory, out := setupFactory(t, reg)

	reg.Register(
		httpmock.REST("GET", "repos/OWNER/REPO/pulls/7/reviews/88"),
		httpmock.StatusJSONResponse(200, map[string]interface{}{
			"id":        88,
			"state":     "PENDING",
			"commit_id": "feedbeef",
		}),
	)

	patch := "@@ -1,2 +1,1 @@\n-lineA\n lineB\n"
	reg.Register(
		httpmock.REST("GET", "repos/OWNER/REPO/commits/feedbeef"),
		httpmock.StatusJSONResponse(200, map[string]interface{}{
			"sha": "feedbeef",
			"files": []map[string]interface{}{
				{"filename": "src/app.go", "status": "modified", "patch": patch},
			},
		}),
	)

	reg.Register(
		httpmock.REST("POST", "repos/OWNER/REPO/pulls/7/reviews/88/comments"),
		func(req *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"body":"removed","path":"src/app.go","position":1}`, string(bytes.TrimSpace(body)))

			return httpmock.StatusJSONResponse(201, map[string]interface{}{
				"id":                     903,
				"pull_request_review_id": 88,
				"body":                   "removed",
				"path":                   "src/app.go",
				"line":                   1,
				"side":                   "LEFT",
				"html_url":               "https://github.com/OWNER/REPO/pull/7#discussion_r903",
				"created_at":             "2024-10-01T12:05:00Z",
				"updated_at":             "2024-10-01T12:05:00Z",
				"user": map[string]interface{}{
					"login": "octocat",
				},
			})(req)
		},
	)

	pr := &api.PullRequest{Number: 7}
	repo := ghrepo.New("OWNER", "REPO")
	prshared.StubFinderForRunCommandStyleTests(t, "7", pr, repo)

	cmd := NewCmdReviewAddComment(factory, nil)
	argv, err := shlex.Split(`7 --review-id 88 --add-comment '{"path":"src/app.go","line":1,"side":"LEFT","body":"removed"}'`)
	require.NoError(t, err)
	cmd.SetArgs(argv)

	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	require.NoError(t, cmd.Execute())

	expected := "[{\"id\":903,\"pull_request_review_id\":88,\"body\":\"removed\",\"author\":\"octocat\",\"path\":\"src/app.go\",\"line\":1,\"side\":\"LEFT\",\"created_at\":\"2024-10-01T12:05:00Z\",\"updated_at\":\"2024-10-01T12:05:00Z\",\"url\":\"https://github.com/OWNER/REPO/pull/7#discussion_r903\"}]\n"
	require.Equal(t, expected, out.String())
}

func TestReviewAddCommentRejectsLeftOnAddition(t *testing.T) {
	reg := &httpmock.Registry{}
	factory, _ := setupFactory(t, reg)

	reg.Register(
		httpmock.REST("GET", "repos/OWNER/REPO/pulls/7/reviews/88"),
		httpmock.StatusJSONResponse(200, map[string]interface{}{
			"id":        88,
			"state":     "PENDING",
			"commit_id": "c0ffee",
		}),
	)

	patch := "@@ -0,0 +1,2 @@\n+added1\n+added2\n"
	reg.Register(
		httpmock.REST("GET", "repos/OWNER/REPO/commits/c0ffee"),
		httpmock.StatusJSONResponse(200, map[string]interface{}{
			"sha": "c0ffee",
			"files": []map[string]interface{}{
				{"filename": "src/app.go", "status": "added", "patch": patch},
			},
		}),
	)

	pr := &api.PullRequest{Number: 7}
	repo := ghrepo.New("OWNER", "REPO")
	prshared.StubFinderForRunCommandStyleTests(t, "7", pr, repo)

	cmd := NewCmdReviewAddComment(factory, nil)
	argv, err := shlex.Split(`7 --review-id 88 --add-comment '{"path":"src/app.go","line":1,"side":"LEFT","body":"whoops"}'`)
	require.NoError(t, err)
	cmd.SetArgs(argv)

	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err = cmd.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "not part of the left side diff at commit c0ffee")
}

func TestReviewAddCommentLineOutsideDiff(t *testing.T) {
	reg := &httpmock.Registry{}
	factory, _ := setupFactory(t, reg)

	reg.Register(
		httpmock.REST("GET", "repos/OWNER/REPO/pulls/7/reviews/88"),
		httpmock.StatusJSONResponse(200, map[string]interface{}{
			"id":        88,
			"state":     "PENDING",
			"commit_id": "def456",
		}),
	)

	patch := "@@ -1,2 +1,2 @@\n-lineA\n+lineB\n"
	reg.Register(
		httpmock.REST("GET", "repos/OWNER/REPO/commits/def456"),
		httpmock.StatusJSONResponse(200, map[string]interface{}{
			"sha": "def456",
			"files": []map[string]interface{}{
				{"filename": "src/app.go", "status": "modified", "patch": patch},
			},
		}),
	)

	pr := &api.PullRequest{Number: 7}
	repo := ghrepo.New("OWNER", "REPO")
	prshared.StubFinderForRunCommandStyleTests(t, "7", pr, repo)

	cmd := NewCmdReviewAddComment(factory, nil)
	argv, err := shlex.Split(`7 --review-id 88 --add-comment '{"path":"src/app.go","line":10,"side":"RIGHT","body":"mapped"}'`)
	require.NoError(t, err)
	cmd.SetArgs(argv)

	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err = cmd.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "line 10 on src/app.go is not part of the right side diff at commit def456")
}

func TestReviewAddCommentRejectsUnknownPath(t *testing.T) {
	reg := &httpmock.Registry{}
	factory, _ := setupFactory(t, reg)

	reg.Register(
		httpmock.REST("GET", "repos/OWNER/REPO/pulls/7/reviews/88"),
		httpmock.StatusJSONResponse(200, map[string]interface{}{
			"id":        88,
			"state":     "PENDING",
			"commit_id": "abc123",
		}),
	)

	reg.Register(
		httpmock.REST("GET", "repos/OWNER/REPO/commits/abc123"),
		httpmock.StatusJSONResponse(200, map[string]interface{}{
			"sha": "abc123",
			"files": []map[string]interface{}{
				{"filename": "src/app.go", "status": "modified", "patch": "@@ -1 +1 @@\n-line\n+line\n"},
			},
		}),
	)

	pr := &api.PullRequest{Number: 7}
	repo := ghrepo.New("OWNER", "REPO")
	prshared.StubFinderForRunCommandStyleTests(t, "7", pr, repo)

	cmd := NewCmdReviewAddComment(factory, nil)
	argv, err := shlex.Split(`7 --review-id 88 --add-comment '{"path":"src/other.go","position":5,"body":"oops"}'`)
	require.NoError(t, err)
	cmd.SetArgs(argv)

	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err = cmd.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), `path "src/other.go" was not changed in commit abc123`)
}

func TestReviewSubmit(t *testing.T) {
	reg := &httpmock.Registry{}
	factory, out := setupFactory(t, reg)

	reg.Register(
		httpmock.REST("POST", "repos/OWNER/REPO/pulls/9/reviews/400/events"),
		func(req *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.Equal(t, "{\"event\":\"REQUEST_CHANGES\",\"body\":\"Needs tweaks\"}", string(bytes.TrimSpace(body)))

			payload := map[string]interface{}{
				"id":           400,
				"state":        "CHANGES_REQUESTED",
				"body":         "Needs tweaks",
				"submitted_at": "2024-10-02T08:00:00Z",
				"html_url":     "https://github.com/OWNER/REPO/pull/9#review-400",
				"user": map[string]interface{}{
					"login": "octocat",
				},
			}
			return httpmock.JSONResponse(payload)(req)
		},
	)

	pr := &api.PullRequest{Number: 9}
	repo := ghrepo.New("OWNER", "REPO")
	prshared.StubFinderForRunCommandStyleTests(t, "9", pr, repo)

	cmd := NewCmdReviewSubmit(factory, nil)
	argv, err := shlex.Split("9 --review-id 400 --event REQUEST_CHANGES --body 'Needs tweaks'")
	require.NoError(t, err)
	cmd.SetArgs(argv)

	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	require.NoError(t, cmd.Execute())

	expected := "{\"id\":400,\"state\":\"CHANGES_REQUESTED\",\"body\":\"Needs tweaks\",\"author\":\"octocat\",\"submitted_at\":\"2024-10-02T08:00:00Z\",\"url\":\"https://github.com/OWNER/REPO/pull/9#review-400\"}\n"
	require.Equal(t, expected, out.String())
}

func TestReviewAbort(t *testing.T) {
	reg := &httpmock.Registry{}
	factory, out := setupFactory(t, reg)

	reg.Register(
		httpmock.REST("DELETE", "repos/OWNER/REPO/pulls/11/reviews/555"),
		httpmock.StatusStringResponse(204, ""),
	)

	pr := &api.PullRequest{Number: 11}
	repo := ghrepo.New("OWNER", "REPO")
	prshared.StubFinderForRunCommandStyleTests(t, "11", pr, repo)

	cmd := NewCmdReviewAbort(factory, nil)
	argv, err := shlex.Split("11 --review-id 555")
	require.NoError(t, err)
	cmd.SetArgs(argv)

	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	require.NoError(t, cmd.Execute())

	expected := "{\"review_id\":555,\"status\":\"aborted\"}\n"
	require.Equal(t, expected, out.String())
}

func TestNormalizePendingCommentInputPreservesBodyWhitespace(t *testing.T) {
	input := commentInput{
		Body:     " ping ",
		Path:     "src/app.go",
		Position: intPtr(3),
	}

	result, err := normalizePendingCommentInput(input)
	require.NoError(t, err)
	require.Equal(t, " ping ", result.Body)
	require.NotNil(t, result.Position)
	require.Equal(t, 3, *result.Position)
}

func TestNormalizePendingCommentInputRejectsPathWhitespace(t *testing.T) {
	input := commentInput{
		Body:     "ping",
		Path:     " src/app.go ",
		Position: intPtr(3),
	}

	_, err := normalizePendingCommentInput(input)
	require.ErrorContains(t, err, "comment path cannot include leading or trailing whitespace")
}

func TestNormalizePendingCommentInputRejectsSideWhitespace(t *testing.T) {
	line := 5
	side := " right"
	input := commentInput{
		Body: "ping",
		Path: "src/app.go",
		Line: &line,
		Side: &side,
	}

	_, err := normalizePendingCommentInput(input)
	require.ErrorContains(t, err, "`side` cannot include leading or trailing whitespace")
}

func TestNormalizePendingCommentInputRejectsRanges(t *testing.T) {
	line := 10
	startLine := 4
	side := "right"
	startSide := "left"
	input := commentInput{
		Body:      "ping",
		Path:      "src/app.go",
		Line:      &line,
		Side:      &side,
		StartLine: &startLine,
		StartSide: &startSide,
	}

	_, err := normalizePendingCommentInput(input)
	require.ErrorContains(t, err, "line ranges are not supported")
}

func intPtr(v int) *int {
	return &v
}
