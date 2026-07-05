package blob

import (
	"strings"
	"testing"
)

func TestPutGetRoundTripWithCJKContent(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("对账窗口保持时间修复方案:改回 30000。")
	digest, err := s.Put(data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("digest form: %s", digest)
	}
	got, err := s.Get(digest)
	if err != nil || string(got) != string(data) {
		t.Fatalf("round trip failed: %v", err)
	}
	if !s.Has(digest) {
		t.Fatal("Has must be true after Put")
	}
	// idempotent re-put
	again, err := s.Put(data)
	if err != nil || again != digest {
		t.Fatalf("re-put must be idempotent: %v %s", err, again)
	}
}

func TestPutExpectFailsClosedOnMismatch(t *testing.T) {
	s, _ := Open(t.TempDir())
	err := s.PutExpect("sha256:"+strings.Repeat("00", 32), []byte("内容与地址不符"))
	if err == nil {
		t.Fatal("PutExpect must reject digest mismatch")
	}
}

func TestMalformedDigestRejected(t *testing.T) {
	s, _ := Open(t.TempDir())
	if _, err := s.Get("sha256:zz"); err == nil {
		t.Fatal("malformed digest must be rejected")
	}
	if s.Has("not-a-digest") {
		t.Fatal("Has must be false for malformed digest")
	}
}
