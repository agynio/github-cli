package reviewapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cli/cli/v2/api"
)

// ReviewComment represents the minimal schema required by the commands.
type ReviewComment struct {
	ID                  int64     `json:"id"`
	PullRequestReviewID *int64    `json:"pull_request_review_id"`
	InReplyToID         *int64    `json:"in_reply_to_id"`
	Path                string    `json:"path"`
	Line                *int      `json:"line"`
	Side                *string   `json:"side"`
	Body                string    `json:"body"`
	User                UserLogin `json:"user"`
	CreatedAt           time.Time `json:"created_at"`
}

// UserLogin captures nested login fields in API responses.
type UserLogin struct {
	Login string `json:"login"`
}

// ReviewSummary contains metadata required to locate submitted reviews.
type ReviewSummary struct {
	ID          int64      `json:"id"`
	SubmittedAt *time.Time `json:"submitted_at"`
	User        UserLogin  `json:"user"`
	State       string     `json:"state"`
}

// ReviewNotFoundError indicates a review lookup failed.
type ReviewNotFoundError struct {
	Owner    string
	Repo     string
	Number   int
	ReviewID int64
	Err      error
}

func (e *ReviewNotFoundError) Error() string {
	return fmt.Sprintf("review %d not found for %s/%s#%d", e.ReviewID, e.Owner, e.Repo, e.Number)
}

func (e *ReviewNotFoundError) Unwrap() error {
	return e.Err
}

// PullRequestNotFoundError indicates the target pull request is missing or inaccessible.
type PullRequestNotFoundError struct {
	Owner  string
	Repo   string
	Number int
	Err    error
}

func (e *PullRequestNotFoundError) Error() string {
	return fmt.Sprintf("pull request %s/%s#%d not found", e.Owner, e.Repo, e.Number)
}

func (e *PullRequestNotFoundError) Unwrap() error {
	return e.Err
}

// NoSubmittedReviewError indicates no submitted review exists for the given reviewer.
type NoSubmittedReviewError struct {
	Reviewer string
	Number   int
}

func (e *NoSubmittedReviewError) Error() string {
	return fmt.Sprintf("no submitted reviews found for %s on pull request #%d", e.Reviewer, e.Number)
}

// LatestReviewID resolves the latest submitted review for the given reviewer.
func (s *Service) LatestReviewID(ctx context.Context, owner, repo string, prNumber int, reviewer string) (int64, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews", owner, repo, prNumber)
	query := url.Values{}
	query.Set("per_page", "100")

	var latestID int64
	var latestTime time.Time
	found := false

	nextPath := path
	for {
		var reviews []ReviewSummary
		resp, err := s.rest.GetJSON(ctx, nextPath, query, &reviews)
		if err != nil {
			var httpErr api.HTTPError
			if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotFound {
				return 0, &PullRequestNotFoundError{Owner: owner, Repo: repo, Number: prNumber, Err: err}
			}
			return 0, err
		}

		for _, review := range reviews {
			if review.SubmittedAt == nil {
				continue
			}
			if !strings.EqualFold(review.User.Login, reviewer) {
				continue
			}
			if !found || review.SubmittedAt.After(latestTime) {
				latestTime = *review.SubmittedAt
				latestID = review.ID
				found = true
			}
		}

		link := resp.Header.Get("Link")
		next, ok := NextLink(link)
		if !ok {
			break
		}
		nextPath = next
		query = nil
	}

	if !found {
		return 0, &NoSubmittedReviewError{Reviewer: reviewer, Number: prNumber}
	}

	return latestID, nil
}

// ReviewComments fetches all inline comments for the specified review.
func (s *Service) ReviewComments(ctx context.Context, owner, repo string, prNumber int, reviewID int64) ([]ReviewComment, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews/%d/comments", owner, repo, prNumber, reviewID)
	query := url.Values{}
	query.Set("per_page", "100")

	var allComments []ReviewComment
	nextPath := path
	for {
		var comments []ReviewComment
		resp, err := s.rest.GetJSON(ctx, nextPath, query, &comments)
		if err != nil {
			var httpErr api.HTTPError
			if errors.As(err, &httpErr) {
				if httpErr.StatusCode == http.StatusNotFound {
					return nil, &ReviewNotFoundError{Owner: owner, Repo: repo, Number: prNumber, ReviewID: reviewID, Err: err}
				}
				if httpErr.StatusCode == http.StatusForbidden {
					return nil, err
				}
			}
			return nil, err
		}

		allComments = append(allComments, comments...)

		link := resp.Header.Get("Link")
		next, ok := NextLink(link)
		if !ok {
			break
		}
		nextPath = next
		query = nil
	}

	return allComments, nil
}
