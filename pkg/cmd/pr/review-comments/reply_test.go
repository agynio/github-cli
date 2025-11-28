package reviewcomments

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

func TestReplyCreatesComment(t *testing.T) {
	reg := &httpmock.Registry{}
	httpClient := &http.Client{}
	httpmock.ReplaceTripper(httpClient, reg)

	reg.Register(
		httpmock.REST("POST", "repos/OWNER/REPO/pulls/123/comments/456/replies"),
		func(req *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.Equal(t, "{\"body\":\"Thanks!\"}", string(bytes.TrimSpace(body)))

			payload := map[string]interface{}{
				"id":                     999,
				"pull_request_review_id": 77,
				"in_reply_to_id":         456,
				"body":                   "Thanks!",
				"path":                   "src/app.go",
				"line":                   18,
				"side":                   "RIGHT",
				"start_line":             11,
				"start_side":             "RIGHT",
				"html_url":               "https://github.com/OWNER/REPO/pull/123#discussion_r999",
				"created_at":             "2024-10-01T12:00:00Z",
				"updated_at":             "2024-10-01T12:00:00Z",
				"user": map[string]interface{}{
					"login": "octocat",
				},
			}
			return httpmock.JSONResponse(payload)(req)
		},
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

	cmd := NewCmdReply(factory, nil)
	argv, err := shlex.Split("123 --comment-id 456 --body 'Thanks!'")
	require.NoError(t, err)
	cmd.SetArgs(argv)

	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	require.NoError(t, cmd.Execute())

	expected := "{\"id\":999,\"pullRequestReviewId\":77,\"inReplyToId\":456,\"body\":\"Thanks!\",\"author\":\"octocat\",\"path\":\"src/app.go\",\"line\":18,\"side\":\"RIGHT\",\"startLine\":11,\"startSide\":\"RIGHT\",\"createdAt\":\"2024-10-01T12:00:00Z\",\"updatedAt\":\"2024-10-01T12:00:00Z\",\"url\":\"https://github.com/OWNER/REPO/pull/123#discussion_r999\"}\n"
	require.Equal(t, expected, stdout.String())
}
