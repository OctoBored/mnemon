package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mnemon-dev/mnemon/harness/internal/blob"
	"github.com/mnemon-dev/mnemon/harness/internal/contract"
	eventmodel "github.com/mnemon-dev/mnemon/harness/internal/event"
	"github.com/mnemon-dev/mnemon/harness/internal/mnemond/access"
	"github.com/mnemon-dev/mnemon/harness/internal/mnemond/policy"
	"github.com/mnemon-dev/mnemon/harness/internal/mnemonhub/exchange"
	"github.com/mnemon-dev/mnemon/harness/internal/runtime"
)

// The driver sync worker (v1.1 #2): inside the SERVING process, sync operates the already-open
// runtime/store handle — push reads pending synced events and applies the hub's verdicts through the
// live handle; pull re-enters Event Intake via the runtime's trusted intake + Tick. It never opens
// the store by path (the single-writer flock would self-collide); the path-based mnemonhub exchange
// helpers remain the OFFLINE CLI verbs' tools, and ProbeAvailable keeps the two mutually exclusive.

// SyncWorkerOptions configures the worker. The zero value is safe: default cadence and transport
// timeout, fail-closed transport security.
type SyncWorkerOptions struct {
	ProjectRoot         string
	Interval            time.Duration // <= 0 defaults to defaultSyncWorkerInterval
	Timeout             time.Duration // per-call transport bound; <= 0 defaults to access.DefaultSyncTimeout
	AllowInsecureRemote bool          // explicit T2 downgrade override (v1.1 #3)
	// Catalog is the boot-resolved event package registry the pull import derives its kind→observation
	// mapping from (descriptor-derived, PD6). nil falls back to the embedded catalog.
	Catalog policy.Registry
	// Direction limits a MANUAL pass: "push" | "pull" | "" (both). The
	// public push/pull verbs run one filtered pass through the same code.
	Direction string
}

const defaultSyncWorkerInterval = 30 * time.Second

// RunSyncWorker loops one sync pass on its own cadence until ctx cancels. Every pass error is
// logged to errw and SWALLOWED — an unreachable remote degrades sync, never the serve path (I13:
// the local loop stays fully functional offline; the bounded client keeps each pass finite).
func RunSyncWorker(ctx context.Context, rt *runtime.Runtime, opts SyncWorkerOptions, errw io.Writer) {
	interval := opts.Interval
	if interval <= 0 {
		interval = defaultSyncWorkerInterval
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := syncWorkerPass(rt, opts); err != nil {
				if delay := syncWorkerErrorBackoff(err, time.Now()); delay > 0 {
					fmt.Fprintf(errw, "mnemon-harness: sync worker: %v; backing off %s\n", err, delay.Round(time.Second))
					timer := time.NewTimer(delay)
					select {
					case <-ctx.Done():
						timer.Stop()
						return
					case <-timer.C:
					}
					continue
				}
				fmt.Fprintf(errw, "mnemon-harness: sync worker: %v\n", err)
			}
		}
	}
}

