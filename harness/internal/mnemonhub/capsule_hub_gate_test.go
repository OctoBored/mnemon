// Hub contract gate (r4-hub-protocol-v1): capsule push/replay/reject/
// re-adjudicate/pull against a live handler, with Chinese diagnostics
// riding problem+json intact (the C5 shape).
package mnemonhub

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mnemon-dev/mnemon/harness/internal/blob"
	"github.com/mnemon-dev/mnemon/harness/internal/capsule"
	"github.com/mnemon-dev/mnemon/harness/internal/contract"
	"github.com/mnemon-dev/mnemon/harness/internal/mnemond/state"
)

type hubRig struct {
	server *httptest.Server
	grants GrantMap
	blobs  *blob.Store
	priv   ed25519.PrivateKey
	pub    ed25519.PublicKey
}

func newHubRig(t *testing.T, scopes []contract.ResourceRef) *hubRig {
	t.Helper()
	dir := t.TempDir()
	st, err := state.OpenStore(filepath.Join(dir, "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	blobs, err := blob.Open(filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	grants := GrantMap{
		"replica-a@e1": {Principal: "replica-a@e1", Scopes: scopes},
		"replica-b@e1": {Principal: "replica-b@e1", Scopes: scopes},
	}
	hub := New(st, grants, func() string { return "2026-07-06T12:00:00Z" })
	auth := BearerAuthenticator{Tokens: map[string]contract.ActorID{
		"tok-a": "replica-a@e1",
		"tok-b": "replica-b@e1",
	}}
	mux := http.NewServeMux()
	RegisterCapsuleHandlers(mux, hub, auth, blobs, io.Discard)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	pub, priv, _ := ed25519.GenerateKey(nil)
	return &hubRig{server: server, grants: grants, blobs: blobs, priv: priv, pub: pub}
}

func (rig *hubRig) capsuleDoc(subjectKind, subjectID, narrative string, blobData []byte) (capsule.Envelope, string, string) {
	var blobs []capsule.BlobRef
	var links []capsule.Link
	digest := ""
	if blobData != nil {
		digest = blob.Digest(blobData)
		blobs = []capsule.BlobRef{{Digest: digest, Size: int64(len(blobData)), MediaType: "text/markdown", Name: "附件.md"}}
		links = []capsule.Link{{Rel: "artifact", Href: digest}}
	}
	doc := capsule.Document{
		Schema: capsule.SchemaV1,
		Header: capsule.Header{
			Producer:  capsule.Producer{Principal: "replica-a@e1", KeyID: capsule.KeyID(rig.pub), PublicKey: base64.StdEncoding.EncodeToString(rig.pub)},
			Boundary:  "space",
			CreatedAt: "2026-07-06T11:59:00Z",
			SchemaIDs: []string{"teamwork/report"},
		},
		Records: []capsule.NormalizedRecord{{
			ID: "local/replica-a-e1/1", Verb: "report",
			Subject: capsule.Subject{Kind: subjectKind, ID: subjectID},
			Actor:   "agent-a@e1", Outcome: "result", ExternalID: "x1",
			Links:     links,
			Narrative: narrative,
			Decision:  capsule.Decision{ID: "dec_1", IngestSeq: 1, AcceptedAt: "2026-07-06T11:59:30Z"},
		}},
		Blobs: blobs,
	}
	env, id, err := capsule.Sign(doc, rig.priv)
	if err != nil {
		panic(err)
	}
	return env, id, digest
}

func (rig *hubRig) do(t *testing.T, method, path, token string, body []byte, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, rig.server.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, raw
}

func TestHubCapsulePushReplayAndPull(t *testing.T) {
	scopes := []contract.ResourceRef{{Kind: "progress_digest", ID: "payments"}}
	rig := newHubRig(t, scopes)
	blobData := []byte("根因: reconcile.window_hold_ms 应改回 30000。")
	env, id, digest := rig.capsuleDoc("progress_digest", "payments", "排查完成,修复值见附件。", blobData)

	// blob first (protocol §6 push order)
	resp, _ := rig.do(t, http.MethodPut, "/blobs/"+digest, "tok-a", blobData, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("blob put = %d", resp.StatusCode)
	}
	raw, _ := json.Marshal(env)
	resp, body := rig.do(t, http.MethodPost, "/capsules", "tok-a", raw, nil)
	if resp.StatusCode != http.StatusCreated || !strings.Contains(string(body), id) {
		t.Fatalf("first push = %d %s", resp.StatusCode, body)
	}
	// replay: 200 + Idempotency-Replayed
	resp, _ = rig.do(t, http.MethodPost, "/capsules", "tok-a", raw, nil)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Idempotency-Replayed") != "true" {
		t.Fatalf("replay = %d replayed=%q", resp.StatusCode, resp.Header.Get("Idempotency-Replayed"))
	}
	// origin exclusion: A (naming its own producer origin) pulls nothing,
	// B pulls the capsule — exclusion keys on the capsule producer, not the
	// hub credential (self-edge devices may share one credential)
	resp, body = rig.do(t, http.MethodGet, "/capsules?cursor=0&origin=replica-a%40e1", "tok-a", nil, nil)
	var feedA struct {
		Items []json.RawMessage `json:"items"`
	}
	_ = json.Unmarshal(body, &feedA)
	if resp.StatusCode != http.StatusOK || len(feedA.Items) != 0 {
		t.Fatalf("origin must not pull its own capsule: %d %s", resp.StatusCode, body)
	}
	resp, body = rig.do(t, http.MethodGet, "/capsules?cursor=0", "tok-b", nil, nil)
	var feedB struct {
		Items      []json.RawMessage `json:"items"`
		NextCursor int64             `json:"next_cursor"`
	}
	_ = json.Unmarshal(body, &feedB)
	if len(feedB.Items) != 1 || feedB.NextCursor == 0 {
		t.Fatalf("peer must pull the capsule: %s", body)
	}
	// the pulled envelope verifies against the HUB blob lane
	var pulled capsule.Envelope
	_ = json.Unmarshal(feedB.Items[0], &pulled)
	if res := capsule.Verify(pulled, rig.blobs.Resolver()); !res.OK() {
		t.Fatalf("pulled capsule must verify: %v", res.Issues)
	}
	// ETag / 304
	etag := resp.Header.Get("ETag")
	resp, _ = rig.do(t, http.MethodGet, "/capsules?cursor=0", "tok-b", nil, map[string]string{"If-None-Match": etag})
	if resp.StatusCode != http.StatusNotModified {
		t.Fatalf("matching ETag must 304, got %d", resp.StatusCode)
	}
}

