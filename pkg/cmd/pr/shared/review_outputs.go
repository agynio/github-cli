package shared

import (
	"strings"
	"time"

	"github.com/cli/cli/v2/api"
	"github.com/cli/cli/v2/pkg/cmdutil"
)

// ReviewCommentFields enumerates the allowed --json fields for review comment exports.
var ReviewCommentFields = []string{
	"id",
	"pull_request_review_id",
	"in_reply_to_id",
	"body",
	"author",
	"path",
	"position",
	"line",
	"side",
	"start_line",
	"start_side",
	"commit_id",
	"original_commit_id",
	"created_at",
	"updated_at",
	"url",
}

// ReviewFields enumerates the allowed --json fields for review metadata exports.
var ReviewFields = []string{
	"id",
	"state",
	"body",
	"author",
	"commit_id",
	"submitted_at",
	"url",
}

// ReviewCommentOutput is the normalized representation of a review comment for command output.
type ReviewCommentOutput struct {
	ID                  int64     `json:"id"`
	PullRequestReviewID int64     `json:"pull_request_review_id"`
	InReplyToID         *int64    `json:"in_reply_to_id,omitempty"`
	Body                string    `json:"body"`
	Author              string    `json:"author"`
	Path                string    `json:"path"`
	Position            *int      `json:"position,omitempty"`
	Line                *int      `json:"line,omitempty"`
	Side                *string   `json:"side,omitempty"`
	StartLine           *int      `json:"start_line,omitempty"`
	StartSide           *string   `json:"start_side,omitempty"`
	CommitID            string    `json:"commit_id,omitempty"`
	OriginalCommitID    string    `json:"original_commit_id,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
	URL                 string    `json:"url,omitempty"`
}

// NewReviewCommentOutput converts the REST API payload into ReviewCommentOutput.
func NewReviewCommentOutput(comment api.PullRequestReviewCommentREST) ReviewCommentOutput {
	out := ReviewCommentOutput{
		ID:                  comment.ID,
		PullRequestReviewID: comment.PullRequestReviewID,
		InReplyToID:         comment.InReplyToID,
		Body:                comment.Body,
		Path:                comment.Path,
		Position:            comment.Position,
		Line:                comment.Line,
		Side:                normalizeSide(comment.Side),
		StartLine:           comment.StartLine,
		StartSide:           normalizeSide(comment.StartSide),
		CommitID:            strings.TrimSpace(comment.CommitID),
		OriginalCommitID:    strings.TrimSpace(comment.OriginalCommitID),
		CreatedAt:           comment.CreatedAt,
		UpdatedAt:           comment.UpdatedAt,
		URL:                 comment.HTMLURL,
	}
	if comment.User != nil {
		out.Author = comment.User.Login
	}
	return out
}

func normalizeSide(side *string) *string {
	if side == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*side)
	if trimmed == "" {
		return nil
	}
	upper := strings.ToUpper(trimmed)
	return &upper
}

// ExportData satisfies the cmdutil.Exportable interface for JSON exports.
func (r ReviewCommentOutput) ExportData(fields []string) map[string]interface{} {
	return cmdutil.StructExportData(r, fields)
}

// ReviewOutput is the normalized representation of a pending review payload.
type ReviewOutput struct {
	ID          int64      `json:"id"`
	State       string     `json:"state"`
	Body        string     `json:"body"`
	Author      string     `json:"author"`
	CommitID    string     `json:"commit_id,omitempty"`
	SubmittedAt *time.Time `json:"submitted_at,omitempty"`
	URL         string     `json:"url,omitempty"`
}

// NewReviewOutput converts the REST API payload into ReviewOutput.
func NewReviewOutput(review api.PullRequestReviewREST) ReviewOutput {
	out := ReviewOutput{
		ID:          review.ID,
		State:       review.State,
		Body:        review.Body,
		CommitID:    strings.TrimSpace(review.CommitID),
		SubmittedAt: review.SubmittedAt,
		URL:         review.HTMLURL,
	}
	if review.User != nil {
		out.Author = review.User.Login
	}
	return out
}

// ExportData satisfies the cmdutil.Exportable interface for JSON exports.
func (r ReviewOutput) ExportData(fields []string) map[string]interface{} {
	return cmdutil.StructExportData(r, fields)
}
