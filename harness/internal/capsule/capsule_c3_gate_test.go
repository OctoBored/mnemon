// C3 gate (chinese-acceptance-case-plan §C1环一): capsule tamper detection
// with CJK content — original verifies green, a single-hanzi narrative edit
// and a single blob byte flip both go red with located error codes, and JCS
// serialization is stable for CJK.
package capsule

import (
	"crypto/ed25519"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/mnemon-dev/mnemon/harness/internal/blob"
)

func c3Document(t *testing.T, store *blob.Store, pub ed25519.PublicKey) Document {
	t.Helper()
	doc := []byte("# 支付回调对账延迟事故排查\n\n根因: reconcile.window_hold_ms 被误设为 43200000, 应改回 30000。\n干扰项: callback.retry_backoff_ms 无需修改。\n")
	digest, err := store.Put(doc)
	if err != nil {
		t.Fatal(err)
	}
	return Document{
		Schema: SchemaV1,
		Header: Header{
			Parent:    "",
			Producer:  Producer{Principal: "agent-a@e1", KeyID: KeyID(pub), PublicKey: base64.StdEncoding.EncodeToString(pub)},
			Boundary:  "space",
			CreatedAt: "2026-07-06T00:00:00Z",
			SchemaIDs: []string{"teamwork/report"},
		},
		Records: []NormalizedRecord{{
			ID: "local/agent-a-e1/1", Verb: "report",
			Subject: Subject{Kind: "progress_digest", ID: "project"},
			Actor:   "agent-a@e1", Assignee: "agent-b@e1",
			Outcome: "result", Scope: "payments/reconcile",
			ExternalID: "prog-c3-1",
			Links:      []Link{{Rel: "artifact", Href: digest}},
			Narrative:  "排查完成:根因已定位为对账窗口保持配置被误设,修复值见附件文档。",
			Decision:   Decision{ID: "dec_c3", IngestSeq: 42, AcceptedAt: "2026-07-06T00:00:01Z"},
		}},
		Blobs: []BlobRef{{Digest: digest, Size: int64(len(doc)), MediaType: "text/markdown", Name: "排查文档.md"}},
		Proof: Proof{Decisions: []Decision{{ID: "dec_c3", IngestSeq: 42, AcceptedAt: "2026-07-06T00:00:01Z"}}},
	}
}

func TestC3GateOriginalVerifiesGreen(t *testing.T) {
	store, _ := blob.Open(t.TempDir())
	pub, priv, _ := ed25519.GenerateKey(nil)
	env, id, err := Sign(c3Document(t, store, pub), priv)
	if err != nil {
		t.Fatal(err)
	}
	res := Verify(env, store.Resolver())
	if !res.OK() {
		t.Fatalf("original capsule must verify green, got %v", res.Issues)
	}
	if res.CapsuleID != id {
		t.Fatalf("capsule id mismatch: %s vs %s", res.CapsuleID, id)
	}
}

func TestC3GateSingleHanziTamperGoesRed(t *testing.T) {
	store, _ := blob.Open(t.TempDir())
	pub, priv, _ := ed25519.GenerateKey(nil)
	env, _, err := Sign(c3Document(t, store, pub), priv)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := base64.StdEncoding.DecodeString(env.Payload)
	tampered := strings.Replace(string(payload), "排查完成", "排查完毕", 1)
	if tampered == string(payload) {
		t.Fatal("tamper target not found")
	}
	env.Payload = base64.StdEncoding.EncodeToString([]byte(tampered))
	res := Verify(env, store.Resolver())
	if res.OK() {
		t.Fatal("hanzi-tampered capsule must fail verification")
	}
	found := false
	for _, issue := range res.Issues {
		if issue.Code == ErrSigInvalid {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected SIG_INVALID, got %v", res.Issues)
	}
}

func TestC3GateBlobByteFlipGoesRed(t *testing.T) {
	dir := t.TempDir()
	store, _ := blob.Open(dir)
	pub, priv, _ := ed25519.GenerateKey(nil)
	docModel := c3Document(t, store, pub)
	env, _, err := Sign(docModel, priv)
	if err != nil {
		t.Fatal(err)
	}
	// flip one byte inside the stored blob (simulates content substitution)
	digest := docModel.Blobs[0].Digest
	data, _ := store.Get(digest)
	data[len(data)/2] ^= 0xFF
	hexPart := strings.TrimPrefix(digest, "sha256:")
	if err := writeRaw(dir, hexPart, data); err != nil {
		t.Fatal(err)
	}
	res := Verify(env, store.Resolver())
	if res.OK() {
		t.Fatal("blob-tampered capsule must fail verification")
	}
	located := false
	for _, issue := range res.Issues {
		// store-level integrity turns the read into a miss (BLOB_MISSING) or,
		// with a raw resolver, a digest mismatch — both codes locate the blob.
		if (issue.Code == ErrBlobDigestBad || issue.Code == ErrBlobMissing) && strings.Contains(issue.Where, digest) {
			located = true
		}
	}
	if !located {
		t.Fatalf("expected blob issue locating %s, got %v", digest, res.Issues)
	}
}

func TestC3GateJCSStableForCJK(t *testing.T) {
	store, _ := blob.Open(t.TempDir())
	pub, _, _ := ed25519.GenerateKey(nil)
	doc := c3Document(t, store, pub)
	a, err := CanonicalBytes(doc)
	if err != nil {
		t.Fatal(err)
	}
	b, err := CanonicalBytes(doc)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatal("JCS serialization must be byte-stable for CJK content")
	}
	if ID(a) != ID(b) {
		t.Fatal("capsule id must be stable")
	}
}

func TestC3GateChainContinuity(t *testing.T) {
	store, _ := blob.Open(t.TempDir())
	pub, priv, _ := ed25519.GenerateKey(nil)
	first := c3Document(t, store, pub)
	env1, id1, err := Sign(first, priv)
	if err != nil {
		t.Fatal(err)
	}
	second := c3Document(t, store, pub)
	second.Header.Parent = id1
	second.Records[0].ExternalID = "prog-c3-2"
	env2, _, err := Sign(second, priv)
	if err != nil {
		t.Fatal(err)
	}
	results := VerifyChain([]Envelope{env1, env2}, store.Resolver())
	for i, r := range results {
		if !r.OK() {
			t.Fatalf("chain capsule %d: %v", i, r.Issues)
		}
	}
	// broken chain: forge the parent
	second.Header.Parent = "sha256:" + strings.Repeat("00", 32)
	env2b, _, _ := Sign(second, priv)
	results = VerifyChain([]Envelope{env1, env2b}, store.Resolver())
	broken := false
	for _, issue := range results[1].Issues {
		if issue.Code == ErrChainBroken {
			broken = true
		}
	}
	if !broken {
		t.Fatalf("expected CHAIN_BROKEN, got %v", results[1].Issues)
	}
}
