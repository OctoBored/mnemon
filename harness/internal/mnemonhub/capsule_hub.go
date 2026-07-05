package mnemonhub

// capsule_hub.go — the capsule-native hub core (r4-hub-protocol-v1 §2-§5):
// push verifies the DSSE envelope offline (signature, canonical id, blob
// closure against the HUB blob store), clamps the WHOLE capsule to the
// pusher's grant (a capsule is a signed atom — records are never filtered
// out of it), and stores it content-addressed; rejection is NON-terminal
// (same id re-pushed is re-adjudicated; only an accepted id replays).

import (
	"fmt"
	"strings"

	"github.com/mnemon-dev/mnemon/harness/internal/blob"
	"github.com/mnemon-dev/mnemon/harness/internal/capsule"
	"github.com/mnemon-dev/mnemon/harness/internal/contract"
)

// Problem is the problem+json shape (§5 closed type vocabulary).
type Problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail"`
}

const (
	ProblemScopeOutOfGrant    = "about:mnemon/hub/scope-out-of-grant"
	ProblemBlobMissing        = "about:mnemon/hub/blob-missing"
	ProblemSignatureInvalid   = "about:mnemon/hub/signature-invalid"
	ProblemCanonicalMismatch  = "about:mnemon/hub/canonical-mismatch"
	ProblemEnvelopeMalformed  = "about:mnemon/hub/envelope-malformed"
	ProblemBlobDigestMismatch = "about:mnemon/hub/blob-digest-mismatch"
)

type PushCapsuleResult struct {
	CapsuleID string `json:"capsule_id"`
	HubSeq    int64  `json:"hub_seq"`
	Replayed  bool   `json:"-"`
}

type CapsuleFeed struct {
	Items      []string `json:"items"` // raw DSSE envelope JSON documents
	NextCursor int64    `json:"next_cursor"`
}

type RejectedFeed struct {
	Items []RejectedEntry `json:"items"`

	NextCursor int64 `json:"next_cursor"`
}

type RejectedEntry struct {
	CapsuleID  string `json:"capsule_id"`
	Code       string `json:"code"`
	Detail     string `json:"detail"`
	ReceivedAt string `json:"received_at"`
}

// PushCapsule adjudicates one pushed capsule. A non-nil Problem is the 422
// path; the rejection is recorded (queryable, non-terminal) and the SAME
// capsule_id may be re-pushed after the cause is fixed.
func (s *Server) PushCapsule(principal contract.ActorID, rawEnvelope []byte, env capsule.Envelope, blobs *blob.Store) (PushCapsuleResult, *Problem) {
	grant, ok := s.grants.Grant(principal, contract.SyncVerbPush)
	if !ok {
		return PushCapsuleResult{}, &Problem{Type: ProblemScopeOutOfGrant, Title: "no push grant", Status: 403,
			Detail: fmt.Sprintf("principal %q has no replica grant for %s", principal, contract.SyncVerbPush)}
	}
	var resolver capsule.BlobResolver
	if blobs != nil {
		resolver = blobs.Resolver()
	}
	res := capsule.Verify(env, resolver)
	if !res.OK() {
		problem := verifyProblem(res)
		s.recordRejection(principal, res.CapsuleID, problem)
		return PushCapsuleResult{}, problem
	}
	// whole-capsule grant clamp: every record's subject must be inside the
	// pusher's scope; a partial overreach rejects the atom (never filtered).
	for _, rec := range res.Document.Records {
		ref := contract.ResourceRef{Kind: contract.ResourceKind(rec.Subject.Kind), ID: contract.ResourceID(rec.Subject.ID)}
		if _, err := contract.ClampRefs(principal, grant.Scopes, []contract.ResourceRef{ref}); err != nil {
			problem := &Problem{Type: ProblemScopeOutOfGrant, Title: "capsule outside grant scope", Status: 422,
				Detail: fmt.Sprintf("record %s subject %s/%s 越出授权范围: %v", rec.ID, rec.Subject.Kind, rec.Subject.ID, err)}
			s.recordRejection(principal, res.CapsuleID, problem)
			return PushCapsuleResult{}, problem
		}
	}
	seq, replayed, err := s.store.AppendHubCapsule(res.CapsuleID, string(principal), string(rawEnvelope), s.now())
	if err != nil {
		return PushCapsuleResult{}, &Problem{Type: ProblemEnvelopeMalformed, Title: "store append failed", Status: 500, Detail: err.Error()}
	}
	return PushCapsuleResult{CapsuleID: res.CapsuleID, HubSeq: seq, Replayed: replayed}, nil
}

