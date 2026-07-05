// Golden tests 1-6 (r4-multica-adapter-contract; C8-C10 的 fake 轨锚点):
// 1 可见 comment 纯自然语言, 禁止集零命中
// 2 metadata 键齐全(机器通道)
// 3 provider-assigned issue 不被推进 active(GetIssue 实查, fail-closed)
// 4 同 event_ref 重放零新 comment(台账接线)
// 5 发送机路径/digest 永不出现在可见正文(artifact 本机物化, 正文只文件名)
// 6 comment 仅经 explicit import 成为 observed(dispatcher 零提交面)
package multica

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mnemon-dev/mnemon/harness/internal/mnemond/presentation"
)

type fakeMulticaClient struct {
	comments    []string
	metadata    []map[string]string
	statusSets  []string
	issue       MulticaIssue
	getIssueErr error
}

func (f *fakeMulticaClient) AddIssueComment(_ context.Context, issueID, content string) (MulticaComment, error) {
	f.comments = append(f.comments, content)
	return MulticaComment{ID: fmt.Sprintf("c%d", len(f.comments))}, nil
}

func (f *fakeMulticaClient) SetIssueMetadataMap(_ context.Context, _ string, values map[string]string) error {
	f.metadata = append(f.metadata, values)
	return nil
}

func (f *fakeMulticaClient) SetIssueStatus(_ context.Context, _ string, status string) (MulticaIssue, error) {
	f.statusSets = append(f.statusSets, status)
	f.issue.Status = status
	return f.issue, nil
}

func (f *fakeMulticaClient) GetIssue(_ context.Context, _ string) (MulticaIssue, error) {
	if f.getIssueErr != nil {
		return MulticaIssue{}, f.getIssueErr
	}
	return f.issue, nil
}

func goldenDeps(t *testing.T, client *fakeMulticaClient, blobs map[string][]byte) DispatchDeps {
	t.Helper()
	dir := t.TempDir()
	return DispatchDeps{
		Client: client,
		Ledger: NewFileSurfaceWriteLedger(filepath.Join(dir, "surface-write-ledger.jsonl")),
		FetchBlob: func(digest string) ([]byte, error) {
			data, ok := blobs[digest]
			if !ok {
				return nil, fmt.Errorf("blob %s not on node", digest)
			}
			return data, nil
		},
		ArtifactDir: filepath.Join(dir, "artifacts"),
		Surface:     "multica",
	}
}

func goldenDelivery() presentation.Delivery {
	digest := "sha256:" + strings.Repeat("ab", 32)
	return presentation.Delivery{
		DeliveryID:  "dlv_golden",
		EventRef:    "dec_golden_1",
		ResourceRef: "progress_digest/payments",
		Surface:     "multica",
		Role:        presentation.DeliveryRoleDisplay,
		Action:      presentation.DeliveryActionWriteComment,
		Title:       "支付回调对账延迟排查完成",
		// 语料故意携带禁止集残渣(上游殘留), 可见文本必须清洗
		BodyMarkdown: "根因已定位,修复值见附件。mnemon:event_ref=dec_golden_1 seq=42 principal 残渣;发送机路径 local/agent-a-e1/1 与 " + digest + " 不得可见。",
		Metadata: map[string]string{
			"mnemon.event_ref":        "dec_golden_1",
			"mnemon.resource_ref":     "progress_digest/payments",
			"mnemon.surface_role":     "display",
			"mnemon.no_auto_dispatch": "true",
		},
		Artifacts: []presentation.DeliveryArtifact{{Digest: digest, Name: "排查文档.md"}},
	}
}

