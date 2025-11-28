package reviewcomments

import (
	"bytes"
	"net/http"
	"net/url"
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

func TestViewByReviewID_JSON(t *testing.T) {
	reg := &httpmock.Registry{}
	httpClient := &http.Client{}
	httpmock.ReplaceTripper(httpClient, reg)

	values := url.Values{}
	values.Set("per_page", "30")
	values.Set("page", "1")
	reg.Register(
		httpmock.QueryMatcher("GET", "repos/OWNER/REPO/pulls/123/reviews/456/comments", values),
		httpmock.JSONResponse([]map[string]interface{}{
			{
				"id":                     1,
				"pull_request_review_id": 456,
				"body":                   "Looks good",
				"path":                   "src/main.go",
				"position":               7,
				"commit_id":              "abc",
				"original_commit_id":     "abc",
				"html_url":               "https://github.com/OWNER/REPO/pull/123#discussion_r1",
				"created_at":             "2024-10-01T12:00:00Z",
				"updated_at":             "2024-10-01T12:05:00Z",
				"user": map[string]interface{}{
					"login": "octocat",
				},
			},
		}),
	)

	pr := &api.PullRequest{Number: 123}
	repo := ghrepo.New("OWNER", "REPO")
	prshared.StubFinderForRunCommandStyleTests(t, "123", pr, repo)

	ios, _, stdout, _ := iostreams.Test()
	ios.SetStdoutTTY(false)

	factory := &cmdutil.Factory{
		IOStreams:  ios,
		HttpClient: func() (*http.Client, error) { return httpClient, nil },
	}

	cmd := NewCmdView(factory, nil)
	argv, err := shlex.Split("123 --review-id 456")
	require.NoError(t, err)
	cmd.SetArgs(argv)

	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	require.NoError(t, cmd.Execute())

	expected := "[{\"id\":1,\"pullRequestReviewId\":456,\"body\":\"Looks good\",\"author\":\"octocat\",\"path\":\"src/main.go\",\"position\":7,\"commitId\":\"abc\",\"originalCommitId\":\"abc\",\"createdAt\":\"2024-10-01T12:00:00Z\",\"updatedAt\":\"2024-10-01T12:05:00Z\",\"url\":\"https://github.com/OWNER/REPO/pull/123#discussion_r1\"}]\n"
	require.Equal(t, expected, stdout.String())
}

func TestViewLatestSelectsSubmittedReview(t *testing.T) {
	reg := &httpmock.Registry{}
	httpClient := &http.Client{}
	httpmock.ReplaceTripper(httpClient, reg)

	reg.Register(
		httpmock.QueryMatcher("GET", "repos/OWNER/REPO/pulls/99/reviews", url.Values{"per_page": []string{"100"}}),
		httpmock.JSONResponse([]map[string]interface{}{
			{
				"id":           10,
				"state":        "PENDING",
				"submitted_at": nil,
			},
			{
				"id":           11,
				"state":        "COMMENTED",
				"submitted_at": "2024-09-01T11:00:00Z",
			},
			{
				"id":           12,
				"state":        "APPROVED",
				"submitted_at": "2024-09-02T10:00:00Z",
			},
		}),
	)

	values := url.Values{}
	values.Set("per_page", "30")
	values.Set("page", "1")
	reg.Register(
		httpmock.QueryMatcher("GET", "repos/OWNER/REPO/pulls/99/reviews/12/comments", values),
		httpmock.JSONResponse([]map[string]interface{}{}),
	)

	pr := &api.PullRequest{Number: 99}
	repo := ghrepo.New("OWNER", "REPO")
	prshared.StubFinderForRunCommandStyleTests(t, "99", pr, repo)

	ios, _, stdout, _ := iostreams.Test()
	ios.SetStdoutTTY(false)

	factory := &cmdutil.Factory{
		IOStreams:  ios,
		HttpClient: func() (*http.Client, error) { return httpClient, nil },
	}

	cmd := NewCmdView(factory, nil)
	argv, err := shlex.Split("99 --latest")
	require.NoError(t, err)
	cmd.SetArgs(argv)

	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	require.NoError(t, cmd.Execute())

	require.Equal(t, "[]\n", stdout.String())
}
