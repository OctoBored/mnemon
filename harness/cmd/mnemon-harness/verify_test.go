package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mnemon-dev/mnemon/harness/internal/blob"
	"github.com/mnemon-dev/mnemon/harness/internal/capsule"
)

func verifyFixture(t *testing.T, dir string) (string, string) {
	t.Helper()
	store, err := blob.Open(filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	docBytes := []byte("根因: reconcile.window_hold_ms 应改回 30000。")
	digest, err := store.Put(docBytes)
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, _ := ed25519.GenerateKey(nil)
	doc := capsule.Document{
		Schema: capsule.SchemaV1,
		Header: capsule.Header{
			Producer:  capsule.Producer{Principal: "agent-a@e1", KeyID: capsule.KeyID(pub), PublicKey: base64.StdEncoding.EncodeToString(pub)},
			Boundary:  "space",
			CreatedAt: "2026-07-06T00:00:00Z",
			SchemaIDs: []string{"teamwork/report"},
		},
		Records: []capsule.NormalizedRecord{{
			ID: "r1", Verb: "report",
			Subject: capsule.Subject{Kind: "progress_digest", ID: "project"},
			Actor:   "agent-a@e1", Outcome: "result", ExternalID: "x1",
			Links:     []capsule.Link{{Rel: "artifact", Href: digest}},
			Narrative: "修复方案见附件。",
			Decision:  capsule.Decision{ID: "d1", IngestSeq: 1, AcceptedAt: "2026-07-06T00:00:01Z"},
		}},
		Blobs: []capsule.BlobRef{{Digest: digest, Size: int64(len(docBytes)), MediaType: "text/markdown", Name: "结论.md"}},
	}
	env, _, err := capsule.Sign(doc, priv)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(env)
	capsulePath := filepath.Join(dir, "capsule.json")
	if err := os.WriteFile(capsulePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return capsulePath, filepath.Join(dir, "blobs")
}

func TestVerifyCommandGreenAndTamperRed(t *testing.T) {
	dir := t.TempDir()
	capsulePath, blobDir := verifyFixture(t, dir)

	run := func(path string) (string, error) {
		cmd := newVerifyCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs([]string{path, "--blobs", blobDir})
		err := cmd.Execute()
		return out.String(), err
	}

	out, err := run(capsulePath)
	if err != nil || !strings.Contains(out, "OK capsule sha256:") {
		t.Fatalf("original must verify green: err=%v out=%s", err, out)
	}

	// tamper one hanzi inside the payload
	var env capsule.Envelope
	raw, _ := os.ReadFile(capsulePath)
	_ = json.Unmarshal(raw, &env)
	payload, _ := base64.StdEncoding.DecodeString(env.Payload)
	env.Payload = base64.StdEncoding.EncodeToString([]byte(strings.Replace(string(payload), "修复方案", "修改方案", 1)))
	tamperedPath := filepath.Join(dir, "tampered.json")
	tamperedRaw, _ := json.Marshal(env)
	_ = os.WriteFile(tamperedPath, tamperedRaw, 0o600)

	out, err = run(tamperedPath)
	if err == nil || !strings.Contains(out, capsule.ErrSigInvalid) {
		t.Fatalf("tampered capsule must fail with SIG_INVALID: err=%v out=%s", err, out)
	}
}
