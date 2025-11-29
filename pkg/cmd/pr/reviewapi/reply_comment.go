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

// PendingReviewError signals that the reply cannot proceed until pending reviews are submitted.
type PendingReviewError struct {
	Message string
	Err     error
}

func (e *PendingReviewError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "pending review must be submitted before replying"
}

func (e *PendingReviewError) Unwrap() error {
	return e.Err
}

// CommentNotFoundError indicates the target comment could not be located.
type CommentNotFoundError struct {
	Owner     string
	Repo      string
	Number    int
	CommentID int64
	Err       error
}

func (e *CommentNotFoundError) Error() string {
	return fmt.Sprintf("comment %d not found for %s/%s#%d", e.CommentID, e.Owner, e.Repo, e.Number)
}

func (e *CommentNotFoundError) Unwrap() error {
	return e.Err
}

// Reply represents the response schema for a comment reply.
type Reply struct {
	ID          int64     `json:"id"`
	InReplyToID *int64    `json:"in_reply_to_id"`
	Body        string    `json:"body"`
	User        UserLogin `json:"user"`
	CreatedAt   time.Time `json:"created_at"`
}

// ReplyToComment posts a threaded reply to an existing review comment.
func (s *Service) ReplyToComment(ctx context.Context, owner, repo string, prNumber int, commentID int64, body string) (*Reply, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/comments/%d/replies", owner, repo, prNumber, commentID)
	var reply Reply
	_, err := s.rest.PostJSON(ctx, path, nil, map[string]string{"body": body}, &reply)
	if err != nil {
		var httpErr api.HTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusUnprocessableEntity && isPendingReviewError(&httpErr) {
			return nil, &PendingReviewError{Message: httpErr.Message, Err: err}
		}
		if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotFound {
			return nil, &CommentNotFoundError{Owner: owner, Repo: repo, Number: prNumber, CommentID: commentID, Err: err}
		}
		return nil, err
	}

	return &reply, nil
}

// SubmitPendingReviews submits all pending reviews for the provided reviewer.
func (s *Service) SubmitPendingReviews(ctx context.Context, owner, repo string, prNumber int, reviewer string, summary string) (int, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews", owner, repo, prNumber)
	query := url.Values{}
	query.Set("per_page", "100")

	var pendingIDs []int64
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
			if !strings.EqualFold(review.User.Login, reviewer) {
				continue
			}
			if !strings.EqualFold(review.State, "PENDING") {
				continue
			}
			pendingIDs = append(pendingIDs, review.ID)
		}

		next, ok := NextLink(resp.Header.Get("Link"))
		if !ok {
			break
		}
		nextPath = next
		query = nil
	}

	count := 0
	for _, id := range pendingIDs {
		if err := s.submitReview(ctx, owner, repo, prNumber, id, summary); err != nil {
			return count, err
		}
		count++
	}

	return count, nil
}

func (s *Service) submitReview(ctx context.Context, owner, repo string, prNumber int, reviewID int64, summary string) error {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews/%d/events", owner, repo, prNumber, reviewID)
	body := map[string]string{"event": "COMMENT"}
	if summary != "" {
		body["body"] = summary
	}

	_, err := s.rest.PostJSON(ctx, path, nil, body, &struct{}{})
	if err != nil {
		var httpErr api.HTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotFound {
			return &ReviewNotFoundError{Owner: owner, Repo: repo, Number: prNumber, ReviewID: reviewID, Err: err}
		}
		return err
	}

	return nil
}

func isPendingReviewError(err *api.HTTPError) bool {
	needle := "pending review"
	if strings.Contains(strings.ToLower(err.Message), needle) {
		return true
	}
	for _, item := range err.Errors {
		if strings.Contains(strings.ToLower(item.Message), needle) {
			return true
		}
		if strings.Contains(strings.ToLower(item.Message), "user_id") && strings.Contains(strings.ToLower(item.Message), "pending review") {
			return true
		}
	}
	return false
}