// PullCapsules serves accepted capsules after cursor with the whole-capsule
// visibility clamp: a capsule is visible iff EVERY record subject is inside
// the puller's grant; partial overreach hides the atom and audits a clamped
// line (the capsule assembler's cue to package per audience/scope).
func (s *Server) PullCapsules(principal contract.ActorID, cursor int64, limit int, clampAudit func(capsuleID string)) (CapsuleFeed, *Problem) {
	grant, ok := s.grants.Grant(principal, contract.SyncVerbPull)
	if !ok {
		return CapsuleFeed{}, &Problem{Type: ProblemScopeOutOfGrant, Title: "no pull grant", Status: 403,
			Detail: fmt.Sprintf("principal %q has no replica grant for %s", principal, contract.SyncVerbPull)}
	}
	records, next, err := s.store.HubCapsulesAfter(cursor, string(principal), limit)
	if err != nil {
		return CapsuleFeed{}, &Problem{Type: ProblemEnvelopeMalformed, Title: "store read failed", Status: 500, Detail: err.Error()}
	}
	feed := CapsuleFeed{Items: []string{}, NextCursor: next}
	for _, rec := range records {
		env, decodeErr := capsule.DecodeEnvelope([]byte(rec.Envelope))
		if decodeErr != nil {
			continue // stored garbage never crosses the wire
		}
		doc, docErr := capsule.DecodePayload(env)
		if docErr != nil {
			continue
		}
		visible := true
		for _, r := range doc.Records {
			ref := contract.ResourceRef{Kind: contract.ResourceKind(r.Subject.Kind), ID: contract.ResourceID(r.Subject.ID)}
			if _, err := contract.ClampRefs(principal, grant.Scopes, []contract.ResourceRef{ref}); err != nil {
				visible = false
				break
			}
		}
		if !visible {
			if clampAudit != nil {
				clampAudit(rec.CapsuleID)
			}
			continue
		}
		feed.Items = append(feed.Items, rec.Envelope)
	}
	return feed, nil
}

// PullRejected serves this replica's rejection history after cursor.
func (s *Server) PullRejected(principal contract.ActorID, cursor int64, limit int) (RejectedFeed, *Problem) {
	if _, ok := s.grants.Grant(principal, contract.SyncVerbPull); !ok {
		return RejectedFeed{}, &Problem{Type: ProblemScopeOutOfGrant, Title: "no pull grant", Status: 403,
			Detail: fmt.Sprintf("principal %q has no replica grant for %s", principal, contract.SyncVerbPull)}
	}
	records, next, err := s.store.HubRejectedAfter(string(principal), cursor, limit)
	if err != nil {
		return RejectedFeed{}, &Problem{Type: ProblemEnvelopeMalformed, Title: "store read failed", Status: 500, Detail: err.Error()}
	}
	feed := RejectedFeed{Items: []RejectedEntry{}, NextCursor: next}
	for _, rec := range records {
		feed.Items = append(feed.Items, RejectedEntry{CapsuleID: rec.CapsuleID, Code: rec.Code, Detail: rec.Detail, ReceivedAt: rec.ReceivedAt})
	}
	return feed, nil
}

func (s *Server) recordRejection(principal contract.ActorID, capsuleID string, problem *Problem) {
	if capsuleID == "" {
		capsuleID = "(unverifiable)"
	}
	_ = s.store.RecordHubRejected(string(principal), capsuleID, problem.Type, problem.Detail, s.now())
}

// verifyProblem maps offline-verify issues onto the closed problem types.
func verifyProblem(res capsule.VerifyResult) *Problem {
	title := "capsule verification failed"
	problemType := ProblemEnvelopeMalformed
	var details []string
	for _, issue := range res.Issues {
		details = append(details, issue.String())
		switch issue.Code {
		case capsule.ErrSigInvalid:
			problemType = ProblemSignatureInvalid
		case capsule.ErrCanonMismatch:
			problemType = ProblemCanonicalMismatch
		case capsule.ErrBlobMissing:
			problemType = ProblemBlobMissing
		case capsule.ErrBlobDigestBad:
			problemType = ProblemBlobDigestMismatch
		}
	}
	return &Problem{Type: problemType, Title: title, Status: 422, Detail: strings.Join(details, "; ")}
}
