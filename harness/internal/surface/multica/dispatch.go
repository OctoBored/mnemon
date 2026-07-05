package multica

// dispatch.go — the R4 delivery dispatcher (r4-multica-adapter-contract):
// consumes Surface Delivery Feed items and executes a DETERMINISTIC
// writeback plan. It may: read deliveries · write comments/status/metadata
// through the idempotency ledger · materialize artifacts locally · advance
// its cursor. It may NOT: parse business semantics, guess intent, schedule
// providers, mint canonical state, or bypass mnemond.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mnemon-dev/mnemon/harness/internal/mnemond/presentation"
)

// visibleTextForbidden is the §2 forbidden set: zero hits allowed in any
// operator-visible text. Hits are replaced and the original moves to
// metadata + the local audit trail.
var visibleTextForbidden = []*regexp.Regexp{
	regexp.MustCompile(`mnemon:[a-z_]+=`),
	regexp.MustCompile(`event_ref`),
	regexp.MustCompile(`resource_ref`),
	regexp.MustCompile(`principal`),
	regexp.MustCompile(`ingest_seq`),
	regexp.MustCompile(`seq=`),
	regexp.MustCompile(`dec_[0-9A-Za-z_-]*`),
	regexp.MustCompile(`local/[0-9A-Za-z._-]+/`),
	regexp.MustCompile(`sha256:[0-9a-f]*`),
}

const redactionPlaceholder = "[技术引用已移入元数据]"

// RedactVisibleText enforces the forbidden set on operator-visible text.
func RedactVisibleText(text string) (string, bool) {
	redacted := false
	for _, pattern := range visibleTextForbidden {
		if pattern.MatchString(text) {
			text = pattern.ReplaceAllString(text, redactionPlaceholder)
			redacted = true
		}
	}
	return text, redacted
}

// DispatchClient is the narrow Multica face the dispatcher needs.
type DispatchClient interface {
	AddIssueComment(ctx context.Context, issueID, content string) (MulticaComment, error)
	SetIssueMetadataMap(ctx context.Context, issueID string, values map[string]string) error
	SetIssueStatus(ctx context.Context, issueID, status string) (MulticaIssue, error)
	GetIssue(ctx context.Context, issueID string) (MulticaIssue, error)
}

type MulticaComment struct {
	ID string `json:"id"`
}

type MulticaIssue struct {
	ID              string `json:"id"`
	Status          string `json:"status"`
	AssignedAgentID string `json:"assigned_agent_id"`
}

// DispatchDeps wires the dispatcher's world.
type DispatchDeps struct {
	Client      DispatchClient
	Ledger      *FileSurfaceWriteLedger
	FetchBlob   func(digest string) ([]byte, error) // node blob lane
	ArtifactDir string                              // .mnemon/multica/artifacts
	Surface     string
}

// DispatchResult reports what one delivery did — honest narration material.
type DispatchResult struct {
	DeliveryID          string   `json:"delivery_id"`
	IssueID             string   `json:"issue_id"`
	CommentID           string   `json:"comment_id,omitempty"`
	SkippedDuplicate    bool     `json:"skipped_duplicate,omitempty"`
	SkippedStatusReason string   `json:"skipped_status_reason,omitempty"`
	Redacted            bool     `json:"redacted,omitempty"`
	Artifacts           []string `json:"artifacts,omitempty"` // local paths (metadata channel)
}

