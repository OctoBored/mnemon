package app

import (
	"strings"
	"testing"

	"fmt"
	"github.com/mnemon-dev/mnemon/harness/internal/blob"
	"github.com/mnemon-dev/mnemon/harness/internal/contract"
	eventmodel "github.com/mnemon-dev/mnemon/harness/internal/event"
	"github.com/mnemon-dev/mnemon/harness/internal/mnemond/access"
	"github.com/mnemon-dev/mnemon/harness/internal/runtime"
	"path/filepath"
)

// staticCapsuleRemote is a capsule-native fake remote: a fixed feed page.
type staticCapsuleRemote struct {
	items [][]byte
	blobs map[string][]byte
}

func (s staticCapsuleRemote) SyncPush(contract.SyncPushRequest) (contract.SyncPushResponse, error) {
	return contract.SyncPushResponse{}, nil
}

func (s staticCapsuleRemote) SyncPull(contract.SyncPullRequest) (contract.SyncPullResponse, error) {
	return contract.SyncPullResponse{}, nil
}

func (s staticCapsuleRemote) SyncStatus() (contract.SyncStatusResponse, error) {
	return contract.SyncStatusResponse{}, nil
}

func (s staticCapsuleRemote) CapsulePush([]byte) (access.CapsuleAccepted, bool, *access.HubProblem, error) {
	return access.CapsuleAccepted{}, false, nil, nil
}

func (s staticCapsuleRemote) CapsulePull(cursor int64, _ int, _ string) (access.CapsuleFeedPage, error) {
	if cursor >= int64(len(s.items)) {
		return access.CapsuleFeedPage{NextCursor: cursor}, nil
	}
	return access.CapsuleFeedPage{Items: s.items[cursor:], NextCursor: int64(len(s.items))}, nil
}

func (s staticCapsuleRemote) BlobPut(string, []byte) error { return nil }

func (s staticCapsuleRemote) BlobGet(digest string) ([]byte, error) {
	data, ok := s.blobs[digest]
	if !ok {
		return nil, fmt.Errorf("blob %s not on hub", digest)
	}
	return data, nil
}

func countRemoteDiagnostics(t *testing.T, rt *runtime.Runtime, remoteID string, want string) int {
	t.Helper()
	events, err := rt.PendingEvents(0)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	n := 0
	for _, ev := range events {
		if ev.Type == "sync.remote_diagnostic.observed" &&
			eventmodel.PayloadRule(ev.Payload)["remote_id"] == remoteID &&
			strings.Contains(stringPayload(eventmodel.PayloadNarrative(ev.Payload), "diagnostic"), want) {
			n++
		}
		if ev.Type == "sync.diagnostic" &&
			strings.Contains(stringPayload(ev.Payload, "reason"), want) {
			n++
		}
	}
	return n
}

func stringPayload(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}

func TestWorkerPullRemoteDiagnosticLandsDurablyOnce(t *testing.T) {
	root := t.TempDir()
	rt := openServingRuntime(t, root)
	progressRef := contract.ResourceRef{Kind: "progress_digest", ID: "project"}

	// one valid capsule (assembled from a foreign material) + one tampered atom
	priv, pub, err := ReplicaSigningKey(filepath.Join(root, "origin"))
	if err != nil {
		t.Fatal(err)
	}
	originBlobs, _ := blob.Open(filepath.Join(root, "origin-blobs"))
	progress := foreignProgressMaterial("dec-remote-diagnostic-progress", "remote-diagnostic-progress", "valid progress imports beside diagnostic")
	progressEnv, err := contract.SyncedEventEnvelopeFromMaterial(progress)
	if err != nil {
		t.Fatalf("materialize progress: %v", err)
	}
	out, err := BuildOutboundCapsule(progressEnv, "other-replica", priv, pub, originBlobs)
	if err != nil {
		t.Fatalf("assemble capsule: %v", err)
	}
	tampered := []byte(strings.Replace(string(out.Envelope), out.CapsuleID[:20], "sha256:deadbeefdead", 1))
	if string(tampered) == string(out.Envelope) {
		// capsule id is not embedded; corrupt the signature instead
		tampered = []byte(strings.Replace(string(out.Envelope), `"sig":"`, `"sig":"AAAA`, 1))
	}
	remote := staticCapsuleRemote{items: [][]byte{out.Envelope, tampered}}

	if err := syncWorkerPull(rt, remote, "github-sub", nil, root); err != nil {
		t.Fatalf("worker pull with diagnostic: %v", err)
	}
	_, fields, err := rt.Resource(progressRef)
	if err != nil {
		t.Fatalf("read progress: %v", err)
	}
	if content, _ := fields["content"].(string); !strings.Contains(content, "valid progress imports beside diagnostic") {
		t.Fatalf("valid pull capsule must still import:\n%s", content)
	}
	if got := countRemoteDiagnostics(t, rt, "github-sub", "failed verification"); got != 2 {
		t.Fatalf("bad atom must land as observation + durable diagnostic, got %d matching events", got)
	}

	if err := syncWorkerPull(rt, remote, "github-sub", nil, root); err != nil {
		t.Fatalf("repeat worker pull with diagnostic: %v", err)
	}
	if got := countRemoteDiagnostics(t, rt, "github-sub", "failed verification"); got != 2 {
		t.Fatalf("repeat pull must dedupe remote diagnostic, got %d matching events", got)
	}
}
