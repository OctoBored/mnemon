package presentation

import (
	"strings"
	"testing"

	"github.com/mnemon-dev/mnemon/harness/internal/contract"
)

func deliverySources() []AcceptedDecisionSource {
	return []AcceptedDecisionSource{
		{
			Cursor: 5, DecisionID: "dec_5",
			Resources: []contract.ResourceSnapshot{{
				Ref: contract.ResourceRef{Kind: "progress_digest", ID: "payments"}, Version: 2,
				Fields: map[string]any{
					"outcome": "progress",
					"summary": "阶段推进:契约测试过半。",
				},
			}},
		},
		{
			Cursor: 7, DecisionID: "dec_7",
			Resources: []contract.ResourceSnapshot{{
				Ref: contract.ResourceRef{Kind: "progress_digest", ID: "payments"}, Version: 3,
				Fields: map[string]any{
					"outcome":       "result",
					"summary":       "排查完成:根因是对账窗口保持配置被误设。\n详情见附件。",
					"result":        "修复值 30000 已生效。",
					"artifact_refs": []any{"sha256:" + strings.Repeat("ab", 32)},
				},
			}},
		},
		{
			// real materialized shape: append-list document, zoned item
			Cursor: 9, DecisionID: "dec_9",
			Resources: []contract.ResourceSnapshot{{
				Ref: contract.ResourceRef{Kind: "assignment", ID: "project"}, Version: 1,
				Fields: map[string]any{
					"content": "# Assignments\n- payments",
					"items": []any{map[string]any{
						"actor": "agent-a@e1",
						"rule":  map[string]any{"assignee": "agent-b@e1", "scope": "payments/reconcile"},
						"narrative": map[string]any{
							"expected_work":     "复核回调重试参数。",
							"expected_feedback": "result",
						},
					}},
				},
			}},
		},
		{
			// second append on the same resource whose LAST item is a plain
			// progress item — must stay silent (item-level judgment, not version)
			Cursor: 11, DecisionID: "dec_11",
			Resources: []contract.ResourceSnapshot{{
				Ref: contract.ResourceRef{Kind: "progress_digest", ID: "payments"}, Version: 4,
				Fields: map[string]any{
					"content": "# Progress",
					"items": []any{
						map[string]any{"rule": map[string]any{"outcome": "result"}, "narrative": map[string]any{"summary": "旧结果,已在 cursor 7 投递。"}},
						map[string]any{"rule": map[string]any{"outcome": "progress"}, "narrative": map[string]any{"summary": "后续小步推进。"}},
					},
				},
			}},
		},
	}
}

func TestReduceDeliveriesMilestoneClosedSet(t *testing.T) {
	feed := ReduceDeliveries(deliverySources(), "multica", 0)
	if feed.NextCursor != 11 {
		t.Fatalf("next_cursor must reach the last row, got %d", feed.NextCursor)
	}
	if len(feed.Deliveries) != 2 {
		t.Fatalf("only result-digest and new assignment may deliver, got %d: %+v", len(feed.Deliveries), feed.Deliveries)
	}

	result := feed.Deliveries[0]
	if result.Role != DeliveryRoleDisplay || result.Action != DeliveryActionWriteComment {
		t.Fatalf("result digest must be display/write_comment, got %s/%s", result.Role, result.Action)
	}
	if result.Title != "排查完成:根因是对账窗口保持配置被误设。" {
		t.Fatalf("title must be the first line, got %q", result.Title)
	}
	if !strings.Contains(result.BodyMarkdown, "修复值 30000 已生效。") {
		t.Fatalf("body must fold narrative fields, got %q", result.BodyMarkdown)
	}
	if len(result.Artifacts) != 1 || !strings.HasPrefix(result.Artifacts[0].Digest, "sha256:") {
		t.Fatalf("artifact refs must ride the artifacts list, got %+v", result.Artifacts)
	}
	if result.Metadata["mnemon.no_auto_dispatch"] != "true" {
		t.Fatal("display deliveries must pin no_auto_dispatch")
	}

	activate := feed.Deliveries[1]
	if activate.Role != DeliveryRoleActivate || activate.Action != DeliveryActionCreateCarrier {
		t.Fatalf("new assignment must be activate/create_carrier, got %s/%s", activate.Role, activate.Action)
	}
	if activate.Metadata["mnemon.target_agent_id"] != "agent-b@e1" {
		t.Fatalf("activate delivery must name the target agent, got %v", activate.Metadata)
	}
}

func TestReduceDeliveriesCursorAndDeterminism(t *testing.T) {
	sources := deliverySources()
	first := ReduceDeliveries(sources, "multica", 0)
	again := ReduceDeliveries(sources, "multica", 0)
	if first.Deliveries[0].DeliveryID != again.Deliveries[0].DeliveryID {
		t.Fatal("delivery ids must be deterministic")
	}
	// resume after the result digest: only the assignment remains
	resumed := ReduceDeliveries(sources, "multica", 7)
	if len(resumed.Deliveries) != 1 || resumed.Deliveries[0].Role != DeliveryRoleActivate {
		t.Fatalf("cursor must skip consumed rows, got %+v", resumed.Deliveries)
	}
	// empty tail keeps the cursor
	empty := ReduceDeliveries(sources, "multica", 11)
	if len(empty.Deliveries) != 0 || empty.NextCursor != 11 {
		t.Fatalf("empty feed must hold the cursor, got %+v", empty)
	}
}

func TestReduceDeliveriesTitleRuneSafe(t *testing.T) {
	long := strings.Repeat("对账窗口保持时间", 20)
	feed := ReduceDeliveries([]AcceptedDecisionSource{{
		Cursor: 1, DecisionID: "dec_1",
		Resources: []contract.ResourceSnapshot{{
			Ref: contract.ResourceRef{Kind: "teamwork_signal", ID: "s1"}, Version: 1,
			Fields: map[string]any{"statement": long, "why_teamwork": "需要更多人手。"},
		}},
	}}, "multica", 0)
	if len(feed.Deliveries) != 1 {
		t.Fatalf("new signal must deliver, got %d", len(feed.Deliveries))
	}
	title := feed.Deliveries[0].Title
	if len([]rune(title)) != 80 {
		t.Fatalf("title must clamp at 80 runes, got %d", len([]rune(title)))
	}
	for _, r := range title {
		if r == '�' {
			t.Fatal("title clamp must never split a rune")
		}
	}
}
