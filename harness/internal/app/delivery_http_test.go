package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/mnemon-dev/mnemon/harness/internal/contract"
	eventmodel "github.com/mnemon-dev/mnemon/harness/internal/event"
	"github.com/mnemon-dev/mnemon/harness/internal/mnemond/access"
	"github.com/mnemon-dev/mnemon/harness/internal/mnemond/presentation"
	"github.com/mnemon-dev/mnemon/harness/internal/runtime"
)

func deliveryTestServer(t *testing.T) (*httptest.Server, *access.Client) {
	t.Helper()
	refs := []contract.ResourceRef{
		{Kind: "assignment", ID: "project"},
		{Kind: "progress_digest", ID: "project"},
	}
	binding := access.HostAgentBinding("codex-a@project", "http://127.0.0.1:8787", refs)
	binding.AllowedObservedTypes = []string{
		"assignment.write_candidate.observed",
		"progress_digest.write_candidate.observed",
	}
	loaded := access.LoadedBindings{
		Bindings: []access.ChannelBinding{binding},
		Tokens:   map[string]contract.ActorID{"tok-a": "codex-a@project"},
	}
	rc, err := LocalRuntimeConfigFromBindings(loaded.Bindings, nil)
	if err != nil {
		t.Fatalf("runtime config: %v", err)
	}
	rt, err := runtime.OpenRuntime(filepath.Join(t.TempDir(), "delivery.db"), rc)
	if err != nil {
		t.Fatalf("open runtime: %v", err)
	}
	t.Cleanup(func() { rt.Close() })
	bindings, err := access.NewBindingSet(loaded.Bindings...)
	if err != nil {
		t.Fatalf("binding set: %v", err)
	}
	srv := httptest.NewServer(NewLocalHTTPHandler(rt, access.TokenAuthenticator{Tokens: loaded.Tokens}, bindings, presentation.Renderer{}, nil))
	t.Cleanup(srv.Close)
	return srv, access.NewClientWithToken(srv.URL, "tok-a")
}

func getDeliveries(t *testing.T, base, query string) (presentation.DeliveryFeed, int) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, base+"/surface/deliveries"+query, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer tok-a")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var feed presentation.DeliveryFeed
	if res.StatusCode == http.StatusOK {
		if err := json.NewDecoder(res.Body).Decode(&feed); err != nil {
			t.Fatal(err)
		}
	}
	return feed, res.StatusCode
}

func TestSurfaceDeliveriesFeedMilestonesAndCursor(t *testing.T) {
	srv, client := deliveryTestServer(t)

	seed := func(externalID, eventType string, payload map[string]any) {
		t.Helper()
		rec, err := client.IngestObserve("", contract.ObservationEnvelope{
			ExternalID: externalID,
			Event:      contract.Event{Type: eventType, Payload: payload},
		})
		if err != nil || rec.ProcessingError != "" {
			t.Fatalf("seed %s: rec=%+v err=%v", externalID, rec, err)
		}
	}
	seed("dlv-asg", "assignment.write_candidate.observed",
		r2Assignment("payments/reconcile", "30m", "agent-b@e1", "复核回调重试参数。", "result", "seq:1"))
	seed("dlv-progress", "progress_digest.write_candidate.observed", r2Progress("阶段推进:契约测试过半。"))
	seed("dlv-result", "progress_digest.write_candidate.observed", eventmodel.BuildPayload(
		map[string]any{"outcome": "result"},
		map[string]any{"summary": "排查完成:根因已定位。", "result": "修复值 30000 已生效。"},
		nil,
	))

	if _, status := getDeliveries(t, srv.URL, ""); status != http.StatusBadRequest {
		t.Fatalf("missing surface param must 400, got %d", status)
	}

	feed, status := getDeliveries(t, srv.URL, "?surface=multica")
	if status != http.StatusOK {
		t.Fatalf("feed status = %d", status)
	}
	if len(feed.Deliveries) != 2 {
		t.Fatalf("expected assignment + result deliveries (progress stays silent), got %d: %+v", len(feed.Deliveries), feed.Deliveries)
	}
	if feed.Deliveries[0].Role != presentation.DeliveryRoleActivate || feed.Deliveries[0].Metadata["mnemon.target_agent_id"] != "agent-b@e1" {
		t.Fatalf("new assignment must activate the assignee, got %+v", feed.Deliveries[0])
	}
	if feed.Deliveries[1].Action != presentation.DeliveryActionWriteComment {
		t.Fatalf("result digest must deliver a comment, got %+v", feed.Deliveries[1])
	}
	if feed.NextCursor <= 0 {
		t.Fatalf("next_cursor must advance, got %d", feed.NextCursor)
	}

	resumed, _ := getDeliveries(t, srv.URL, "?surface=multica&cursor="+jsonNumber(feed.NextCursor))
	if len(resumed.Deliveries) != 0 || resumed.NextCursor != feed.NextCursor {
		t.Fatalf("resume past the feed must be empty and hold the cursor, got %+v", resumed)
	}
}

func jsonNumber(n int64) string {
	raw, _ := json.Marshal(n)
	return string(raw)
}
