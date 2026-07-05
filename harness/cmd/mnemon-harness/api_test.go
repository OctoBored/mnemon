package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAPIEscapeHatchSendsCredentialedRequest(t *testing.T) {
	var gotPrincipal, gotMethod, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPrincipal = r.Header.Get("X-Mnemon-Principal")
		gotMethod = r.Method
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)

	controlAddr = server.URL
	controlPrincipal = "agent-a"
	controlToken = ""
	controlTokenFile = ""

	cmd := rootSub(t, "api")
	if !cmd.Hidden {
		t.Fatal("api escape hatch must stay hidden (the only hidden command)")
	}
	var out strings.Builder
	cmd.SetOut(&out)
	if err := cmd.RunE(cmd, []string{"get", "status"}); err != nil {
		t.Fatalf("api failed: %v", err)
	}
	if gotMethod != http.MethodGet || gotPath != "/status" || gotPrincipal != "agent-a" {
		t.Fatalf("request shape wrong: %s %s principal=%s", gotMethod, gotPath, gotPrincipal)
	}
	if !strings.Contains(out.String(), `"ok":true`) {
		t.Fatalf("response body must pass through, got %s", out.String())
	}
}
