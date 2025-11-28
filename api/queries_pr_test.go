package api

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/cli/cli/v2/internal/ghrepo"
	"github.com/cli/cli/v2/pkg/httpmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBranchDeleteRemote(t *testing.T) {
	var tests = []struct {
		name        string
		branch      string
		httpStubs   func(*httpmock.Registry)
		expectError bool
	}{
		{
			name:   "success",
			branch: "owner/branch#123",
			httpStubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("DELETE", "repos/OWNER/REPO/git/refs/heads/owner%2Fbranch%23123"),
					httpmock.StatusStringResponse(204, ""))
			},
			expectError: false,
		},
		{
			name:   "error",
			branch: "my-branch",
			httpStubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("DELETE", "repos/OWNER/REPO/git/refs/heads/my-branch"),
					httpmock.StatusStringResponse(500, `{"message": "oh no"}`))
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			http := &httpmock.Registry{}
			if tt.httpStubs != nil {
				tt.httpStubs(http)
			}

			client := newTestClient(http)
			repo, _ := ghrepo.FromFullName("OWNER/REPO")

			err := BranchDeleteRemote(client, repo, tt.branch)
			if (err != nil) != tt.expectError {
				t.Fatalf("unexpected result: %v", err)
			}
		})
	}
}

func Test_Logins(t *testing.T) {
	rr := ReviewRequests{}
	var tests = []struct {
		name             string
		requestedReviews string
		want             []string
	}{
		{
			name:             "no requested reviewers",
			requestedReviews: `{"nodes": []}`,
			want:             []string{},
		},
		{
			name: "user",
			requestedReviews: `{"nodes": [
				{
					"requestedreviewer": {
						"__typename": "User", "login": "testuser"
					}
				}
			]}`,
			want: []string{"testuser"},
		},
		{
			name: "team",
			requestedReviews: `{"nodes": [
				{
					"requestedreviewer": {
						"__typename": "Team",
						"name": "Test Team",
						"slug": "test-team",
						"organization": {"login": "myorg"}
					}
				}
			]}`,
			want: []string{"myorg/test-team"},
		},
		{
			name: "multiple users and teams",
			requestedReviews: `{"nodes": [
				{
					"requestedreviewer": {
						"__typename": "User", "login": "user1"
					}
				},
				{
					"requestedreviewer": {
						"__typename": "User", "login": "user2"
					}
				},
				{
					"requestedreviewer": {
						"__typename": "Team",
						"name": "Test Team",
						"slug": "test-team",
						"organization": {"login": "myorg"}
					}
				},
				{
					"requestedreviewer": {
						"__typename": "Team",
						"name": "Dev Team",
						"slug": "dev-team",
						"organization": {"login": "myorg"}
					}
				}
			]}`,
			want: []string{"user1", "user2", "myorg/test-team", "myorg/dev-team"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := json.Unmarshal([]byte(tt.requestedReviews), &rr)
			assert.NoError(t, err, "Failed to unmarshal json string as ReviewRequests")
			logins := rr.Logins()
			assert.Equal(t, tt.want, logins)
		})
	}
}

func TestListPullRequestFilePaths(t *testing.T) {
	reg := &httpmock.Registry{}
	defer reg.Verify(t)

	reg.Register(
		httpmock.REST("POST", "graphql"),
		func(req *http.Request) (*http.Response, error) {
			var body struct {
				Query     string                 `json:"query"`
				Variables map[string]interface{} `json:"variables"`
			}
			payload, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}
			if err := json.Unmarshal(payload, &body); err != nil {
				return nil, err
			}

			assert.Nil(t, body.Variables["after"])
			return httpmock.StatusJSONResponse(200, map[string]interface{}{
				"data": map[string]interface{}{
					"repository": map[string]interface{}{
						"pullRequest": map[string]interface{}{
							"files": map[string]interface{}{
								"nodes": []map[string]interface{}{{"path": "README.md"}},
								"pageInfo": map[string]interface{}{
									"hasNextPage": true,
									"endCursor":   "cursor1",
								},
							},
						},
					},
				},
			})(req)
		},
	)

	reg.Register(
		httpmock.REST("POST", "graphql"),
		func(req *http.Request) (*http.Response, error) {
			var body struct {
				Query     string                 `json:"query"`
				Variables map[string]interface{} `json:"variables"`
			}
			payload, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}
			if err := json.Unmarshal(payload, &body); err != nil {
				return nil, err
			}

			require.Equal(t, "cursor1", body.Variables["after"])
			return httpmock.StatusJSONResponse(200, map[string]interface{}{
				"data": map[string]interface{}{
					"repository": map[string]interface{}{
						"pullRequest": map[string]interface{}{
							"files": map[string]interface{}{
								"nodes": []map[string]interface{}{{"path": "app.go"}},
								"pageInfo": map[string]interface{}{
									"hasNextPage": false,
									"endCursor":   nil,
								},
							},
						},
					},
				},
			})(req)
		},
	)

	client := newTestClient(reg)
	repo := ghrepo.New("OWNER", "REPO")

	paths, err := ListPullRequestFilePaths(client, repo, 10)
	require.NoError(t, err)
	assert.Equal(t, map[string]struct{}{
		"README.md": {},
		"app.go":    {},
	}, paths)
}