func syncWorkerErrorBackoff(err error, now time.Time) time.Duration {
	if err == nil {
		return 0
	}
	msg := err.Error()
	if retryAfter := syncWorkerErrorToken(msg, "retry_after"); retryAfter != "" {
		seconds, parseErr := strconv.Atoi(retryAfter)
		if parseErr == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	if syncWorkerErrorToken(msg, "rate_limit_remaining") != "0" {
		return 0
	}
	reset := syncWorkerErrorToken(msg, "rate_limit_reset")
	if reset == "" {
		return 0
	}
	seconds, parseErr := strconv.ParseInt(reset, 10, 64)
	if parseErr != nil || seconds <= 0 {
		return 0
	}
	resetAt := time.Unix(seconds, 0)
	if !resetAt.After(now) {
		return 0
	}
	return resetAt.Sub(now) + time.Second
}

func syncWorkerErrorToken(msg, key string) string {
	prefix := key + "="
	start := strings.Index(msg, prefix)
	if start < 0 {
		return ""
	}
	value := msg[start+len(prefix):]
	if end := strings.IndexAny(value, ",) "); end >= 0 {
		value = value[:end]
	}
	return strings.TrimSpace(value)
}

// syncWorkerPass runs ONE sync pass against the configured Remote Workspace plan. Gate: when
// remotes.json does not exist, the pass is a no-op — zero sync activity without a connected remote
// (I13), checked per pass so `sync connect` takes effect without a restart.
func syncWorkerPass(rt *runtime.Runtime, opts SyncWorkerOptions) error {
	remotesPath := filepath.Join(opts.ProjectRoot, ".mnemon", "harness", "sync", "remotes.json")
	if _, err := os.Stat(remotesPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat Remote Workspace config: %w", err)
	}
	plan, err := exchange.LoadRemotePlan(remotesPath, "default")
	if err != nil {
		return err
	}
	if opts.Direction != "pull" {
		for _, entry := range plan.PushTargets {
			remote, err := syncWorkerRemote(entry, opts)
			if err != nil {
				return err
			}
			if err := syncWorkerPush(rt, remote, entry.ID, opts.ProjectRoot); err != nil {
				return err
			}
		}
	}
	if opts.Direction != "push" {
		for _, entry := range plan.PullSources {
			remote, err := syncWorkerRemote(entry, opts)
			if err != nil {
				return err
			}
			if err := syncWorkerPull(rt, remote, entry.ID, opts.Catalog, opts.ProjectRoot); err != nil {
				return err
			}
		}
	}
	return nil
}

// SyncOnce runs one manual federation pass (the public push/pull verbs).
// It opens the local runtime offline — the daemon must be stopped (the
// store's single-writer lock enforces it, same as watch).
func SyncOnce(projectRoot, direction string, errw io.Writer) error {
	boot, err := ResolveLocalBoot(projectRoot, "", "")
	if err != nil {
		return err
	}
	catalog := SyncImportCatalog(projectRoot, errw)
	rt, err := OpenLocalRuntime(boot.StorePath, boot.Loaded, boot.Config.Loops, catalog)
	if err != nil {
		return fmt.Errorf("open Local Mnemon (manual sync needs exclusive store access — is the daemon running?): %w", err)
	}
	defer rt.Close()
	return syncWorkerPass(rt, SyncWorkerOptions{ProjectRoot: projectRoot, Direction: direction, Catalog: catalog})
}

// syncWorkerRemote builds the selected Remote Workspace backend from the remote entry. The
// first-party backend is the HTTP MnemonHub wire; product surfaces must not bypass local import.
func syncWorkerRemote(entry exchange.RemoteEntry, opts SyncWorkerOptions) (capsuleWorkspace, error) {
	switch entry.NormalizedBackend() {
	case exchange.RemoteBackendHTTP:
		return syncWorkerHTTPRemote(entry, opts)
	default:
		return nil, fmt.Errorf("Remote Workspace %q: unsupported backend %q", entry.ID, entry.NormalizedBackend())
	}
}

// syncWorkerHTTPRemote builds the bounded HTTP mnemon-hub sync client from the remote entry:
// credential_ref + ca_file resolve relative to the project root (the same resolution `sync connect`
// wrote them under), and the endpoint passes the T2 downgrade gate unless explicitly overridden.
func syncWorkerHTTPRemote(entry exchange.RemoteEntry, opts SyncWorkerOptions) (capsuleWorkspace, error) {
	if strings.TrimSpace(entry.CredentialRef) == "" {
		return nil, fmt.Errorf("Remote Workspace %q has no credential_ref", entry.ID)
	}
	tokPath := entry.CredentialRef
	if !filepath.IsAbs(tokPath) {
		tokPath = filepath.Join(opts.ProjectRoot, tokPath)
	}
	raw, err := os.ReadFile(tokPath)
	if err != nil {
		return nil, fmt.Errorf("read Remote Workspace token file: %w", err)
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		return nil, fmt.Errorf("Remote Workspace token file %s is empty", entry.CredentialRef)
	}
	caFile := entry.CAFile
	if caFile != "" && !filepath.IsAbs(caFile) {
		caFile = filepath.Join(opts.ProjectRoot, caFile)
	}
	return access.NewSyncClient(entry.Endpoint, access.SyncClientConfig{
		Token:         token,
		Timeout:       opts.Timeout,
		CAFile:        caFile,
		AllowInsecure: opts.AllowInsecureRemote,
	})
}

// capsuleWorkspace is the capsule-native remote face (r4-hub-protocol-v1);
// the first-party HTTP client satisfies it. Asserted at the push/pull site
// so the exchange backend seam stays untouched.
type capsuleWorkspace interface {
	CapsulePush(raw []byte) (access.CapsuleAccepted, bool, *access.HubProblem, error)
	CapsulePull(cursor int64, limit int, etag string) (access.CapsuleFeedPage, error)
	BlobPut(digest string, data []byte) error
	BlobGet(digest string) ([]byte, error)
}

// syncWorkerPush assembles one capsule per pending decision, pushes blobs
// first, then the capsule, and mirrors verdicts into the same local ledger
// the legacy wire fed (accepted → synced; problem → rejected with the
// problem type+detail as diagnostic; transport errors abort the pass and
// leave everything pending for the next tick).
func syncWorkerPush(rt *runtime.Runtime, hub capsuleWorkspace, remoteID, projectRoot string) error {
	batch, err := exchange.ReadPushBatch(rt)
	if err != nil {
		return err
	}
	if len(batch.Events) == 0 {
		return nil
	}
	priv, pub, err := ReplicaSigningKey(projectRoot)
	if err != nil {
		return err
	}
	blobs, err := blob.Open(filepath.Join(projectRoot, blob.DefaultDir))
	if err != nil {
		return err
	}
	var resp contract.SyncPushResponse
	for _, env := range batch.Events {
		out, buildErr := BuildOutboundCapsule(env, batch.ReplicaID, priv, pub, blobs)
		if buildErr != nil {
			resp.Rejected = append(resp.Rejected, contract.EventExchangeResultFromEnvelope(env, "rejected", buildErr.Error()))
			continue
		}
		blobFailed := false
		for digest, data := range out.Blobs {
			if putErr := hub.BlobPut(digest, data); putErr != nil {
				// transport-grade: leave this decision pending for the next pass
				blobFailed = true
				break
			}
		}
		if blobFailed {
			continue
		}
		accepted, _, problem, pushErr := hub.CapsulePush(out.Envelope)
		if pushErr != nil {
			return fmt.Errorf("capsule push failed: %w", pushErr)
		}
		if problem != nil {
			resp.Rejected = append(resp.Rejected, contract.EventExchangeResultFromEnvelope(env, "rejected", problem.Type+": "+problem.Detail))
			continue
		}
		resp.Accepted = append(resp.Accepted, contract.EventExchangeResultFromEnvelope(env, "accepted", ""))
		resp.NextCursor = fmt.Sprintf("%d", accepted.HubSeq)
	}
	return exchange.ApplyPushResponse(rt, remoteID, resp)
}

// syncWorkerPull pulls capsules after the durable cursor, verifies each
// atom (fetching its blob closure into the local store), unpacks records
// into same-shape synced-event envelopes, and re-enters them through the
// live runtime's trusted intake (importPulledEvents — the same pipeline the
// legacy wire fed), then advances the cursor.
func syncWorkerPull(rt *runtime.Runtime, hub capsuleWorkspace, remoteID string, catalog policy.Registry, projectRoot string) error {
	pullState, err := exchange.ReadPullState(rt, remoteID)
	if err != nil {
		return err
	}
	cursor, _ := strconv.ParseInt(strings.TrimSpace(pullState.RemoteCursor), 10, 64)
	page, err := hub.CapsulePull(cursor, 0, "")
	if err != nil {
		return fmt.Errorf("capsule pull failed: %w", err)
	}
	if page.NotModified || len(page.Items) == 0 {
		return nil
	}
	blobs, err := blob.Open(filepath.Join(projectRoot, blob.DefaultDir))
	if err != nil {
		return err
	}
	resolved := resolveSyncCatalog(catalog)
	itemsFieldFor := func(kind string) string {
		if pkg, ok := resolved[kind]; ok && pkg.ItemsField != "" {
			return pkg.ItemsField
		}
		return "items"
	}
	var events []eventmodel.EventEnvelope
	var diagnostics []contract.EventExchangeResult
	for _, raw := range page.Items {
		unpacked, unpackErr := UnpackInboundCapsule(raw, hub.BlobGet, blobs, itemsFieldFor)
		if unpackErr != nil {
			// a bad atom never blocks the feed: the cursor keeps moving and
			// the failure lands as a DURABLE local diagnostic (once).
			diagnostics = append(diagnostics, contract.EventExchangeResult{
				OriginMnemond: remoteID,
				EventID:       capsuleFeedItemID(raw),
				Subject:       "capsule/inbound",
				Status:        "invalid",
				Diagnostic:    unpackErr.Error(),
			})
			continue
		}
		events = append(events, unpacked...)
	}
	if err := importPulledEvents(rt, remoteID, events, catalog); err != nil {
		return err
	}
	if err := importRemoteDiagnostics(rt, remoteID, diagnostics); err != nil {
		return err
	}
	return exchange.SetPullCursor(rt, remoteID, strconv.FormatInt(page.NextCursor, 10))
}
