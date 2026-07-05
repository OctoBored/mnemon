// Hub-protocol wire freeze (successor of the sync-abi fixture suite): one
// canonical capsule document signed with a FIXED seed must reproduce the
// frozen DSSE envelope bytes and capsule_id in testdata/hub-protocol/.
// A change here is a WIRE change and must be a deliberate protocol rev.
package mnemonhub

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mnemon-dev/mnemon/harness/internal/capsule"
)

func fixtureCapsule() (capsule.Envelope, string) {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	doc := capsule.Document{
		Schema: capsule.SchemaV1,
		Header: capsule.Header{
			Producer:  capsule.Producer{Principal: "replica-a@fixture", KeyID: capsule.KeyID(pub), PublicKey: base64.StdEncoding.EncodeToString(pub)},
			Boundary:  "space",
			CreatedAt: "2026-07-06T00:00:00Z",
			SchemaIDs: []string{"teamwork/report"},
		},
		Records: []capsule.NormalizedRecord{{
			ID: "local/replica-a-fixture/1", Verb: "report",
			Subject: capsule.Subject{Kind: "progress_digest", ID: "payments"},
			Actor:   "agent-a@fixture", Outcome: "result", Scope: "payments/reconcile",
			TTL: "30m", ExternalID: "fixture-1",
			Narrative: `{"summary":"对账窗口保持时间修复完成,恢复 30000。"}`,
			Decision:  capsule.Decision{ID: "dec_fixture", IngestSeq: 1, AcceptedAt: "2026-07-06T00:00:01Z"},
		}},
		Proof: capsule.Proof{Decisions: []capsule.Decision{{ID: "dec_fixture", IngestSeq: 1, AcceptedAt: "2026-07-06T00:00:01Z"}}},
	}
	env, id, err := capsule.Sign(doc, priv)
	if err != nil {
		panic(err)
	}
	return env, id
}

func TestHubProtocolCapsuleWireIsFrozen(t *testing.T) {
	env, id := fixtureCapsule()
	raw, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	dir := filepath.Join("testdata", "hub-protocol")
	envPath := filepath.Join(dir, "capsule-v1.json")
	idPath := filepath.Join(dir, "capsule-v1.id")

	frozenEnv, envErr := os.ReadFile(envPath)
	frozenID, idErr := os.ReadFile(idPath)
	if os.IsNotExist(envErr) || os.IsNotExist(idErr) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(envPath, raw, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(idPath, []byte(id+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("hub-protocol fixture frozen: %s", id)
		return
	}
	if envErr != nil || idErr != nil {
		t.Fatalf("read fixtures: %v %v", envErr, idErr)
	}
	if strings.TrimSpace(string(frozenID)) != id {
		t.Fatalf("capsule_id drifted from the frozen wire:\nfrozen: %s\nnow:    %s", strings.TrimSpace(string(frozenID)), id)
	}
	if !bytes.Equal(frozenEnv, raw) {
		t.Fatal("DSSE envelope bytes drifted from the frozen wire; a deliberate protocol rev must update testdata/hub-protocol with a version bump")
	}
	// and the frozen envelope still verifies offline
	var reread capsule.Envelope
	if err := json.Unmarshal(frozenEnv, &reread); err != nil {
		t.Fatal(err)
	}
	if res := capsule.Verify(reread, nil); !res.OK() {
		t.Fatalf("frozen fixture must verify: %v", res.Issues)
	}
}
