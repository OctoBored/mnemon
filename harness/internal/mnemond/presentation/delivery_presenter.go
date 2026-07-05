package presentation

// delivery_presenter.go — Surface Delivery Reducer (r4-multica-adapter-contract §1):
// the machine-surface audience of the presentation layer. It reduces the
// accepted-decision log into surface deliveries a display adapter can
// dispatch deterministically. NO new state machine: the reducer is a pure
// function of decision rows; the cursor is the decision-log position and
// idempotency lives in the adapter's ledger.
//
// Milestone judgment is a CLOSED set: a progress digest whose outcome is
// result or blocker, a newly created assignment, or a newly opened teamwork
// signal. Everything else stays silent by design.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/mnemon-dev/mnemon/harness/internal/contract"
)

type Delivery struct {
	DeliveryID   string             `json:"delivery_id"`
	EventRef     string             `json:"event_ref"`
	ResourceRef  string             `json:"resource_ref"`
	Surface      string             `json:"surface"`
	Role         string             `json:"role"`   // display | activate
	Action       string             `json:"action"` // write_comment | write_status | create_carrier
	Title        string             `json:"title"`
	BodyMarkdown string             `json:"body_markdown"`
	Metadata     map[string]string  `json:"metadata"`
	Artifacts    []DeliveryArtifact `json:"artifacts,omitempty"`
}

type DeliveryArtifact struct {
	Digest string `json:"digest"`
	Name   string `json:"name,omitempty"`
}

type DeliveryFeed struct {
	Surface    string     `json:"surface"`
	Deliveries []Delivery `json:"deliveries"`
	NextCursor int64      `json:"next_cursor"`
}

// AcceptedDecisionSource is one accepted decision's reducer-visible slice:
// its log position (the feed cursor unit) and the resource snapshots it
// applied. The caller feeds only accepted rows after the request cursor.
type AcceptedDecisionSource struct {
	Cursor     int64
	DecisionID string
	Resources  []contract.ResourceSnapshot
}

const (
	DeliveryRoleDisplay  = "display"
	DeliveryRoleActivate = "activate"

	DeliveryActionWriteComment  = "write_comment"
	DeliveryActionCreateCarrier = "create_carrier"
)

// ReduceDeliveries projects accepted decisions after `cursor` into the
// delivery feed for one surface. Deterministic: same rows, same feed.
func ReduceDeliveries(sources []AcceptedDecisionSource, surface string, cursor int64) DeliveryFeed {
	feed := DeliveryFeed{Surface: surface, Deliveries: []Delivery{}, NextCursor: cursor}
	for _, src := range sources {
		if src.Cursor <= cursor {
			continue
		}
		if src.Cursor > feed.NextCursor {
			feed.NextCursor = src.Cursor
		}
		for _, snap := range src.Resources {
			if delivery, ok := reduceSnapshot(src, snap, surface); ok {
				feed.Deliveries = append(feed.Deliveries, delivery)
			}
		}
	}
	return feed
}