func TestHubCapsuleRejectionIsNonTerminal(t *testing.T) {
	// grant covers payments only; the capsule targets billing → 422 scope
	scopes := []contract.ResourceRef{{Kind: "progress_digest", ID: "payments"}}
	rig := newHubRig(t, scopes)
	env, id, _ := rig.capsuleDoc("progress_digest", "billing", "会员权益结算延迟联动排查。", nil)
	raw, _ := json.Marshal(env)
	resp, body := rig.do(t, http.MethodPost, "/capsules", "tok-a", raw, nil)
	if resp.StatusCode != 422 || resp.Header.Get("Content-Type") != "application/problem+json" {
		t.Fatalf("scope overreach = %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	var problem Problem
	_ = json.Unmarshal(body, &problem)
	if problem.Type != ProblemScopeOutOfGrant {
		t.Fatalf("problem type = %s", problem.Type)
	}
	// C5 shape: Chinese diagnostic content survives the round trip intact
	if !strings.Contains(problem.Detail, "越出授权范围") {
		t.Fatalf("chinese diagnostic mangled: %q", problem.Detail)
	}
	// rejection queryable
	_, body = rig.do(t, http.MethodGet, "/capsules/rejected?cursor=0", "tok-a", nil, nil)
	if !strings.Contains(string(body), id) || !strings.Contains(string(body), ProblemScopeOutOfGrant) {
		t.Fatalf("rejected feed must carry the record: %s", body)
	}
	// server-side fix: widen the grant → SAME capsule, SAME id re-push is re-adjudicated
	rig.grants["replica-a@e1"] = ReplicaGrant{Principal: "replica-a@e1", Scopes: []contract.ResourceRef{
		{Kind: "progress_digest", ID: "payments"}, {Kind: "progress_digest", ID: "billing"},
	}}
	resp, body = rig.do(t, http.MethodPost, "/capsules", "tok-a", raw, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("re-adjudication after grant fix must accept: %d %s", resp.StatusCode, body)
	}
}

func TestHubCapsuleBlobMissingThenSamePackage(t *testing.T) {
	scopes := []contract.ResourceRef{{Kind: "progress_digest", ID: "payments"}}
	rig := newHubRig(t, scopes)
	blobData := []byte("排查文档正文。")
	env, _, digest := rig.capsuleDoc("progress_digest", "payments", "附件先漏推。", blobData)
	raw, _ := json.Marshal(env)

	// capsule BEFORE blob → 422 blob-missing (named digest), non-terminal
	resp, body := rig.do(t, http.MethodPost, "/capsules", "tok-a", raw, nil)
	var problem Problem
	_ = json.Unmarshal(body, &problem)
	if resp.StatusCode != 422 || problem.Type != ProblemBlobMissing || !strings.Contains(problem.Detail, digest) {
		t.Fatalf("blob-missing = %d %+v", resp.StatusCode, problem)
	}
	// push the blob, re-push the SAME package → accepted
	if resp, _ := rig.do(t, http.MethodPut, "/blobs/"+digest, "tok-a", blobData, nil); resp.StatusCode != http.StatusCreated {
		t.Fatalf("blob put = %d", resp.StatusCode)
	}
	resp, body = rig.do(t, http.MethodPost, "/capsules", "tok-a", raw, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("same-package re-push after blob = %d %s", resp.StatusCode, body)
	}
}

func TestHubHeadReportsProtocolVersion(t *testing.T) {
	rig := newHubRig(t, nil)
	resp, _ := rig.do(t, http.MethodHead, "/", "tok-a", nil, nil)
	if resp.Header.Get(HubProtocolHeader) != HubProtocolVersion {
		t.Fatalf("HEAD must report the protocol version, got %q", resp.Header.Get(HubProtocolHeader))
	}
}

// The wire security floor, migrated from the legacy /sync suite: unknown
// token → 401 before anything else; an authenticated principal without a
// replica grant → 403; disallowed methods → 405 with Allow.
func TestHubCapsuleWireSecurityFloor(t *testing.T) {
	scopes := []contract.ResourceRef{{Kind: "progress_digest", ID: "payments"}}
	rig := newHubRig(t, scopes)

	for _, probe := range []struct {
		method, path string
	}{
		{http.MethodPost, "/capsules"},
		{http.MethodGet, "/capsules?cursor=0"},
		{http.MethodGet, "/capsules/rejected"},
		{http.MethodPut, "/blobs/sha256:" + strings.Repeat("00", 32)},
	} {
		resp, _ := rig.do(t, probe.method, probe.path, "wrong-token", []byte("{}"), nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s %s with unknown token = %d, want 401", probe.method, probe.path, resp.StatusCode)
		}
	}

	// authenticated but ungranted principal: token resolves, grants say no
	delete(rig.grants, "replica-b@e1")
	resp, _ := rig.do(t, http.MethodGet, "/capsules?cursor=0", "tok-b", nil, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("ungranted pull = %d, want 403", resp.StatusCode)
	}
	env, _, _ := rig.capsuleDoc("progress_digest", "payments", "无授权推送。", nil)
	raw, _ := json.Marshal(env)
	resp, _ = rig.do(t, http.MethodPost, "/capsules", "tok-b", raw, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("ungranted push = %d, want 403", resp.StatusCode)
	}

	// method enforcement
	resp, _ = rig.do(t, http.MethodDelete, "/capsules", "tok-a", nil, nil)
	if resp.StatusCode != http.StatusMethodNotAllowed || resp.Header.Get("Allow") == "" {
		t.Fatalf("DELETE /capsules = %d Allow=%q, want 405 with Allow", resp.StatusCode, resp.Header.Get("Allow"))
	}
	resp, _ = rig.do(t, http.MethodPost, "/capsules/rejected", "tok-a", nil, nil)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST /capsules/rejected = %d, want 405", resp.StatusCode)
	}
}
