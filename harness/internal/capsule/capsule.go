// Package capsule implements the R4 persistent exchange unit: decided records
// plus a content-addressed blob closure plus a signed provenance chain.
// Spec: .mnemon-dev/architecture/r4/r4-capsule-format-v1.md — the capsule_id is
// computed over canonical bytes and never embedded in the document (git/OCI
// precedent), and DSSE signs the whole document so the blob manifest is covered
// transitively.
package capsule

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/gowebpki/jcs"
)

const (
	SchemaV1    = "mnemon/capsule@v1"
	PayloadType = "application/vnd.mnemon.capsule+json"
)

// Verify error codes — closed set (spec §6).
const (
	ErrSigInvalid        = "SIG_INVALID"
	ErrCanonMismatch     = "CANON_MISMATCH"
	ErrChainBroken       = "CHAIN_BROKEN"
	ErrBlobMissing       = "BLOB_MISSING"
	ErrBlobDigestBad     = "BLOB_DIGEST_MISMATCH"
	ErrSchemaUnknown     = "SCHEMA_UNKNOWN"
	ErrEnvelopeMalformed = "ENVELOPE_MALFORMED"
)

type Subject struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type Link struct {
	Rel  string `json:"rel"`
	Href string `json:"href"`
}

// linkRels is the closed rel vocabulary (spec §2; registries spec links note).
var linkRels = map[string]bool{
	"evidence": true, "artifact": true, "caused-by": true,
	"assignment": true, "external": true,
}

type Decision struct {
	ID         string `json:"id"`
	IngestSeq  int64  `json:"ingest_seq"`
	AcceptedAt string `json:"accepted_at"`
}

// NormalizedRecord is the persistent projection of a decided record (spec §2).
// Field set is closed: unknown fields fail decoding.
type NormalizedRecord struct {
	ID         string   `json:"id"`
	Verb       string   `json:"verb"`
	Subject    Subject  `json:"subject"`
	Actor      string   `json:"actor"`
	Audience   string   `json:"audience,omitempty"`
	Assignee   string   `json:"assignee,omitempty"`
	Outcome    string   `json:"outcome,omitempty"`
	Scope      string   `json:"scope,omitempty"`
	TTL        string   `json:"ttl,omitempty"`
	ExternalID string   `json:"external_id"`
	Links      []Link   `json:"links,omitempty"`
	Narrative  string   `json:"narrative"`
	Decision   Decision `json:"decision"`
}

var verbs = map[string]bool{"signal": true, "assign": true, "report": true}
var outcomes = map[string]bool{"": true, "progress": true, "result": true, "blocker": true}

type Producer struct {
	Principal string `json:"principal"`
	KeyID     string `json:"key_id"`
	PublicKey string `json:"public_key"`
}

type Header struct {
	Parent    string   `json:"parent"` // capsule_id of parent, "" encodes null (genesis)
	Producer  Producer `json:"producer"`
	Boundary  string   `json:"boundary"` // "time" | "space"
	CreatedAt string   `json:"created_at"`
	SchemaIDs []string `json:"schema_ids"`
}

type BlobRef struct {
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
	MediaType string `json:"media_type"`
	Name      string `json:"name"`
}

type Proof struct {
	Decisions []Decision `json:"decisions"`
}

type Document struct {
	Schema  string             `json:"schema"`
	Header  Header             `json:"header"`
	Records []NormalizedRecord `json:"records"`
	Blobs   []BlobRef          `json:"blobs"`
	Proof   Proof              `json:"proof"`
}

// Envelope is a DSSE envelope carrying the canonical capsule bytes.
type Envelope struct {
	PayloadType string      `json:"payloadType"`
	Payload     string      `json:"payload"` // base64(canonical bytes)
	Signatures  []Signature `json:"signatures"`
}

type Signature struct {
	KeyID string `json:"keyid"`
	Sig   string `json:"sig"`
}

// pae implements the DSSE Pre-Authentication Encoding.
func pae(payloadType string, payload []byte) []byte {
	return []byte(fmt.Sprintf("DSSEv1 %d %s %d %s",
		len(payloadType), payloadType, len(payload), payload))
}

// KeyID derives the spec key id: first 16 bytes of sha256(pubkey), hex.
func KeyID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:16])
}

// CanonicalBytes serializes the document via JCS (RFC 8785).
func CanonicalBytes(doc Document) ([]byte, error) {
	if err := validateDocument(doc); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	return jcs.Transform(raw)
}