// DispatchDelivery executes one delivery against one issue. Idempotency:
// the (event_ref, role, comment, issue) ledger key reserves before any
// write; a replayed delivery is skipped_duplicate with zero new comments.
func DispatchDelivery(ctx context.Context, deps DispatchDeps, delivery presentation.Delivery, issueID string) (DispatchResult, error) {
	result := DispatchResult{DeliveryID: delivery.DeliveryID, IssueID: issueID}
	if deps.Client == nil || deps.Ledger == nil {
		return result, fmt.Errorf("dispatch requires a client and the write ledger")
	}
	ledgerRecord := SurfaceWriteLedgerRecord{
		EventRef:    delivery.EventRef,
		ResourceRef: delivery.ResourceRef,
		SurfaceRole: SurfaceRole(delivery.Role),
		TargetKind:  "comment",
		TargetID:    issueID,
	}
	_, reserved, err := deps.Ledger.Reserve(ledgerRecord)
	if err != nil {
		return result, err
	}
	if !reserved {
		result.SkippedDuplicate = true
		return result, nil
	}

	// artifacts materialize FIRST (§6): visible text names files only; the
	// full local path and digest ride metadata.
	metadata := map[string]string{}
	for key, value := range delivery.Metadata {
		metadata[key] = value
	}
	metadata[MulticaMetadataSurfaceRef] = "multica://issue/" + issueID
	var artifactNames []string
	for _, artifact := range delivery.Artifacts {
		localPath, name, matErr := MaterializeArtifact(deps.FetchBlob, deps.ArtifactDir, artifact.Digest, artifact.Name)
		if matErr != nil {
			return result, matErr
		}
		artifactNames = append(artifactNames, name)
		result.Artifacts = append(result.Artifacts, localPath)
		metadata[MulticaMetadataSourceArtifactRef] = artifact.Digest
	}

	// visible text: title + narrative through redaction; NEVER an error
	// object, digest, or sender path.
	body := "Mnemon 更新: " + strings.TrimSpace(delivery.Title)
	if narrative := strings.TrimSpace(delivery.BodyMarkdown); narrative != "" {
		body += "\n\n" + narrative
	}
	if len(artifactNames) > 0 {
		body += "\n\n附件: " + strings.Join(artifactNames, ", ")
	}
	clean, redacted := RedactVisibleText(body)
	if redacted {
		metadata["mnemon.redacted"] = "true"
		result.Redacted = true
	}

	comment, err := deps.Client.AddIssueComment(ctx, issueID, clean)
	if err != nil {
		return result, fmt.Errorf("写回未完成: %w", err)
	}
	result.CommentID = comment.ID
	if err := deps.Client.SetIssueMetadataMap(ctx, issueID, metadata); err != nil {
		return result, fmt.Errorf("元数据未写入: %w", err)
	}
	ledgerRecord.Status = "written"
	ledgerRecord.SurfaceRef = metadata[MulticaMetadataSurfaceRef]
	if ref, ok := metadata[MulticaMetadataSourceArtifactRef]; ok {
		ledgerRecord.SourceArtifactRef = ref
	}
	if err := deps.Ledger.Record(ledgerRecord); err != nil {
		return result, err
	}
	return result, nil
}

// DispatchStatus applies a status change under the §5 rule: the assignment
// state comes from GetIssue (never a caller flag); a failed lookup
// fail-closes with a recorded reason.
func DispatchStatus(ctx context.Context, deps DispatchDeps, issueID, desired string) (string, string, error) {
	issue, err := deps.Client.GetIssue(ctx, issueID)
	if err != nil {
		return "", fmt.Sprintf("状态未写入: 指派状态查询失败(%v)", err), nil
	}
	if !DisplayStatusWriteAllowed(DisplayStatusWriteInput{
		Status:             desired,
		AssigneeAgentID:    issue.AssignedAgentID,
		AssignedToProvider: strings.TrimSpace(issue.AssignedAgentID) != "",
	}) {
		return "", fmt.Sprintf("状态未写入: 已指派 issue 仅允许 backlog/done/cancelled(当前指派 %s)", issue.AssignedAgentID), nil
	}
	if _, err := deps.Client.SetIssueStatus(ctx, issueID, desired); err != nil {
		return "", "", fmt.Errorf("状态写入失败: %w", err)
	}
	return desired, "", nil
}

// MaterializeArtifact lands one blob at <dir>/<digest>/<name> and returns
// (localPath, visibleName). The visible name never contains the digest.
func MaterializeArtifact(fetch func(string) ([]byte, error), dir, digest, name string) (string, string, error) {
	if fetch == nil {
		return "", "", fmt.Errorf("artifact %s: no blob lane available", digest)
	}
	if strings.TrimSpace(name) == "" {
		name = "附件.bin"
	}
	name = filepath.Base(name)
	target := filepath.Join(dir, strings.TrimPrefix(digest, "sha256:"), name)
	if _, err := os.Stat(target); err == nil {
		return target, name, nil
	}
	data, err := fetch(digest)
	if err != nil {
		return "", "", fmt.Errorf("artifact %s: %w", digest, err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(target, data, 0o600); err != nil {
		return "", "", err
	}
	return target, name, nil
}
