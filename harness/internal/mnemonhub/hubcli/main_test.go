package hubcli

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"crypto/ed25519"
	"encoding/base64"
	"github.com/mnemon-dev/mnemon/harness/internal/capsule"
	"github.com/mnemon-dev/mnemon/harness/internal/contract"
	"github.com/mnemon-dev/mnemon/harness/internal/mnemond/access"
	"strconv"
)

func writeReplicas(t *testing.T, dir, content string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, "replicas.json")
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeToken(t *testing.T, dir, name, token string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestHelpDescribesRemoteExchangeBackend(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"help"}, {"serve", "--help"}} {
		var errw bytes.Buffer
		err := Run(context.Background(), args, io.Discard, &errw)
		if err != nil {
			t.Fatalf("%v help should exit successfully, got %v", args, err)
		}
		got := errw.String()
		for _, want := range []string{"remote event exchange backend", "replica push", "pull", "status", "cursors", "tenant boundaries", "Commands:", "serve"} {
			if !strings.Contains(got, want) {
				t.Fatalf("%v mnemon-hub help missing %q:\n%s", args, want, got)
			}
		}
		for _, blocked := range []string{"managed runtime", "Multica projection", "local drive source"} {
			if strings.Contains(got, blocked) {
				t.Fatalf("%v mnemon-hub help leaked non-exchange wording %q:\n%s", args, blocked, got)
			}
		}
	}
}

const twoReplicaDoc = `{
  "schema_version": 1,
  "replicas": [
    {"principal": "replica-a@team", "credential_ref": "a.token",
     "scopes": [{"kind": "memory", "id": "project"}, {"kind": "skill", "id": "project"}]},
    {"principal": "replica-b@team", "credential_ref": "b.token",
     "scopes": [{"kind": "memory", "id": "project"}]}
  ]
}`

// replicas.json is fail-closed at every gate: strict decoding (unknown fields), schema version,
// world-readable refusal, empty scopes, missing credential, duplicate principal/token.
func TestLoadReplicasFailClosed(t *testing.T) {
	dir := t.TempDir()
	writeToken(t, dir, "a.token", "tok-a")
	writeToken(t, dir, "b.token", "tok-b")

	path := writeReplicas(t, dir, twoReplicaDoc, 0o600)
	grants, tokens, err := loadReplicas(path)
	if err != nil {
		t.Fatalf("valid replicas.json: %v", err)
	}
	if len(grants) != 2 || len(tokens) != 2 || tokens["tok-a"] != "replica-a@team" {
		t.Fatalf("grants/tokens not assembled: %+v / %+v", grants, tokens)
	}
	if g, ok := grants.Grant("replica-b@team", contract.SyncVerbPull); !ok || len(g.Scopes) != 1 || g.Scopes[0].Kind != "memory" {
		t.Fatalf("replica-b grant scopes wrong: %+v ok=%v", g, ok)
	}

	cases := []struct {
		name string
		doc  string
		mode os.FileMode
		want string
	}{
		{"world readable", twoReplicaDoc, 0o644, "world-readable"},
		{"unknown field", `{"schema_version":1,"replicas":[{"principal":"p","credential_ref":"a.token","scopes":[{"kind":"memory","id":"project"}],"extra":true}]}`, 0o600, "unknown field"},
		{"bad schema", `{"schema_version":2,"replicas":[]}`, 0o600, "schema_version"},
		{"no replicas", `{"schema_version":1,"replicas":[]}`, 0o600, "no replicas"},
		{"empty scopes", `{"schema_version":1,"replicas":[{"principal":"p","credential_ref":"a.token","scopes":[]}]}`, 0o600, "scopes must be non-empty"},
		{"missing credential", `{"schema_version":1,"replicas":[{"principal":"p","scopes":[{"kind":"memory","id":"project"}]}]}`, 0o600, "credential_ref is required"},
		{"duplicate principal", `{"schema_version":1,"replicas":[{"principal":"p","credential_ref":"a.token","scopes":[{"kind":"memory","id":"project"}]},{"principal":"p","credential_ref":"b.token","scopes":[{"kind":"memory","id":"project"}]}]}`, 0o600, "duplicate principal"},
		{"duplicate token", `{"schema_version":1,"replicas":[{"principal":"p1","credential_ref":"a.token","scopes":[{"kind":"memory","id":"project"}]},{"principal":"p2","credential_ref":"a.token","scopes":[{"kind":"memory","id":"project"}]}]}`, 0o600, "also bound"},
	}
	for _, tc := range cases {
		caseDir := t.TempDir()
		writeToken(t, caseDir, "a.token", "tok-a")
		writeToken(t, caseDir, "b.token", "tok-b")
		p := writeReplicas(t, caseDir, tc.doc, tc.mode)
		if _, _, err := loadReplicas(p); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: want error containing %q, got %v", tc.name, tc.want, err)
		}
	}

	// MED-2: the credential token file holds the actual secret — a world-readable (0644) token file
	// is refused even when replicas.json itself is correctly 0600.
	credDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(credDir, "a.token"), []byte("tok-a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(credDir, "a.token"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(credDir, "b.token"), []byte("tok-b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	credPath := writeReplicas(t, credDir, twoReplicaDoc, 0o600)
	if _, _, err := loadReplicas(credPath); err == nil || !strings.Contains(err.Error(), "world-readable") {
		t.Fatalf("world-readable token file must be refused: %v", err)
	}
}

