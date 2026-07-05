package main

import (
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func rootSub(t *testing.T, name string) *cobra.Command {
	t.Helper()
	for _, c := range rootCmd.Commands() {
		if c.Name() == name {
			return c
		}
	}
	t.Fatalf("root command %q not registered", name)
	return nil
}

func TestEmitUnknownSchemaListsRegisteredKinds(t *testing.T) {
	emitSchema = "audit/finding"
	emit := rootSub(t, "emit")
	err := emit.RunE(emit, nil)
	if err == nil || !strings.Contains(err.Error(), "teamwork/signal") {
		t.Fatalf("unknown schema must fail closed listing registered kinds, got %v", err)
	}
}

func TestEmitAssemblesSameWireAsDialect(t *testing.T) {
	server, envelopes, _ := stubNode(t)
	controlAddr = server.URL
	controlPrincipal = "agent-a"
	controlToken = ""
	controlTokenFile = ""

	// generic emit
	emitSchema = "teamwork/report"
	emitRulePairs = []string{"feedback_kind=result", "scope=payments/reconcile"}
	emitRefPairs = []string{"evidence_refs=seq:41", "evidence_refs=seq:42"}
	emitNarrPairs = []string{"summary=排查完成,根因已定位。", "result=对账窗口恢复 30000。"}
	emitExternalID = ""
	emit := rootSub(t, "emit")
	emit.SetOut(io.Discard)
	if err := emit.RunE(emit, nil); err != nil {
		t.Fatalf("emit failed: %v", err)
	}

	// dialect equivalent
	resetProgressVars()
	controlTeamworkProgressFeedbackKind = "result"
	controlTeamworkProgressScope = "payments/reconcile"
	controlTeamworkProgressSummary = "排查完成,根因已定位。"
	controlTeamworkProgressResult = "对账窗口恢复 30000。"
	controlTeamworkProgressEvidence = []string{"seq:41", "seq:42"}
	report := teamworkSub(t, "report")
	report.SetOut(io.Discard)
	if err := report.RunE(report, nil); err != nil {
		t.Fatalf("teamwork report failed: %v", err)
	}

	if len(*envelopes) != 2 {
		t.Fatalf("expected two ingests, got %d", len(*envelopes))
	}
	a, b := (*envelopes)[0], (*envelopes)[1]
	if a.Event.Type != b.Event.Type {
		t.Fatalf("emit and dialect diverge on type: %s vs %s", a.Event.Type, b.Event.Type)
	}
	if !reflect.DeepEqual(a.Event.Payload, b.Event.Payload) {
		t.Fatalf("emit and dialect diverge on payload:\nemit:    %#v\ndialect: %#v", a.Event.Payload, b.Event.Payload)
	}
}

func TestEmitZonePairShapes(t *testing.T) {
	zone, err := emitZone([]string{"scope=a", "report_on=x", "report_on=y"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if zone["scope"] != "a" {
		t.Fatalf("single key must stay scalar, got %#v", zone["scope"])
	}
	if got, ok := zone["report_on"].([]string); !ok || len(got) != 2 {
		t.Fatalf("repeated key must become a list, got %#v", zone["report_on"])
	}
	refs, err := emitZone([]string{"evidence_refs=seq:1"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := refs["evidence_refs"].([]string); !ok || len(got) != 1 {
		t.Fatalf("refs zone must always be list-typed, got %#v", refs["evidence_refs"])
	}
	if _, err := emitZone([]string{"no-equals"}, false); err == nil {
		t.Fatal("malformed pair must fail closed")
	}
}

func TestEmitBareKindFallsThroughToRegistry(t *testing.T) {
	server, envelopes, _ := stubNode(t)
	controlAddr = server.URL
	controlPrincipal = "agent-a"
	controlToken = ""
	controlTokenFile = ""

	emitSchema = "widget"
	emitRulePairs = nil
	emitRefPairs = nil
	emitNarrPairs = []string{"statement=外部包事件。"}
	emitExternalID = ""
	emit := rootSub(t, "emit")
	emit.SetOut(io.Discard)
	if err := emit.RunE(emit, nil); err != nil {
		t.Fatalf("bare-kind emit must submit (node registry validates): %v", err)
	}
	got := (*envelopes)[len(*envelopes)-1]
	if got.Event.Type != "widget.write_candidate.observed" {
		t.Fatalf("bare kind must map to its observed type, got %s", got.Event.Type)
	}
	// a slashed unknown schema still fails closed at the CLI gate
	emitSchema = "audit/finding"
	if err := emit.RunE(emit, nil); err == nil {
		t.Fatal("unknown capability/kind schema must fail closed")
	}
}
