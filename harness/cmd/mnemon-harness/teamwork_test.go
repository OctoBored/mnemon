package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/mnemon-dev/mnemon/harness/internal/blob"
	"github.com/mnemon-dev/mnemon/harness/internal/contract"
)

// stubNode captures /ingest envelopes and accepts content-addressed /blobs puts.
func stubNode(t *testing.T) (*httptest.Server, *[]contract.ObservationEnvelope, *[]string) {
	t.Helper()
	var envelopes []contract.ObservationEnvelope
	var blobPuts []string
	mux := http.NewServeMux()
	mux.HandleFunc("/ingest", func(w http.ResponseWriter, r *http.Request) {
		var env contract.ObservationEnvelope
		if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		envelopes = append(envelopes, env)
		_ = json.NewEncoder(w).Encode(map[string]any{"seq": len(envelopes)})
	})
	mux.HandleFunc("/blobs/", func(w http.ResponseWriter, r *http.Request) {
		digest := strings.TrimPrefix(r.URL.Path, "/blobs/")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if blob.Digest(body) != digest {
			http.Error(w, "blob-digest-mismatch", http.StatusBadRequest)
			return
		}
		blobPuts = append(blobPuts, digest)
		w.WriteHeader(http.StatusCreated)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server, &envelopes, &blobPuts
}

func teamworkSub(t *testing.T, name string) *cobra.Command {
	t.Helper()
	for _, c := range teamworkCmd.Commands() {
		if c.Name() == name {
			return c
		}
	}
	t.Fatalf("teamwork subcommand %q not registered", name)
	return nil
}

func resetProgressVars() {
	controlTeamworkProgressOutcome = "progress"
	controlTeamworkProgressSummary = ""
	controlTeamworkProgressAssignmentRef = ""
	controlTeamworkProgressScope = ""
	controlTeamworkProgressBlocker = ""
	controlTeamworkProgressResult = ""
	controlTeamworkProgressChanged = nil
	controlTeamworkProgressSuggestedNext = ""
	controlTeamworkProgressEvidence = nil
	controlTeamworkProgressArtifacts = nil
	teamworkReportOutcome = ""
	teamworkReportAttach = nil
}

func TestTeamworkReportOutcomeAndAttachWire(t *testing.T) {
	server, envelopes, blobPuts := stubNode(t)
	controlAddr = server.URL
	controlPrincipal = "agent-a"
	controlToken = ""
	controlTokenFile = ""
	resetProgressVars()

	artifact := filepath.Join(t.TempDir(), "结论.md")
	content := []byte("根因: reconcile.window_hold_ms 应改回 30000。")
	if err := os.WriteFile(artifact, content, 0o600); err != nil {
		t.Fatal(err)
	}
	wantDigest := blob.Digest(content)

	report := teamworkSub(t, "report")
	for k, v := range map[string]string{
		"summary": "排查完成,修复值见附件。",
		"outcome": "result",
		"result":  "对账窗口恢复 30000。",
	} {
		if err := report.Flags().Set(k, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := report.Flags().Set("attach", artifact); err != nil {
		t.Fatal(err)
	}
	teamworkReportAttach = []string{artifact}
	report.SetOut(io.Discard)
	if err := report.RunE(report, nil); err != nil {
		t.Fatalf("teamwork report failed: %v", err)
	}

	if len(*blobPuts) != 1 || (*blobPuts)[0] != wantDigest {
		t.Fatalf("attach must PUT the content-addressed blob, got %v", *blobPuts)
	}
	if len(*envelopes) != 1 {
		t.Fatalf("expected one ingest, got %d", len(*envelopes))
	}
	env := (*envelopes)[0]
	if env.Event.Type != "progress_digest.write_candidate.observed" {
		t.Fatalf("wire event type changed: %s", env.Event.Type)
	}
	rule, _ := env.Event.Payload["rule"].(map[string]any)
	if rule["outcome"] != "result" {
		t.Fatalf("--outcome must map onto the wire outcome, got %v", rule["outcome"])
	}
	refs, _ := env.Event.Payload["refs"].(map[string]any)
	arts, _ := refs["artifact_refs"].([]any)
	if len(arts) != 1 || arts[0] != wantDigest {
		t.Fatalf("attached digest must ride artifact_refs, got %v", refs["artifact_refs"])
	}
}

func TestTeamworkDialectAndControlFaceProduceSameWire(t *testing.T) {
	server, envelopes, _ := stubNode(t)
	controlAddr = server.URL
	controlPrincipal = "agent-a"
	controlToken = ""
	controlTokenFile = ""

	run := func(cmd *cobra.Command) {
		t.Helper()
		resetProgressVars()
		controlTeamworkProgressSummary = "阶段推进:契约测试已过半。"
		controlTeamworkProgressScope = "payments/reconcile"
		cmd.SetOut(io.Discard)
		if err := cmd.RunE(cmd, nil); err != nil {
			t.Fatalf("%s failed: %v", cmd.Name(), err)
		}
	}

	run(teamworkSub(t, "report"))
	run(controlTeamworkProgressCmd)

	if len(*envelopes) != 2 {
		t.Fatalf("expected two ingests, got %d", len(*envelopes))
	}
	a, b := (*envelopes)[0], (*envelopes)[1]
	if a.Event.Type != b.Event.Type {
		t.Fatalf("faces diverge on event type: %s vs %s", a.Event.Type, b.Event.Type)
	}
	if !reflect.DeepEqual(a.Event.Payload, b.Event.Payload) {
		t.Fatalf("faces diverge on payload:\nnew: %#v\nold: %#v", a.Event.Payload, b.Event.Payload)
	}
}