func TestDevSelfsignedGeneratesUsablePair(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "certs")
	certPath, keyPath, err := generateSelfSigned(dir)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if _, err := tls.LoadX509KeyPair(certPath, keyPath); err != nil {
		t.Fatalf("generated pair must load as a TLS key pair: %v", err)
	}
	if info, err := os.Stat(keyPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("key must be 0600: %v %v", info, err)
	}
	var out bytes.Buffer
	if err := Run(context.Background(), []string{"--dev-selfsigned", dir}, &out, &out); err != nil {
		t.Fatalf("run --dev-selfsigned: %v", err)
	}
	if !strings.Contains(out.String(), certPath) || !strings.Contains(out.String(), keyPath) {
		t.Fatalf("--dev-selfsigned must print the pair paths, got:\n%s", out.String())
	}
}

func TestRunFlagValidation(t *testing.T) {
	var out bytes.Buffer
	if err := Run(context.Background(), nil, &out, &out); err == nil || !strings.Contains(err.Error(), "--store and --replicas") {
		t.Fatalf("missing flags must fail: %v", err)
	}
	if err := Run(context.Background(), []string{"--store", "x.db", "--replicas", "r.json", "--tls-cert", "c.pem"}, &out, &out); err == nil || !strings.Contains(err.Error(), "set together") {
		t.Fatalf("lone --tls-cert must fail: %v", err)
	}
	if err := Run(context.Background(), []string{"serve", "--store", "x.db", "--replicas", "r.json", "--tls-cert", "c.pem"}, &out, &out); err == nil || !strings.Contains(err.Error(), "set together") {
		t.Fatalf("serve alias must parse service flags: %v", err)
	}
}

