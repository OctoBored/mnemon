package app

// capsule_sync.go — the node side of the capsule-native federation edge
// (r4-hub-protocol-v1 §6). The assembler projects one pending synced-event
// material into one signed capsule (finest rejection granularity: one
// decision, one atom); blobs travel first; the push pass mirrors hub
// verdicts into the same local ledger the legacy wire used, so offline
// bookkeeping is unchanged. The replica signing key is minted once under
// .mnemon/harness/sync/credentials/replica.ed25519.

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mnemon-dev/mnemon/harness/internal/blob"
	"github.com/mnemon-dev/mnemon/harness/internal/capsule"
	"github.com/mnemon-dev/mnemon/harness/internal/contract"
	eventmodel "github.com/mnemon-dev/mnemon/harness/internal/event"
)

var capsuleKindVerbs = map[string]string{
	"teamwork_signal": "signal",
	"assignment":      "assign",
	"progress_digest": "report",
	"agent_profile":   "report", // profile = verb report + schema teamwork/profile
}

func capsuleVerbForKind(kind string) string {
	if verb, ok := capsuleKindVerbs[kind]; ok {
		return verb
	}
	return "report"
}

func capsuleSchemaIDForKind(kind string) string {
	switch kind {
	case "teamwork_signal":
		return "teamwork/signal"
	case "assignment":
		return "teamwork/assignment"
	case "progress_digest":
		return "teamwork/report"
	case "agent_profile":
		return "teamwork/profile"
	default:
		return kind
	}
}

// ReplicaSigningKey loads (minting on first use) the node's federation
// signing key. Same file format as the principal keys setup writes.
func ReplicaSigningKey(projectRoot string) (ed25519.PrivateKey, ed25519.PublicKey, error) {
	path := filepath.Join(projectRoot, ".mnemon", "harness", "sync", "credentials", "replica.ed25519")
	if _, err := writePrincipalKey(path); err != nil {
		return nil, nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if seedHex, ok := strings.CutPrefix(line, "seed="); ok {
			seed, decodeErr := hex.DecodeString(strings.TrimSpace(seedHex))
			if decodeErr != nil || len(seed) != ed25519.SeedSize {
				return nil, nil, fmt.Errorf("replica key %s: malformed seed", path)
			}
			priv := ed25519.NewKeyFromSeed(seed)
			return priv, priv.Public().(ed25519.PublicKey), nil
		}
	}
	return nil, nil, fmt.Errorf("replica key %s: no seed line", path)
}

// OutboundCapsule is one assembled, signed federation atom plus the blob
// bytes that must land on the hub BEFORE it (protocol push order).
type OutboundCapsule struct {
	Envelope  []byte // raw DSSE envelope JSON
	CapsuleID string
	Blobs     map[string][]byte // digest -> bytes
	Source    eventmodel.EventEnvelope
}

// BuildOutboundCapsule projects one synced-event material into a capsule.
// Artifact digests resolve against the node blob store — a missing local
// blob is an error (E1: content accompanies the handoff, never a dead ref).
func BuildOutboundCapsule(env eventmodel.EventEnvelope, replicaID string, priv ed25519.PrivateKey, pub ed25519.PublicKey, blobs *blob.Store) (OutboundCapsule, error) {
	material, err := contract.SyncedEventMaterialFromEnvelope(env)
	if err != nil {
		return OutboundCapsule{}, err
	}
	item := capsuleLastItem(material.Fields)
	rule, _ := item["rule"].(map[string]any)
	refs, _ := item["refs"].(map[string]any)
	// the FULL item rides as the record's semantic payload — capsules stay
	// faithful for any kind shape (zoned coordination items and flat
	// external-package entries alike); the rule seats below are the
	// governed projection of the same item.
	narrativeJSON, err := json.Marshal(item)
	if err != nil {
		return OutboundCapsule{}, err
	}

	var links []capsule.Link
	for _, ref := range capsuleStringList(refs, "evidence_refs") {
		links = append(links, capsule.Link{Rel: "evidence", Href: ref})
	}
	if assignmentRef := capsuleString(rule, "assignment_ref"); assignmentRef != "" {
		links = append(links, capsule.Link{Rel: "assignment", Href: assignmentRef})
	}
	blobPayloads := map[string][]byte{}
	var blobRefs []capsule.BlobRef
	for _, ref := range capsuleStringList(refs, "artifact_refs") {
		if !strings.HasPrefix(ref, "sha256:") {
			continue
		}
		data, getErr := blobs.Get(ref)
		if getErr != nil {
			return OutboundCapsule{}, fmt.Errorf("artifact %s named by %s is not in the local blob store: %w", ref, material.LocalDecisionID, getErr)
		}
		links = append(links, capsule.Link{Rel: "artifact", Href: ref})
		blobPayloads[ref] = data
		blobRefs = append(blobRefs, capsule.BlobRef{Digest: ref, Size: int64(len(data)), MediaType: "application/octet-stream", Name: ""})
	}
	sort.Slice(blobRefs, func(i, j int) bool { return blobRefs[i].Digest < blobRefs[j].Digest })

	kind := string(material.ResourceRef.Kind)
	decision := capsule.Decision{ID: material.LocalDecisionID, IngestSeq: material.LocalIngestSeq, AcceptedAt: material.DecidedAt}
	doc := capsule.Document{
		Schema: capsule.SchemaV1,
		Header: capsule.Header{
			Producer:  capsule.Producer{Principal: replicaID, KeyID: capsule.KeyID(pub), PublicKey: base64.StdEncoding.EncodeToString(pub)},
			Boundary:  "space",
			CreatedAt: material.DecidedAt,
			SchemaIDs: []string{capsuleSchemaIDForKind(kind)},
		},
		Records: []capsule.NormalizedRecord{{
			ID:         fmt.Sprintf("local/%s/%d", replicaID, material.LocalIngestSeq),
			Verb:       capsuleVerbForKind(kind),
			Subject:    capsule.Subject{Kind: kind, ID: string(material.ResourceRef.ID)},
			Actor:      string(material.Actor),
			Assignee:   capsuleString(rule, "assignee"),
			Outcome:    capsuleString(rule, "outcome"),
			Scope:      capsuleString(rule, "scope"),
			TTL:        capsuleString(rule, "ttl"),
			ExternalID: material.LocalDecisionID,
			Links:      links,
			Narrative:  string(narrativeJSON),
			Decision:   decision,
		}},
		Blobs: blobRefs,
		Proof: capsule.Proof{Decisions: []capsule.Decision{decision}},
	}
	signed, capsuleID, err := capsule.Sign(doc, priv)
	if err != nil {
		return OutboundCapsule{}, err
	}
	raw, err := json.Marshal(signed)
	if err != nil {
		return OutboundCapsule{}, err
	}
	return OutboundCapsule{Envelope: raw, CapsuleID: capsuleID, Blobs: blobPayloads, Source: env}, nil
}

