package reviewapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/cli/cli/v2/api"
)

type GraphQLReviewState struct {
	ID          string
	State       string
	SubmittedAt *time.Time
}

type ReviewThread struct {
	ID         string
	Path       string
	IsOutdated bool
}

type AddReviewThreadInput struct {
	ReviewID  string
	Path      string
	Line      int
	Side      string
	StartLine *int
	StartSide *string
	Body      string
}

type CreateReviewComment struct {
	Path     string `json:"path"`
	Position int    `json:"position"`
	Body     string `json:"body"`
}

type CreateReviewInput struct {
	CommitID string
	Event    string
	Body     string
	Comments []CreateReviewComment
}

type RESTReviewState struct {
	ID          int64
	State       string
	SubmittedAt *time.Time
}

func (s *Service) OpenReview(ctx context.Context, owner, repo string, prNumber int, commitOID string) (*GraphQLReviewState, error) {
	if commitOID == "" {
		sha, err := s.PullRequestHeadSHA(ctx, owner, repo, prNumber)
		if err != nil {
			return nil, err
		}
		commitOID = sha
	}

	prID, err := s.pullRequestNodeID(ctx, owner, repo, prNumber)
	if err != nil {
		return nil, err
	}

	query := `mutation AddPullRequestReview($pr:ID!,$oid:GitObjectID!){
		addPullRequestReview(input:{pullRequestId:$pr,commitOID:$oid}){
			pullRequestReview{ id state submittedAt }
		}
	}`

	variables := map[string]interface{}{
		"pr":  prID,
		"oid": commitOID,
	}

	var result struct {
		AddPullRequestReview struct {
			PullRequestReview struct {
				ID          string     `json:"id"`
				State       string     `json:"state"`
				SubmittedAt *time.Time `json:"submittedAt"`
			} `json:"pullRequestReview"`
		} `json:"addPullRequestReview"`
	}

	if err := s.gql.GraphQL(s.host, query, variables, &result); err != nil {
		return nil, err
	}

	review := result.AddPullRequestReview.PullRequestReview
	return &GraphQLReviewState{ID: review.ID, State: review.State, SubmittedAt: review.SubmittedAt}, nil
}

func (s *Service) AddReviewThread(ctx context.Context, _owner, _repo string, _prNumber int, input AddReviewThreadInput) (*ReviewThread, error) {
	query := `mutation AddPullRequestReviewThread($input: AddPullRequestReviewThreadInput!){
		addPullRequestReviewThread(input:$input){
			thread{ id path isOutdated }
		}
	}`

	graphqlInput := map[string]interface{}{
		"pullRequestReviewId": input.ReviewID,
		"path":                input.Path,
		"line":                input.Line,
		"side":                input.Side,
		"body":                input.Body,
	}
	if input.StartLine != nil {
		graphqlInput["startLine"] = *input.StartLine
	}
	if input.StartSide != nil {
		graphqlInput["startSide"] = *input.StartSide
	}

	variables := map[string]interface{}{"input": graphqlInput}

	var result struct {
		AddPullRequestReviewThread struct {
			Thread struct {
				ID         string `json:"id"`
				Path       string `json:"path"`
				IsOutdated bool   `json:"isOutdated"`
			} `json:"thread"`
		} `json:"addPullRequestReviewThread"`
	}

	if err := s.gql.GraphQL(s.host, query, variables, &result); err != nil {
		return nil, err
	}

	thread := result.AddPullRequestReviewThread.Thread
	return &ReviewThread{ID: thread.ID, Path: thread.Path, IsOutdated: thread.IsOutdated}, nil
}

func (s *Service) SubmitReview(ctx context.Context, _owner, _repo string, _prNumber int, reviewID, event, body string) (*GraphQLReviewState, error) {
	query := `mutation SubmitPullRequestReview($input: SubmitPullRequestReviewInput!){
		submitPullRequestReview(input:$input){
			pullRequestReview{ id state submittedAt }
		}
	}`

	graphqlInput := map[string]interface{}{
		"pullRequestReviewId": reviewID,
		"event":               event,
	}
	if body != "" {
		graphqlInput["body"] = body
	}

	variables := map[string]interface{}{"input": graphqlInput}

	var result struct {
		SubmitPullRequestReview struct {
			PullRequestReview struct {
				ID          string     `json:"id"`
				State       string     `json:"state"`
				SubmittedAt *time.Time `json:"submittedAt"`
			} `json:"pullRequestReview"`
		} `json:"submitPullRequestReview"`
	}

	if err := s.gql.GraphQL(s.host, query, variables, &result); err != nil {
		return nil, err
	}

	review := result.SubmitPullRequestReview.PullRequestReview
	return &GraphQLReviewState{ID: review.ID, State: review.State, SubmittedAt: review.SubmittedAt}, nil
}

func (s *Service) CreateReviewREST(ctx context.Context, owner, repo string, prNumber int, input CreateReviewInput) (*RESTReviewState, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews", owner, repo, prNumber)

	body := map[string]interface{}{}
	if input.CommitID != "" {
		body["commit_id"] = input.CommitID
	}
	if input.Event != "" {
		body["event"] = input.Event
	}
	if input.Body != "" {
		body["body"] = input.Body
	}
	if len(input.Comments) > 0 {
		body["comments"] = input.Comments
	}

	var response struct {
		ID          int64      `json:"id"`
		State       string     `json:"state"`
		SubmittedAt *time.Time `json:"submitted_at"`
	}

	_, err := s.rest.PostJSON(ctx, path, nil, body, &response)
	if err != nil {
		var httpErr api.HTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotFound {
			return nil, &PullRequestNotFoundError{Owner: owner, Repo: repo, Number: prNumber, Err: err}
		}
		return nil, err
	}

	return &RESTReviewState{ID: response.ID, State: response.State, SubmittedAt: response.SubmittedAt}, nil
}

func (s *Service) pullRequestNodeID(ctx context.Context, owner, repo string, prNumber int) (string, error) {
	query := `query PullRequestID($owner:String!,$name:String!,$number:Int!){
		repository(owner:$owner,name:$name){
			pullRequest(number:$number){ id }
		}
	}`

	variables := map[string]interface{}{
		"owner":  owner,
		"name":   repo,
		"number": prNumber,
	}

	var result struct {
		Repository struct {
			PullRequest struct {
				ID string `json:"id"`
			} `json:"pullRequest"`
		} `json:"repository"`
	}

	if err := s.gql.GraphQL(s.host, query, variables, &result); err != nil {
		return "", err
	}

	if result.Repository.PullRequest.ID == "" {
		return "", &PullRequestNotFoundError{Owner: owner, Repo: repo, Number: prNumber, Err: errors.New("pull request not found")}
	}

	return result.Repository.PullRequest.ID, nil
}

func (s *Service) PullRequestHeadSHA(ctx context.Context, owner, repo string, prNumber int) (string, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, prNumber)
	var pr struct {
		Head struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}

	_, err := s.rest.GetJSON(ctx, path, nil, &pr)
	if err != nil {
		var httpErr api.HTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotFound {
			return "", &PullRequestNotFoundError{Owner: owner, Repo: repo, Number: prNumber, Err: err}
		}
		return "", err
	}

	return pr.Head.SHA, nil
}
