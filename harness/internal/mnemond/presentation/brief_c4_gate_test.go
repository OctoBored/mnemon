// C4 gate (chinese-acceptance-case-plan §C4): rune-safe brief rendering.
// An over-budget pure-CJK brief (no ASCII break points) must truncate on a
// character boundary — utf8-valid, no replacement runes — and the
// truncation fact must land in the render audit (E1-era truncation left no
// trace; render.go's old body[:MaxChars] byte cut split hanzi).
package presentation

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/mnemon-dev/mnemon/harness/internal/mnemond/presentation/view"
)

func c4Projection() view.View {
	long := strings.Repeat("对账窗口保持时间被误设为四万三千二百秒需要立即回滚到三万秒并复核回调重试参数", 40)
	return view.View{Ref: "proj_c4", Digest: "digest_c4", Content: []view.ResourceContent{
		content("progress_digest", "payments", []any{map[string]any{
			"id": "pg-c4", "summary": long, "feedback_kind": "progress",
		}}),
	}}
}

func TestC4GateRuneSafeTruncationWithAuditTrace(t *testing.T) {
	audit := &MemoryAuditSink{}
	r := Renderer{
		Now:       func() time.Time { return mustTime(t, "2026-07-06T10:00:00Z") },
		AuditSink: audit,
	}
	req := Request{
		Principal:    "agent-a@e1",
		Lifecycle:    "remind",
		RenderIntent: IntentBrief,
		// budget lands mid-hanzi on purpose: 501 is not a multiple of 3, and
		// the body is pure CJK at the cut region
		Budget: Budget{MaxChars: 501},
	}
	resp, err := r.RenderPresentation(context.Background(), req, c4Projection())
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Body) > 501 {
		t.Fatalf("budget must bound the body, got %d bytes", len(resp.Body))
	}
	if !utf8.ValidString(resp.Body) {
		t.Fatal("truncated brief must remain valid UTF-8")
	}
	if strings.ContainsRune(resp.Body, '�') {
		t.Fatal("truncated brief must not contain replacement runes")
	}
	last, _ := utf8.DecodeLastRuneInString(resp.Body)
	if last == utf8.RuneError {
		t.Fatal("truncation split the final character")
	}
	if len(audit.Records) != 1 {
		t.Fatalf("render must write exactly one audit record, got %d", len(audit.Records))
	}
	record := audit.Records[0]
	if record.TruncatedFromChars == 0 || record.TruncatedFromChars <= record.BodyChars {
		t.Fatalf("truncation must leave a trace (pre-truncation %d, body %d)", record.TruncatedFromChars, record.BodyChars)
	}

	// same brief under a generous budget: untruncated, no truncation trace
	req.Budget = Budget{MaxChars: 1 << 20}
	resp, err = r.RenderPresentation(context.Background(), req, c4Projection())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Body, "[mnemon:context]") || !strings.Contains(resp.Body, "[mnemon:contract]") {
		t.Fatalf("untruncated brief must carry its sections:\n%.200s", resp.Body)
	}
	if audit.Records[1].TruncatedFromChars != 0 {
		t.Fatal("untruncated render must not claim truncation")
	}
}

func TestTruncateRuneSafeBoundaries(t *testing.T) {
	body := "中文字符串"
	for max := 0; max <= len(body)+1; max++ {
		got := truncateRuneSafe(body, max)
		if max > 0 && len(got) > max {
			t.Fatalf("max=%d: result too long (%d)", max, len(got))
		}
		if !utf8.ValidString(got) {
			t.Fatalf("max=%d: invalid UTF-8 %q", max, got)
		}
	}
	if truncateRuneSafe(body, len(body)) != body {
		t.Fatal("exact budget must not truncate")
	}
}
