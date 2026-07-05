package multica

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildDisplayWritebackPlanIsDisplaySafe(t *testing.T) {
	plan, err := BuildDisplayWritebackPlan(DisplayWritebackRequest{
		IssueID: "issue-1",
		Refs: SurfaceRefs{
			EventRef:          "event-progress-1",
			ResourceRef:       "progress_digest/prog-1",
			SurfaceRef:        "surface-1",
			SourceArtifactRef: "multica.comment:comment-1",
		},
		Title:              "进展",
		StatusLabel:        "result",
		Summary:            "已确认退款风控误杀来自昨日规则变更。",
		EvidenceRefs:       []string{"ctx:风险登记", "artifact:规则 diff"},
		DesiredStatus:      StatusInProgress,
		AssignedToProvider: true,
		AssigneeAgentID:    "agent-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != "" {
		t.Fatalf("display lane must not move provider-assigned issue to active status: %+v", plan)
	}
	if plan.SkippedStatusReason == "" {
		t.Fatalf("unsafe status skip reason missing: %+v", plan)
	}
	if plan.Metadata[MulticaMetadataSurfaceRole] != string(SurfaceRoleDisplay) ||
		plan.Metadata[MulticaMetadataNoAutoDispatch] != "true" ||
		plan.Metadata[MulticaMetadataEventRef] != "event-progress-1" {
		t.Fatalf("display metadata mismatch: %+v", plan.Metadata)
	}
	if !strings.Contains(plan.CommentBody, "Mnemon 更新: 进展") ||
		!strings.Contains(plan.CommentBody, "已确认退款风控误杀") {
		t.Fatalf("comment body is not a Chinese structured OA update:\n%s", plan.CommentBody)
	}
	// §2/§6: evidence/artifact/event refs are machine-channel only — never
	// in the operator-visible comment.
	for _, ref := range []string{"ctx:风险登记", "artifact:规则 diff", "event-progress-1"} {
		if strings.Contains(plan.CommentBody, ref) {
			t.Fatalf("visible comment leaked a machine-channel ref %q:\n%s", ref, plan.CommentBody)
		}
	}
}

func TestBuildDisplayWritebackPlanAllowsSafeDoneStatus(t *testing.T) {
	plan, err := BuildDisplayWritebackPlan(DisplayWritebackRequest{
		IssueID:       "issue-1",
		Refs:          SurfaceRefs{EventRef: "event-result-1", ResourceRef: "progress_digest/prog-2"},
		Title:         "结果",
		Summary:       "支付回调失败已完成复核。",
		DesiredStatus: StatusDone,
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != StatusDone {
		t.Fatalf("safe done status = %q, want %q", plan.Status, StatusDone)
	}
}

func TestBuildActivationCarrierRequiresEventRef(t *testing.T) {
	if _, err := BuildActivationCarrier(ActivationCarrierRequest{
		IssueID:     "issue-1",
		ResourceRef: "assignment/asg-1",
	}); err == nil {
		t.Fatal("activation carrier without event_ref must fail")
	}

	carrier, err := BuildActivationCarrier(ActivationCarrierRequest{
		IssueID:      "issue-1",
		EventRef:     "event-assignment-1",
		ResourceRef:  "assignment/asg-1",
		CarrierTitle: "继续处理支付回调排查",
	})
	if err != nil {
		t.Fatal(err)
	}
	if carrier.Metadata[MulticaMetadataSurfaceRole] != string(SurfaceRoleActivate) ||
		carrier.Metadata[MulticaMetadataEventRef] != "event-assignment-1" {
		t.Fatalf("activation metadata mismatch: %+v", carrier.Metadata)
	}
}

func TestSurfaceWriteLedgerDedupesByEventRoleAndTarget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "surface-write-ledger.jsonl")
	ledger := NewFileSurfaceWriteLedger(path)
	record := SurfaceWriteLedgerRecord{
		EventRef:    "event-1",
		ResourceRef: "progress_digest/prog-1",
		SurfaceRef:  "surface-1",
		SurfaceRole: SurfaceRoleDisplay,
		TargetKind:  "comment",
		TargetID:    "comment-1",
		Status:      "reserved",
	}
	if _, ok, err := ledger.Reserve(record); err != nil || !ok {
		t.Fatalf("reserve failed ok=%v err=%v", ok, err)
	}
	if _, ok, err := NewFileSurfaceWriteLedger(path).Reserve(record); err != nil || ok {
		t.Fatalf("duplicate reserve ok=%v err=%v", ok, err)
	}
	record.Status = "written"
	if err := NewFileSurfaceWriteLedger(path).Record(record); err != nil {
		t.Fatal(err)
	}
	found, ok, err := NewFileSurfaceWriteLedger(path).Find("event-1", SurfaceRoleDisplay, SurfaceWriteLedgerTarget{Kind: "comment", ID: "comment-1"})
	if err != nil || !ok {
		t.Fatalf("find failed ok=%v err=%v", ok, err)
	}
	if found.Status != "written" || found.SurfaceRef != "surface-1" {
		t.Fatalf("ledger record mismatch: %+v", found)
	}
}
