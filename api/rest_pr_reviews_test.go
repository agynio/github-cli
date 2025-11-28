package api

import (
	"io"
	"net/http"
	"testing"

	"github.com/cli/cli/v2/internal/ghrepo"
	"github.com/cli/cli/v2/pkg/httpmock"
	"github.com/stretchr/testify/require"
)

func TestCreatePendingReviewREST(t *testing.T) {
	reg := &httpmock.Registry{}
	defer reg.Verify(t)

	reg.Register(
		httpmock.REST("POST", "repos/OWNER/REPO/pulls/1/reviews"),
		func(req *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"body":"Initial notes","commit_id":"abc123"}`, string(body))

			return httpmock.StatusJSONResponse(201, map[string]interface{}{
				"id":        321,
				"node_id":   "MDExOlB1bGxSZXF1ZXN0UmV2aWV3MzIx",
				"body":      "Initial notes",
				"state":     "PENDING",
				"commit_id": "abc123",
				"html_url":  "https://example.com/reviews/321",
				"url":       "https://api.github.com/reviews/321",
				"user": map[string]interface{}{
					"login":   "monalisa",
					"id":      1,
					"node_id": "MDQ6VXNlcjE=",
				},
			})(req)
		},
	)

	client := newTestClient(reg)
	repo := ghrepo.New("OWNER", "REPO")
	review, err := CreatePendingReviewREST(client, repo, 1, PendingReviewInput{Body: "Initial notes", CommitID: "abc123"})
	require.NoError(t, err)
	require.Equal(t, int64(321), review.ID)
	require.Equal(t, "Initial notes", review.Body)
	require.NotNil(t, review.User)
	require.Equal(t, "monalisa", review.User.Login)
}

func TestAddPendingReviewCommentREST(t *testing.T) {
	reg := &httpmock.Registry{}
	defer reg.Verify(t)

	reg.Register(
		httpmock.REST("POST", "repos/OWNER/REPO/pulls/7/reviews/99/comments"),
		func(req *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"body":"Looks good","path":"file.go","line":123,"side":"RIGHT"}`, string(body))

			return httpmock.StatusJSONResponse(201, map[string]interface{}{
				"id":                     555,
				"node_id":                "MDEyOklzc3VlQ29tbWVudDU1NQ==",
				"body":                   "Looks good",
				"path":                   "file.go",
				"line":                   123,
				"side":                   "RIGHT",
				"pull_request_review_id": 99,
				"created_at":             "2020-01-01T00:00:00Z",
				"updated_at":             "2020-01-01T00:00:00Z",
				"user": map[string]interface{}{
					"login":   "monalisa",
					"id":      1,
					"node_id": "MDQ6VXNlcjE=",
				},
			})(req)
		},
	)

	client := newTestClient(reg)
	repo := ghrepo.New("OWNER", "REPO")
	comment, err := AddPendingReviewCommentREST(client, repo, 7, 99, PendingReviewCommentInput{
		Body: "Looks good",
		Path: "file.go",
		Line: intPtr(123),
		Side: strPtr("RIGHT"),
	})
	require.NoError(t, err)
	require.Equal(t, int64(555), comment.ID)
	require.Equal(t, "file.go", comment.Path)
	require.NotNil(t, comment.Line)
	require.Equal(t, 123, *comment.Line)
}

func TestReplyToReviewCommentREST(t *testing.T) {
	reg := &httpmock.Registry{}
	defer reg.Verify(t)

	reg.Register(
		httpmock.REST("POST", "repos/OWNER/REPO/pulls/7/comments/42/replies"),
		func(req *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"body":"Thanks!"}`, string(body))

			return httpmock.StatusJSONResponse(201, map[string]interface{}{
				"id":                     777,
				"pull_request_review_id": 15,
				"in_reply_to_id":         42,
				"body":                   "Thanks!",
				"path":                   "file.go",
				"created_at":             "2020-01-02T00:00:00Z",
				"updated_at":             "2020-01-02T00:00:00Z",
				"user": map[string]interface{}{
					"login":   "octocat",
					"id":      2,
					"node_id": "MDQ6VXNlcjI=",
				},
			})(req)
		},
	)

	client := newTestClient(reg)
	repo := ghrepo.New("OWNER", "REPO")
	comment, err := ReplyToReviewCommentREST(client, repo, 7, 42, "Thanks!")
	require.NoError(t, err)
	require.Equal(t, int64(777), comment.ID)
	require.NotNil(t, comment.InReplyToID)
	require.Equal(t, int64(42), *comment.InReplyToID)
}

func TestSubmitPendingReviewREST(t *testing.T) {
	reg := &httpmock.Registry{}
	defer reg.Verify(t)

	reg.Register(
		httpmock.REST("POST", "repos/OWNER/REPO/pulls/9/reviews/88/events"),
		func(req *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"event":"APPROVE","body":"ship it"}`, string(body))

			return httpmock.StatusJSONResponse(200, map[string]interface{}{
				"id":       88,
				"state":    "APPROVED",
				"body":     "ship it",
				"html_url": "https://example.com/reviews/88",
				"user": map[string]interface{}{
					"login":   "monalisa",
					"id":      1,
					"node_id": "MDQ6VXNlcjE=",
				},
			})(req)
		},
	)

	client := newTestClient(reg)
	repo := ghrepo.New("OWNER", "REPO")
	review, err := SubmitPendingReviewREST(client, repo, 9, 88, SubmitReviewInput{Event: "APPROVE", Body: "ship it"})
	require.NoError(t, err)
	require.Equal(t, "APPROVED", review.State)
}