// Full hub integration over native TLS: mnemon-hub serves push/pull/status with the dev self-signed
// pair; the SAME channel sync client used against the co-hosted hub talks to it via ca_file
// (dual-form proof); scopes differ per principal; audit lines land on stdout.
func TestMnemonHubServesSyncOverTLS(t *testing.T) {
	work := t.TempDir()
	certPath, keyPath, err := generateSelfSigned(filepath.Join(work, "certs"))
	if err != nil {
		t.Fatal(err)
	}
	repDir := filepath.Join(work, "rep")
	if err := os.MkdirAll(repDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeToken(t, repDir, "a.token", "tok-a")
	writeToken(t, repDir, "b.token", "tok-b")
	replicasPath := writeReplicas(t, repDir, twoReplicaDoc, 0o600)

	var out lockedBuffer
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, []string{
			"--addr", "127.0.0.1:0",
			"--store", filepath.Join(work, "hub", "hub.db"),
			"--replicas", replicasPath,
			"--tls-cert", certPath,
			"--tls-key", keyPath,
		}, &out, &out)
	}()
	endpoint := waitForListen(t, &out)
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Fatalf("mnemon-hub exited with error: %v", err)
		}
	}()

	clientA, err := access.NewSyncClient(endpoint, access.SyncClientConfig{Token: "tok-a", CAFile: certPath})
	if err != nil {
		t.Fatal(err)
	}
	clientB, err := access.NewSyncClient(endpoint, access.SyncClientConfig{Token: "tok-b", CAFile: certPath})
	if err != nil {
		t.Fatal(err)
	}

	// capsule wire over TLS: A pushes one signed capsule, B pulls it.
	env, capsuleID := hubTestCapsule(t, "memory", "project", "pushed through mnemon-hub")
	raw, _ := json.Marshal(env)
	accepted, replayed, problem, err := clientA.CapsulePush(raw)
	if err != nil || problem != nil || replayed || accepted.CapsuleID != capsuleID {
		t.Fatalf("push over TLS: %+v replayed=%v problem=%+v err=%v", accepted, replayed, problem, err)
	}
	page, err := clientB.CapsulePull(0, 10, "")
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("pull over TLS: %+v err=%v", page, err)
	}

	// B's grant is memory-only: pushing a skill capsule is clamped (422 problem).
	skillEnv, _ := hubTestCapsule(t, "skill", "project", "scope probe")
	skillRaw, _ := json.Marshal(skillEnv)
	_, _, problem, err = clientB.CapsulePush(skillRaw)
	if err != nil || problem == nil {
		t.Fatalf("out-of-scope push must reject with a problem: %+v err=%v", problem, err)
	}

	// An unknown token is 401 (the wire security floor under TLS).
	badClient, err := access.NewSyncClient(endpoint, access.SyncClientConfig{Token: "wrong", CAFile: certPath})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := badClient.CapsulePull(0, 10, ""); err == nil {
		t.Fatal("unknown token must be unauthorized")
	}

	for _, want := range []string{
		"principal=replica-a@team verb=capsules.push result=ok",
		"principal=replica-b@team verb=capsules.pull result=ok",
		"principal=replica-b@team verb=capsules.push result=rejected",
		"principal=- verb=capsules result=unauthorized",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("audit line %q missing in:\n%s", want, out.String())
		}
	}
}

var listenLine = regexp.MustCompile(`mnemon-hub: listening on (https?://[^\s]+) `)

func waitForListen(t *testing.T, out *lockedBuffer) string {
	t.Helper()
	for i := 0; i < 100; i++ {
		if m := listenLine.FindStringSubmatch(out.String()); m != nil {
			return m[1]
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("mnemon-hub did not report a listen address:\n%s", out.String())
	return ""
}

// lockedBuffer keeps the run goroutine's writes race-free with the test's polling reads.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// hubTestCapsule signs one minimal capsule for the boot round trip.
func hubTestCapsule(t *testing.T, kind, id, narrative string) (capsule.Envelope, string) {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(7 + i)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	doc := capsule.Document{
		Schema: capsule.SchemaV1,
		Header: capsule.Header{
			Producer:  capsule.Producer{Principal: "local-a", KeyID: capsule.KeyID(pub), PublicKey: base64.StdEncoding.EncodeToString(pub)},
			Boundary:  "space",
			CreatedAt: "2026-07-06T00:00:00Z",
			SchemaIDs: []string{kind},
		},
		Records: []capsule.NormalizedRecord{{
			ID: "local/local-a/1", Verb: "report",
			Subject: capsule.Subject{Kind: kind, ID: id},
			Actor:   "codex@a", ExternalID: "dec-" + kind,
			Narrative: `{"summary":` + strconv.Quote(narrative) + `}`,
			Decision:  capsule.Decision{ID: "dec-" + kind, IngestSeq: 1, AcceptedAt: "2026-07-06T00:00:01Z"},
		}},
	}
	env, capsuleID, err := capsule.Sign(doc, priv)
	if err != nil {
		t.Fatal(err)
	}
	return env, capsuleID
}
