package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mnemon-dev/mnemon/harness/internal/contract"
	"github.com/mnemon-dev/mnemon/harness/internal/mnemond/presentation/view"
)

func stubViewServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/presentation-view", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(view.View{
			Ref:    "proj_abc",
			Digest: "abc",
			Content: []view.ResourceContent{
				{
					Ref:     contract.ResourceRef{Kind: "progress_digest", ID: "payments"},
					Version: 3,
					Fields:  map[string]any{"summary": "对账窗口修复完成,恢复 30000。"},
				},
				{
					Ref:     contract.ResourceRef{Kind: "teamwork_signal", ID: "s1"},
					Version: 1,
					Fields:  map[string]any{"statement": "需要更多人手排查回调重试。"},
				},
			},
		})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func TestRecallKeywordAndKindFilter(t *testing.T) {
	server := stubViewServer(t)
	controlAddr = server.URL
	controlPrincipal = "agent-a"
	controlToken = ""
	controlTokenFile = ""

	run := func(keyword, kind string) string {
		t.Helper()
		recallKind = kind
		recallJSON = false
		cmd := rootSub(t, "recall")
		var out strings.Builder
		cmd.SetOut(&out)
		if err := cmd.RunE(cmd, []string{keyword}); err != nil {
			t.Fatalf("recall %q failed: %v", keyword, err)
		}
		return out.String()
	}

	got := run("对账窗口", "")
	if !strings.Contains(got, "progress_digest/payments") || strings.Contains(got, "teamwork_signal") {
		t.Fatalf("keyword must hit only the matching resource:\n%s", got)
	}
	got = run("排查", "teamwork_signal")
	if !strings.Contains(got, "teamwork_signal/s1") || strings.Contains(got, "progress_digest") {
		t.Fatalf("--kind must restrict matches:\n%s", got)
	}
	got = run("不存在的关键词", "")
	if !strings.Contains(got, "no matches") {
		t.Fatalf("empty result must say so:\n%s", got)
	}
}