// ID computes the external capsule id over canonical bytes (never embedded).
func ID(canonical []byte) string {
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Sign produces the DSSE envelope over the document's canonical bytes.
func Sign(doc Document, priv ed25519.PrivateKey) (Envelope, string, error) {
	canonical, err := CanonicalBytes(doc)
	if err != nil {
		return Envelope{}, "", err
	}
	sig := ed25519.Sign(priv, pae(PayloadType, canonical))
	env := Envelope{
		PayloadType: PayloadType,
		Payload:     base64.StdEncoding.EncodeToString(canonical),
		Signatures: []Signature{{
			KeyID: KeyID(priv.Public().(ed25519.PublicKey)),
			Sig:   base64.StdEncoding.EncodeToString(sig),
		}},
	}
	return env, ID(canonical), nil
}

type VerifyIssue struct {
	Code  string
	Where string
}

func (v VerifyIssue) String() string { return v.Code + ": " + v.Where }

type VerifyResult struct {
	CapsuleID string
	Document  Document
	Issues    []VerifyIssue
}

func (r VerifyResult) OK() bool { return len(r.Issues) == 0 }

// BlobResolver returns blob bytes by digest (local store lookup).
type BlobResolver func(digest string) ([]byte, bool)

// Verify checks the envelope per spec §3/§6: ① DSSE signature over the payload
// as-is, ② capsule_id derived (no field comparison), ③ optional blob closure.
func Verify(env Envelope, resolve BlobResolver) VerifyResult {
	var res VerifyResult
	if env.PayloadType != PayloadType {
		res.Issues = append(res.Issues, VerifyIssue{ErrEnvelopeMalformed, "payloadType " + env.PayloadType})
		return res
	}
	payload, err := base64.StdEncoding.DecodeString(env.Payload)
	if err != nil {
		res.Issues = append(res.Issues, VerifyIssue{ErrEnvelopeMalformed, "payload base64"})
		return res
	}
	var doc Document
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		res.Issues = append(res.Issues, VerifyIssue{ErrEnvelopeMalformed, err.Error()})
		return res
	}
	if err := validateDocument(doc); err != nil {
		res.Issues = append(res.Issues, VerifyIssue{ErrSchemaUnknown, err.Error()})
		return res
	}
	res.Document = doc
	res.CapsuleID = ID(payload)

	// ① signature over the exact payload bytes.
	pubRaw, err := base64.StdEncoding.DecodeString(doc.Header.Producer.PublicKey)
	if err != nil || len(pubRaw) != ed25519.PublicKeySize {
		res.Issues = append(res.Issues, VerifyIssue{ErrEnvelopeMalformed, "producer public_key"})
		return res
	}
	pub := ed25519.PublicKey(pubRaw)
	sigOK := false
	for _, s := range env.Signatures {
		if s.KeyID != doc.Header.Producer.KeyID {
			continue
		}
		sig, err := base64.StdEncoding.DecodeString(s.Sig)
		if err != nil {
			continue
		}
		if ed25519.Verify(pub, pae(PayloadType, payload), sig) {
			sigOK = true
			break
		}
	}
	if !sigOK {
		res.Issues = append(res.Issues, VerifyIssue{ErrSigInvalid, "no valid signature for key " + doc.Header.Producer.KeyID})
	}
	if KeyID(pub) != doc.Header.Producer.KeyID {
		res.Issues = append(res.Issues, VerifyIssue{ErrCanonMismatch, "key_id does not match public_key"})
	}
	// ② JCS restability: re-transforming the payload must be byte-identical.
	if re, err := jcs.Transform(payload); err != nil || !bytes.Equal(re, payload) {
		res.Issues = append(res.Issues, VerifyIssue{ErrCanonMismatch, "payload is not canonical JCS"})
	}
	// ③ blob closure (when a resolver is supplied).
	if resolve != nil {
		for _, b := range doc.Blobs {
			data, ok := resolve(b.Digest)
			if !ok {
				res.Issues = append(res.Issues, VerifyIssue{ErrBlobMissing, b.Digest})
				continue
			}
			sum := sha256.Sum256(data)
			if "sha256:"+hex.EncodeToString(sum[:]) != b.Digest {
				res.Issues = append(res.Issues, VerifyIssue{ErrBlobDigestBad, b.Digest})
			}
		}
	}
	return res
}

// VerifyChain checks parent continuity over a producer stream ordered oldest
// first (single-chain mode; spec §4).
func VerifyChain(ordered []Envelope, resolve BlobResolver) []VerifyResult {
	results := make([]VerifyResult, 0, len(ordered))
	prev := ""
	for i, env := range ordered {
		r := Verify(env, resolve)
		if r.OK() {
			if r.Document.Header.Parent != prev {
				r.Issues = append(r.Issues, VerifyIssue{ErrChainBroken,
					fmt.Sprintf("capsule %d parent %q != %q", i, r.Document.Header.Parent, prev)})
			}
			prev = r.CapsuleID
		}
		results = append(results, r)
	}
	return results
}

func validateDocument(doc Document) error {
	if doc.Schema != SchemaV1 {
		return fmt.Errorf("schema %q", doc.Schema)
	}
	if doc.Header.Boundary != "time" && doc.Header.Boundary != "space" {
		return fmt.Errorf("boundary %q", doc.Header.Boundary)
	}
	for _, r := range doc.Records {
		if !verbs[r.Verb] {
			return fmt.Errorf("record %s: verb %q", r.ID, r.Verb)
		}
		if !outcomes[r.Outcome] {
			return fmt.Errorf("record %s: outcome %q", r.ID, r.Outcome)
		}
		for _, l := range r.Links {
			if !linkRels[l.Rel] {
				return fmt.Errorf("record %s: link rel %q", r.ID, l.Rel)
			}
		}
	}
	return nil
}

// DecodeEnvelope parses a raw DSSE envelope JSON document.
func DecodeEnvelope(raw []byte) (Envelope, error) {
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return Envelope{}, fmt.Errorf("capsule: not a DSSE envelope: %w", err)
	}
	return env, nil
}

// DecodePayload decodes the envelope's capsule document WITHOUT verifying —
// for read paths that already trust the store or verify separately.
func DecodePayload(env Envelope) (Document, error) {
	raw, err := base64.StdEncoding.DecodeString(env.Payload)
	if err != nil {
		return Document{}, fmt.Errorf("capsule: payload base64: %w", err)
	}
	var doc Document
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Document{}, fmt.Errorf("capsule: payload document: %w", err)
	}
	return doc, nil
}