func capsuleLastItem(fields map[string]any) map[string]any {
	for _, listField := range []string{"items", "entries", "declarations"} {
		items, ok := fields[listField].([]any)
		if !ok || len(items) == 0 {
			continue
		}
		if item, ok := items[len(items)-1].(map[string]any); ok {
			return item
		}
	}
	return fields
}

func capsuleString(section map[string]any, key string) string {
	if section == nil {
		return ""
	}
	value, _ := section[key].(string)
	return strings.TrimSpace(value)
}

func capsuleStringList(section map[string]any, key string) []string {
	if section == nil {
		return nil
	}
	var out []string
	switch list := section[key].(type) {
	case []string:
		out = list
	case []any:
		for _, item := range list {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// UnpackInboundCapsule verifies one pulled envelope (fetching any missing
// blobs from the hub lane into the local store first) and synthesizes the
// same-shape synced-event envelopes the import pipeline re-governs — the
// "same pipeline re-run" form of capsule import.
func UnpackInboundCapsule(raw []byte, fetchBlob func(digest string) ([]byte, error), blobs *blob.Store, itemsFieldFor func(kind string) string) ([]eventmodel.EventEnvelope, error) {
	env, err := capsule.DecodeEnvelope(raw)
	if err != nil {
		return nil, err
	}
	doc, err := capsule.DecodePayload(env)
	if err != nil {
		return nil, err
	}
	for _, ref := range doc.Blobs {
		if blobs.Has(ref.Digest) {
			continue
		}
		if fetchBlob == nil {
			return nil, fmt.Errorf("capsule names blob %s but no hub blob lane is available", ref.Digest)
		}
		data, fetchErr := fetchBlob(ref.Digest)
		if fetchErr != nil {
			return nil, fmt.Errorf("fetch blob %s: %w", ref.Digest, fetchErr)
		}
		if putErr := blobs.PutExpect(ref.Digest, data); putErr != nil {
			return nil, putErr
		}
	}
	res := capsule.Verify(env, blobs.Resolver())
	if !res.OK() {
		return nil, fmt.Errorf("inbound capsule failed verification: %v", res.Issues)
	}
	var out []eventmodel.EventEnvelope
	for _, rec := range res.Document.Records {
		// the record's semantic payload IS the original item; parse it back
		// whole, falling back to a seat-reconstructed item for foreign
		// producers that sent plain text
		item := map[string]any{}
		if strings.TrimSpace(rec.Narrative) != "" {
			if err := json.Unmarshal([]byte(rec.Narrative), &item); err != nil {
				item = map[string]any{"narrative": map[string]any{"summary": rec.Narrative}}
			}
		}
		if _, ok := item["id"]; !ok {
			item["id"] = rec.ID
		}
		if _, ok := item["actor"]; !ok {
			item["actor"] = rec.Actor
		}
		listField := "items"
		if itemsFieldFor != nil {
			if name := itemsFieldFor(rec.Subject.Kind); name != "" {
				listField = name
			}
		}
		fields := map[string]any{listField: []any{item}}
		material := contract.SyncedEventMaterial{
			OriginReplicaID: res.Document.Header.Producer.Principal,
			LocalDecisionID: rec.Decision.ID,
			LocalIngestSeq:  rec.Decision.IngestSeq,
			Actor:           contract.ActorID(rec.Actor),
			ResourceRef:     contract.ResourceRef{Kind: contract.ResourceKind(rec.Subject.Kind), ID: contract.ResourceID(rec.Subject.ID)},
			ResourceVersion: 1,
			FieldsDigest:    syncFieldsDigest(fields),
			Fields:          fields,
			DecidedAt:       rec.Decision.AcceptedAt,
			Status:          "accepted",
		}
		syncedEnv, buildErr := contract.SyncedEventEnvelopeFromMaterial(material)
		if buildErr != nil {
			return nil, buildErr
		}
		out = append(out, syncedEnv)
	}
	return out, nil
}

func syncFieldsDigest(fields map[string]any) string {
	b, _ := json.Marshal(fields)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// capsuleFeedItemID extracts a stable identity for a feed item that failed
// unpacking — the capsule id when computable, else a digest of the bytes
// (the durable-once diagnostic needs SOME stable key).
func capsuleFeedItemID(raw []byte) string {
	if env, err := capsule.DecodeEnvelope(raw); err == nil {
		if payload, decodeErr := base64.StdEncoding.DecodeString(env.Payload); decodeErr == nil {
			sum := sha256.Sum256(payload)
			return "sha256:" + hex.EncodeToString(sum[:])
		}
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}
