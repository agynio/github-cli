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
	"pullRequestReviewId",
	"inReplyToId",
	"body",
	"author",
	"path",
	"position",
	"line",
	"side",
	"startLine",
	"startSide",
	"commitId",
	"originalCommitId",
	"createdAt",
	"updatedAt",
	"url",
}

// ReviewFields enumerates the allowed --json fields for review metadata exports.
var ReviewFields = []string{
	"id",
	"state",
	"body",
	"author",
	"commitId",
	"submittedAt",
	"url",
}

// ReviewCommentOutput is the normalized representation of a review comment for command output.
type ReviewCommentOutput struct {
	ID                  int64     `json:"id"`
	PullRequestReviewID int64     `json:"pullRequestReviewId"`
	InReplyToID         *int64    `json:"inReplyToId,omitempty"`
	Body                string    `json:"body"`
	Author              string    `json:"author"`
	Path                string    `json:"path"`
	Position            *int      `json:"position,omitempty"`
	Line                *int      `json:"line,omitempty"`
	Side                *string   `json:"side,omitempty"`
	StartLine           *int      `json:"startLine,omitempty"`
	StartSide           *string   `json:"startSide,omitempty"`
	CommitID            string    `json:"commitId,omitempty"`
	OriginalCommitID    string    `json:"originalCommitId,omitempty"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
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
	normalized := strings.TrimSpace(*side)
	if normalized == "" {
		return nil
	}
	upper := strings.ToUpper(normalized)
	return &upper
}

// ExportData satisfies the cmdutil.exportable interface for JSON exports.
func (r ReviewCommentOutput) ExportData(fields []string) map[string]interface{} {
	return cmdutil.StructExportData(r, fields)
}

// ReviewOutput is the normalized representation of a pending review payload.
type ReviewOutput struct {
	ID          int64      `json:"id"`
	State       string     `json:"state"`
	Body        string     `json:"body"`
	Author      string     `json:"author"`
	CommitID    string     `json:"commitId,omitempty"`
	SubmittedAt *time.Time `json:"submittedAt,omitempty"`
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

// ExportData satisfies the cmdutil.exportable interface for JSON exports.
func (r ReviewOutput) ExportData(fields []string) map[string]interface{} {
	return cmdutil.StructExportData(r, fields)
}