func TestDeletePendingReviewREST(t *testing.T) {
	reg := &httpmock.Registry{}
	defer reg.Verify(t)

	reg.Register(
		httpmock.REST("DELETE", "repos/OWNER/REPO/pulls/5/reviews/33"),
		httpmock.StatusJSONResponse(204, nil),
	)

	client := newTestClient(reg)
	repo := ghrepo.New("OWNER", "REPO")
	require.NoError(t, DeletePendingReviewREST(client, repo, 5, 33))
}

func TestListReviewCommentsREST(t *testing.T) {
	reg := &httpmock.Registry{}
	defer reg.Verify(t)

	reg.Register(
		httpmock.REST("GET", "repos/OWNER/REPO/pulls/3/reviews/12/comments"),
		func(req *http.Request) (*http.Response, error) {
			require.Equal(t, "page=2&per_page=50", req.URL.RawQuery)
			return httpmock.StatusJSONResponse(200, []map[string]interface{}{
				{
					"id":                     2001,
					"pull_request_review_id": 12,
					"body":                   "nit",
					"path":                   "file.go",
					"created_at":             "2020-01-03T00:00:00Z",
					"updated_at":             "2020-01-03T00:00:00Z",
					"user": map[string]interface{}{
						"login":   "hubot",
						"id":      2,
						"node_id": "MDQ6VXNlcjI=",
					},
				},
			})(req)
		},
	)

	client := newTestClient(reg)
	repo := ghrepo.New("OWNER", "REPO")
	comments, err := ListReviewCommentsREST(client, repo, 3, 12, ReviewCommentsListParams{PerPage: 50, Page: 2})
	require.NoError(t, err)
	require.Len(t, comments, 1)
	require.Equal(t, int64(2001), comments[0].ID)
}

func TestListPullRequestReviewsREST(t *testing.T) {
	reg := &httpmock.Registry{}
	defer reg.Verify(t)

	reg.Register(
		httpmock.REST("GET", "repos/OWNER/REPO/pulls/2/reviews"),
		func(req *http.Request) (*http.Response, error) {
			require.Equal(t, "per_page=100", req.URL.RawQuery)
			responder := httpmock.StatusJSONResponse(200, []map[string]interface{}{
				{
					"id":       1,
					"state":    "COMMENTED",
					"html_url": "https://example.com/reviews/1",
					"user": map[string]interface{}{
						"login":   "octocat",
						"id":      3,
						"node_id": "MDQ6VXNlcjM=",
					},
				},
			})
			return httpmock.WithHeader(responder, "Link", `<https://api.github.com/repos/OWNER/REPO/pulls/2/reviews?per_page=100&page=2>; rel="next"`)(req)
		},
	)

	reg.Register(
		httpmock.WithHost(httpmock.REST("GET", "repos/OWNER/REPO/pulls/2/reviews"), "api.github.com"),
		func(req *http.Request) (*http.Response, error) {
			require.Equal(t, "per_page=100&page=2", req.URL.RawQuery)
			return httpmock.StatusJSONResponse(200, []map[string]interface{}{
				{
					"id":       2,
					"state":    "APPROVED",
					"html_url": "https://example.com/reviews/2",
					"user": map[string]interface{}{
						"login":   "hubot",
						"id":      4,
						"node_id": "MDQ6VXNlcjQ=",
					},
				},
			})(req)
		},
	)

	client := newTestClient(reg)
	repo := ghrepo.New("OWNER", "REPO")
	reviews, err := ListPullRequestReviewsREST(client, repo, 2)
	require.NoError(t, err)
	require.Len(t, reviews, 2)
	require.Equal(t, int64(1), reviews[0].ID)
	require.Equal(t, int64(2), reviews[1].ID)
}

func intPtr(v int) *int {
	return &v
}

func strPtr(v string) *string {
	return &v
}