func reduceSnapshot(src AcceptedDecisionSource, snap contract.ResourceSnapshot, surface string) (Delivery, bool) {
	kind := string(snap.Ref.Kind)
	// Materialized coordination resources are append-list documents
	// ({content, items:[...]}); the decision's own contribution is the LAST
	// item — versions before it were already delivered under earlier cursors.
	item := deliveryItem(snap.Fields)
	var role, action, title, body string
	metadata := map[string]string{}
	switch kind {
	case "progress_digest":
		outcome := deliveryField(item, "outcome")
		if outcome != "result" && outcome != "blocker" {
			return Delivery{}, false
		}
		role, action = DeliveryRoleDisplay, DeliveryActionWriteComment
		title = deliveryTitle(deliveryField(item, "summary"))
		body = deliveryBody(item, "summary", "result", "blocker", "suggested_next")
	case "assignment":
		// every accepted assignment write appends a NEW assignment item
		role, action = DeliveryRoleActivate, DeliveryActionCreateCarrier
		title = deliveryTitle(deliveryField(item, "expected_work"))
		body = deliveryBody(item, "expected_work", "expected_feedback", "rationale")
		if assignee := deliveryField(item, "assignee"); assignee != "" {
			metadata["mnemon.target_agent_id"] = assignee
		}
	case "teamwork_signal":
		// every accepted signal write opens a NEW signal item
		role, action = DeliveryRoleDisplay, DeliveryActionWriteComment
		title = deliveryTitle(deliveryField(item, "statement"))
		body = deliveryBody(item, "statement", "why_teamwork")
	default:
		return Delivery{}, false
	}
	resourceRef := kind + "/" + string(snap.Ref.ID)
	metadata["mnemon.event_ref"] = src.DecisionID
	metadata["mnemon.resource_ref"] = resourceRef
	metadata["mnemon.surface_role"] = role
	if role == DeliveryRoleDisplay {
		metadata["mnemon.no_auto_dispatch"] = "true"
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%s", surface, src.Cursor, resourceRef)))
	return Delivery{
		DeliveryID:   "dlv_" + hex.EncodeToString(sum[:8]),
		EventRef:     src.DecisionID,
		ResourceRef:  resourceRef,
		Surface:      surface,
		Role:         role,
		Action:       action,
		Title:        title,
		BodyMarkdown: body,
		Metadata:     metadata,
		Artifacts:    deliveryArtifacts(item),
	}, true
}

// deliveryItem returns the decision's appended item from an append-list
// document, or the fields themselves when no items list exists (flat and
// zone lookup both stay valid on either shape).
func deliveryItem(fields map[string]any) map[string]any {
	items, ok := fields["items"].([]any)
	if !ok || len(items) == 0 {
		return fields
	}
	if item, ok := items[len(items)-1].(map[string]any); ok {
		return item
	}
	return fields
}

// deliveryField reads a materialized field: flat first, then inside the
// rule/narrative/refs zones (materializers differ on flattening).
func deliveryField(fields map[string]any, key string) string {
	if fields == nil {
		return ""
	}
	if value, ok := fields[key].(string); ok {
		return strings.TrimSpace(value)
	}
	for _, zone := range []string{"rule", "narrative", "refs"} {
		if section, ok := fields[zone].(map[string]any); ok {
			if value, ok := section[key].(string); ok {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

func deliveryFieldList(fields map[string]any, key string) []string {
	raw, ok := fields[key]
	if !ok {
		for _, zone := range []string{"refs", "rule", "narrative"} {
			if section, sok := fields[zone].(map[string]any); sok {
				if inner, iok := section[key]; iok {
					raw = inner
					ok = true
					break
				}
			}
		}
	}
	if !ok {
		return nil
	}
	var out []string
	switch list := raw.(type) {
	case []string:
		out = list
	case []any:
		for _, item := range list {
			if s, sok := item.(string); sok {
				out = append(out, s)
			}
		}
	}
	return out
}

// deliveryTitle is the first line of the leading narrative field, bounded
// rune-safe (CJK titles must never split inside a character).
func deliveryTitle(text string) string {
	line := text
	if idx := strings.IndexByte(line, '\n'); idx >= 0 {
		line = line[:idx]
	}
	runes := []rune(strings.TrimSpace(line))
	if len(runes) > 80 {
		return string(runes[:80])
	}
	return string(runes)
}

func deliveryBody(fields map[string]any, keys ...string) string {
	var parts []string
	for _, key := range keys {
		if value := deliveryField(fields, key); value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, "\n\n")
}

func deliveryArtifacts(fields map[string]any) []DeliveryArtifact {
	var artifacts []DeliveryArtifact
	for _, ref := range deliveryFieldList(fields, "artifact_refs") {
		ref = strings.TrimSpace(ref)
		if strings.HasPrefix(ref, "sha256:") {
			artifacts = append(artifacts, DeliveryArtifact{Digest: ref})
		}
	}
	return artifacts
}