func TestGolden1And2And5VisibleTextMetadataAndLocalArtifacts(t *testing.T) {
	client := &fakeMulticaClient{}
	digest := "sha256:" + strings.Repeat("ab", 32)
	deps := goldenDeps(t, client, map[string][]byte{digest: []byte("根因: reconcile.window_hold_ms 应改回 30000。")})

	result, err := DispatchDelivery(context.Background(), deps, goldenDelivery(), "issue-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(client.comments) != 1 {
		t.Fatalf("expected one comment, got %d", len(client.comments))
	}
	visible := client.comments[0]

	// golden 1: 禁止集零命中
	for _, pattern := range visibleTextForbidden {
		if pattern.MatchString(visible) {
			t.Fatalf("golden 1: forbidden pattern %s hit visible text:\n%s", pattern, visible)
		}
	}
	if !strings.Contains(visible, "Mnemon 更新: 支付回调对账延迟排查完成") {
		t.Fatalf("golden 1: visible text must stay natural language:\n%s", visible)
	}
	if !result.Redacted {
		t.Fatal("golden 1: redaction fact must surface in the result")
	}

	// golden 2: metadata 键齐全
	meta := client.metadata[0]
	for _, key := range []string{
		MulticaMetadataEventRef, MulticaMetadataResourceRef, MulticaMetadataSurfaceRef,
		MulticaMetadataSourceArtifactRef, MulticaMetadataSurfaceRole,
	} {
		if strings.TrimSpace(meta[key]) == "" {
			t.Fatalf("golden 2: metadata key %s missing: %v", key, meta)
		}
	}
	if meta["mnemon.redacted"] != "true" {
		t.Fatal("golden 2: redaction must be recorded in metadata")
	}

	// golden 5: 正文只文件名; 完整路径/digest 进 metadata+result
	if !strings.Contains(visible, "排查文档.md") {
		t.Fatalf("golden 5: visible text must name the file:\n%s", visible)
	}
	if strings.Contains(visible, deps.ArtifactDir) || strings.Contains(visible, strings.TrimPrefix(digest, "sha256:")) {
		t.Fatalf("golden 5: visible text leaked a path or digest:\n%s", visible)
	}
	if len(result.Artifacts) != 1 || !strings.HasPrefix(result.Artifacts[0], deps.ArtifactDir) {
		t.Fatalf("golden 5: artifact must materialize locally, got %v", result.Artifacts)
	}
	if meta[MulticaMetadataSourceArtifactRef] != digest {
		t.Fatalf("golden 5: digest must ride metadata, got %v", meta)
	}
}

func TestGolden4ReplaySameEventRefWritesNothing(t *testing.T) {
	client := &fakeMulticaClient{}
	digest := "sha256:" + strings.Repeat("ab", 32)
	deps := goldenDeps(t, client, map[string][]byte{digest: []byte("内容")})
	if _, err := DispatchDelivery(context.Background(), deps, goldenDelivery(), "issue-1"); err != nil {
		t.Fatal(err)
	}
	replay, err := DispatchDelivery(context.Background(), deps, goldenDelivery(), "issue-1")
	if err != nil {
		t.Fatal(err)
	}
	if !replay.SkippedDuplicate {
		t.Fatal("golden 4: replay must be skipped_duplicate")
	}
	if len(client.comments) != 1 {
		t.Fatalf("golden 4: replay wrote a new comment (%d total)", len(client.comments))
	}
}

func TestGolden3ProviderAssignedIssueNeverGoesActive(t *testing.T) {
	client := &fakeMulticaClient{issue: MulticaIssue{ID: "issue-1", Status: "active", AssignedAgentID: "provider-agent-7"}}
	deps := goldenDeps(t, client, nil)
	status, skipped, err := DispatchStatus(context.Background(), deps, "issue-1", "active")
	if err != nil {
		t.Fatal(err)
	}
	if status != "" || skipped == "" || len(client.statusSets) != 0 {
		t.Fatalf("golden 3: provider-assigned issue must not be pushed active: status=%q skipped=%q sets=%v", status, skipped, client.statusSets)
	}
	// §5 fail-closed: GetIssue 失败 → 不写 status, 记原因
	client.getIssueErr = fmt.Errorf("multica unreachable")
	status, skipped, err = DispatchStatus(context.Background(), deps, "issue-1", "done")
	if err != nil {
		t.Fatal(err)
	}
	if status != "" || !strings.Contains(skipped, "查询失败") || len(client.statusSets) != 0 {
		t.Fatalf("§5 fail-closed violated: status=%q skipped=%q", status, skipped)
	}
}

// golden 6: dispatcher 是纯出境面 — 它对 mnemond 零提交(comment 成为
// observed 的唯一通道是 explicit import-issue)。编译层证据: DispatchDeps
// 没有任何提交客户端;本测试锁定 DispatchClient 接口面。
func TestGolden6DispatcherHasNoSubmissionSurface(t *testing.T) {
	var _ DispatchClient = (*fakeMulticaClient)(nil)
	// The dispatcher's entire world is DispatchDeps: a display client, the
	// ledger, the blob lane, and a directory. No observe/ingest/emit handle
	// exists to smuggle a comment into governed state.
	deps := DispatchDeps{}
	if deps.Client != nil || deps.Ledger != nil || deps.FetchBlob != nil {
		t.Fatal("zero-value deps must be inert")
	}
}
