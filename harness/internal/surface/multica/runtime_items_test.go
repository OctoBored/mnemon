package multica

import (
	"testing"
	"time"
)

func TestRuntimeAgentMessageMessagesUsePhaseAndDelta(t *testing.T) {
	messages := RuntimeAgentMessageMessages("thread-1", "turn-1", "msg-1", "done", "final_answer", time.Date(2026, 6, 29, 7, 32, 0, 0, time.UTC))
	if len(messages) != 3 {
		t.Fatalf("messages = %+v, want start+delta+complete", messages)
	}
	started, _ := messages[0].Params["item"].(map[string]any)
	completed, _ := messages[2].Params["item"].(map[string]any)
	if started["phase"] != "final_answer" || completed["phase"] != "final_answer" {
		t.Fatalf("phase mismatch start=%+v completed=%+v", started, completed)
	}
	if messages[1].Method != "item/agentMessage/delta" || messages[1].Params["delta"] != "done" || messages[1].Params["itemId"] != "msg-1" {
		t.Fatalf("unexpected delta message: %+v", messages[1])
	}
}

func TestRuntimeTextInputExtractsOnlyTextItems(t *testing.T) {
	got := RuntimeTextInput(map[string]any{
		"input": []any{
			map[string]any{"type": "text", "text": "Open [TEA-1](mention://issue/iss-1)."},
			map[string]any{"type": "image", "url": "ignored"},
			map[string]any{"type": "text", "text": "  "},
			map[string]any{"type": "text", "text": "Then summarize."},
			"ignored",
		},
	})
	want := "Open [TEA-1](mention://issue/iss-1).\nThen summarize."
	if got != want {
		t.Fatalf("RuntimeTextInput() = %q, want %q", got, want)
	}
	if got := RuntimeTextInput(map[string]any{"input": "not-list"}); got != "" {
		t.Fatalf("non-list input should be ignored, got %q", got)
	}
}

func TestRuntimeInputMaterialExtractsStructuredIssueIdentity(t *testing.T) {
	got := RuntimeInputMaterial(map[string]any{
		"input": []any{
			map[string]any{
				"type": "text",
				"id":   "item-1",
				"text": "Please review the linked issue.",
				"text_elements": []any{
					map[string]any{
						"type":        "mention",
						"target_type": "issue",
						"target_id":   "iss-49",
						"text":        "@TEA-49",
					},
				},
			},
		},
	})
	if got.Text != "Please review the linked issue." {
		t.Fatalf("visible text changed: %+v", got)
	}
	if got.IssueIdentity != "iss-49" {
		t.Fatalf("structured issue identity = %q, want iss-49", got.IssueIdentity)
	}
	if got.IssueIdentitySource != RuntimeIssueSourceInput {
		t.Fatalf("structured issue source = %q, want %q", got.IssueIdentitySource, RuntimeIssueSourceInput)
	}
}

func TestRuntimeInputMaterialFallsBackToVisibleIssueTag(t *testing.T) {
	got := RuntimeInputMaterial(map[string]any{
		"input": []any{
			map[string]any{"type": "text", "id": "item-1", "text": "Please handle @TEA-50 next."},
		},
	})
	if got.IssueIdentity != "TEA-50" {
		t.Fatalf("visible issue tag identity = %q, want TEA-50", got.IssueIdentity)
	}
	if got.IssueIdentitySource != RuntimeIssueSourceInputText {
		t.Fatalf("visible issue source = %q, want %q", got.IssueIdentitySource, RuntimeIssueSourceInputText)
	}
}

func TestRuntimeInputMaterialPrefersStructuredIssueOverVisibleTag(t *testing.T) {
	got := RuntimeInputMaterial(map[string]any{
		"input": []any{
			map[string]any{"type": "text", "text": "Ignore the stale copied tag @TEA-1."},
			map[string]any{
				"type": "text",
				"text": "Use the selected Multica issue tag.",
				"text_elements": []any{
					map[string]any{
						"type":        "mention",
						"target_type": "issue",
						"target_id":   "iss-selected",
						"text":        "@TEA-99",
					},
				},
			},
		},
	})
	if got.IssueIdentity != "iss-selected" {
		t.Fatalf("structured issue identity should win over visible tag, got %q", got.IssueIdentity)
	}
}

func TestRuntimeInputMaterialIgnoresNonIssueItemID(t *testing.T) {
	got := RuntimeInputMaterial(map[string]any{
		"input": []any{
			map[string]any{"type": "text", "id": "item-1", "text": "Coordinate with @team."},
		},
	})
	if got.IssueIdentity != "" {
		t.Fatalf("non-issue item id should be ignored, got %q", got.IssueIdentity)
	}
}

func TestRuntimeRefNormalizesMulticaRefs(t *testing.T) {
	if got := RuntimeRef(" issue ", " iss-1 "); got != "multica:issue:iss-1" {
		t.Fatalf("RuntimeRef() = %q", got)
	}
	if got := RuntimeRef("", "iss-1"); got != "" {
		t.Fatalf("empty kind should produce no ref, got %q", got)
	}
	if got := RuntimeRef("issue", ""); got != "" {
		t.Fatalf("empty id should produce no ref, got %q", got)
	}
}
