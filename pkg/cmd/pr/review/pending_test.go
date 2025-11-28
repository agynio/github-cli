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

	expected := "{\"id\":300,\"state\":\"PENDING\",\"body\":\"Initial note\",\"author\":\"octocat\",\"commitId\":\"abc123\",\"url\":\"https://github.com/OWNER/REPO/pull/5#review-300\"}\n"
	require.Equal(t, expected, out.String())
}

func TestReviewAddComment(t *testing.T) {
	reg := &httpmock.Registry{}
	factory, out := setupFactory(t, reg)

	reg.Register(
		httpmock.REST("GET", "repos/OWNER/REPO/pulls/7/reviews/88"),
		httpmock.JSONResponse(map[string]interface{}{
			"id":                 88,
			"node_id":            "PRR_node",
			"state":              "PENDING",
			"commit_id":          "abc123",
			"html_url":           "https://github.com/OWNER/REPO/pull/7#review-88",
			"author_association": "OWNER",
		}),
	)

	reg.Register(
		httpmock.GraphQL(`query PullRequestFilePaths\b`),
		httpmock.GraphQLQuery(`{"data":{"repository":{"pullRequest":{"files":{"nodes":[{"path":"src/app.go"}],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}}`, func(_ string, variables map[string]interface{}) {
			require.Equal(t, float64(100), variables["perPage"])
		}),
	)

	reg.Register(
		httpmock.REST("POST", "repos/OWNER/REPO/pulls/7/reviews/88/comments"),
		func(req *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.Equal(t, "{\"body\":\"ping\",\"commit_id\":\"abc123\",\"path\":\"src/app.go\",\"position\":3}", string(bytes.TrimSpace(body)))

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
	argv, err := shlex.Split("7 --review-id 88 --add-comment '{\"path\":\"src/app.go\",\"position\":3,\"body\":\"ping\"}'")
	require.NoError(t, err)
	cmd.SetArgs(argv)

	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	require.NoError(t, cmd.Execute())

	expected := "[{\"id\":901,\"pullRequestReviewId\":88,\"body\":\"ping\",\"author\":\"octocat\",\"path\":\"src/app.go\",\"createdAt\":\"2024-10-01T12:00:00Z\",\"updatedAt\":\"2024-10-01T12:00:00Z\",\"url\":\"https://github.com/OWNER/REPO/pull/7#discussion_r901\"}]\n"
	require.Equal(t, expected, out.String())
}

func TestReviewAddCommentRange(t *testing.T) {
	reg := &httpmock.Registry{}
	factory, out := setupFactory(t, reg)

	reg.Register(
		httpmock.REST("GET", "repos/OWNER/REPO/pulls/7/reviews/88"),
		httpmock.JSONResponse(map[string]interface{}{
			"id":        88,
			"node_id":   "PRR_node",
			"state":     "PENDING",
			"commit_id": "abc123",
			"html_url":  "https://github.com/OWNER/REPO/pull/7#review-88",
		}),
	)

	reg.Register(
		httpmock.GraphQL(`query PullRequestFilePaths\b`),
		httpmock.GraphQLQuery(`{"data":{"repository":{"pullRequest":{"files":{"nodes":[{"path":"src/app.go"}],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}}`, func(_ string, variables map[string]interface{}) {
			require.Equal(t, float64(100), variables["perPage"])
		}),
	)

	reg.Register(
		httpmock.REST("POST", "repos/OWNER/REPO/pulls/7/reviews/88/comments"),
		func(req *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"body":"range","commit_id":"abc123","path":"src/app.go","line":12,"side":"RIGHT","start_line":3,"start_side":"RIGHT"}`, string(body))

			payload := map[string]interface{}{
				"id":                     902,
				"pull_request_review_id": 88,
				"body":                   "range",
				"path":                   "src/app.go",
				"line":                   12,
				"side":                   "RIGHT",
				"start_line":             3,
				"start_side":             "RIGHT",
				"html_url":               "https://github.com/OWNER/REPO/pull/7#discussion_r902",
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
	argv, err := shlex.Split(`7 --review-id 88 --add-comment '{"path":"src/app.go","line":12,"side":"RIGHT","start_line":3,"start_side":"RIGHT","body":"range"}'`)
	require.NoError(t, err)
	cmd.SetArgs(argv)

	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	require.NoError(t, cmd.Execute())

	expected := "[{\"id\":902,\"pullRequestReviewId\":88,\"body\":\"range\",\"author\":\"octocat\",\"path\":\"src/app.go\",\"line\":12,\"side\":\"RIGHT\",\"startLine\":3,\"startSide\":\"RIGHT\",\"createdAt\":\"2024-10-01T12:00:00Z\",\"updatedAt\":\"2024-10-01T12:00:00Z\",\"url\":\"https://github.com/OWNER/REPO/pull/7#discussion_r902\"}]\n"
	require.Equal(t, expected, out.String())
}

func TestReviewAddCommentStartSideWithoutStartLine(t *testing.T) {
	reg := &httpmock.Registry{}
	factory, _ := setupFactory(t, reg)

	reg.Register(
		httpmock.REST("GET", "repos/OWNER/REPO/pulls/7/reviews/88"),
		httpmock.JSONResponse(map[string]interface{}{
			"id":        88,
			"node_id":   "PRR_node",
			"state":     "PENDING",
			"commit_id": "abc123",
		}),
	)

	reg.Register(
		httpmock.GraphQL(`query PullRequestFilePaths\b`),
		httpmock.GraphQLQuery(`{"data":{"repository":{"pullRequest":{"files":{"nodes":[{"path":"src/app.go"}],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}}`, func(_ string, variables map[string]interface{}) {
			require.Equal(t, float64(100), variables["perPage"])
		}),
	)

	pr := &api.PullRequest{Number: 7}
	repo := ghrepo.New("OWNER", "REPO")
	prshared.StubFinderForRunCommandStyleTests(t, "7", pr, repo)

	cmd := NewCmdReviewAddComment(factory, nil)
	argv, err := shlex.Split(`7 --review-id 88 --add-comment '{"path":"src/app.go","line":12,"side":"RIGHT","start_side":"RIGHT","body":"range"}'`)
	require.NoError(t, err)
	cmd.SetArgs(argv)

	err = cmd.Execute()
	require.Error(t, err)
	require.EqualError(t, err, "comment 1: `start_line` is required when `start_side` is provided")
}

func TestReviewAddCommentRangeNotAllowedWithPosition(t *testing.T) {
	reg := &httpmock.Registry{}
	factory, _ := setupFactory(t, reg)

	reg.Register(
		httpmock.REST("GET", "repos/OWNER/REPO/pulls/7/reviews/88"),
		httpmock.JSONResponse(map[string]interface{}{
			"id":        88,
			"node_id":   "PRR_node",
			"state":     "PENDING",
			"commit_id": "abc123",
		}),
	)

	reg.Register(
		httpmock.GraphQL(`query PullRequestFilePaths\b`),
		httpmock.GraphQLQuery(`{"data":{"repository":{"pullRequest":{"files":{"nodes":[{"path":"src/app.go"}],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}}`, func(_ string, variables map[string]interface{}) {
			require.Equal(t, float64(100), variables["perPage"])
		}),
	)

	pr := &api.PullRequest{Number: 7}
	repo := ghrepo.New("OWNER", "REPO")
	prshared.StubFinderForRunCommandStyleTests(t, "7", pr, repo)

	cmd := NewCmdReviewAddComment(factory, nil)
	argv, err := shlex.Split(`7 --review-id 88 --add-comment '{"path":"src/app.go","position":4,"start_line":3,"start_side":"RIGHT","body":"range"}'`)
	require.NoError(t, err)
	cmd.SetArgs(argv)

	err = cmd.Execute()
	require.Error(t, err)
	require.EqualError(t, err, "comment 1: `start_line` and `start_side` cannot be used with `position`")
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

	expected := "{\"id\":400,\"state\":\"CHANGES_REQUESTED\",\"body\":\"Needs tweaks\",\"author\":\"octocat\",\"submittedAt\":\"2024-10-02T08:00:00Z\",\"url\":\"https://github.com/OWNER/REPO/pull/9#review-400\"}\n"
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

	expected := "{\"reviewId\":555,\"status\":\"aborted\"}\n"
	require.Equal(t, expected, out.String())
}
