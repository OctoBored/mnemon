// Package mnemonhub is the remote event exchange for synced event envelopes. The accept surface is
// the replica grant scope: push adjudicates synced envelopes, pull serves accepted synced envelopes
// with the same scope clamp, and status reports event counters. It is extracted from the runtime so
// the standalone hub binary (mnemon-hub) can host the same wire without the runtime: it imports ONLY
// contract + store (+stdlib) — never channel / runtime / app / hostagent (the trust-domain import
// boundary, pinned by a test). Replica authorization enters through the Grants seam; the co-hosted
// runtime adapts its channel bindings to grants, mnemon-hub builds grants from replicas.json — same
// fields, same semantics (dual-form rule).
package mnemonhub

import (
	"errors"
	"fmt"

	"github.com/mnemon-dev/mnemon/harness/internal/contract"
	"github.com/mnemon-dev/mnemon/harness/internal/mnemond/state"
)

// ReplicaGrant aliases the contract type (the grant record is ABI surface, sync-abi-v1 §2).
type ReplicaGrant = contract.ReplicaGrant

// BadRequestError marks a request-VALIDATION failure (a malformed/missing field) as distinct from an
// AUTHORIZATION failure (no grant / out-of-scope). The HTTP layer maps it to 400; everything else
// from Push/Pull/Status (no replica grant, out-of-scope clamp) stays 403 (LOW-10). It is the wire
// layer's only error-class signal — per-event accept/reject/conflict verdicts ride the 200 body.
type BadRequestError struct{ err error }

func (e *BadRequestError) Error() string { return e.err.Error() }
func (e *BadRequestError) Unwrap() error { return e.err }

// badRequestf builds a BadRequestError (validation class).
func badRequestf(format string, a ...any) error {
	return &BadRequestError{err: fmt.Errorf(format, a...)}
}

// IsBadRequest reports whether err is (or wraps) a request-validation failure.
func IsBadRequest(err error) bool {
	var bre *BadRequestError
	return errors.As(err, &bre)
}

// Grants resolves an authenticated principal's replica grant for ONE sync verb (a contract.SyncVerb*
// string). Implementations MUST be fail-closed: an unknown principal, a non-replica principal, or an
// ungranted verb returns false — there is no anonymous or default grant.
type Grants interface {
	Grant(principal contract.ActorID, verb string) (ReplicaGrant, bool)
}

// GrantMap is the static Grants form mnemon-hub builds from replicas.json: every listed replica holds
// all three sync verbs (a replica credential is sync-only by construction; per-verb narrowing is the
// co-hosted binding form's concern).
type GrantMap map[contract.ActorID]ReplicaGrant

func (m GrantMap) Grant(principal contract.ActorID, verb string) (ReplicaGrant, bool) {
	if verb != contract.SyncVerbPush && verb != contract.SyncVerbPull && verb != contract.SyncVerbStatus {
		return ReplicaGrant{}, false
	}
	g, ok := m[principal]
	return g, ok
}

// Server is one hub over one open state. It holds no other state: adjudication and counters are
// durable in the store, so a restart (or a concurrent co-hosted runtime surface) sees the same hub.
type Server struct {
	store  *state.Store
	grants Grants
	now    func() string
}

// New wires a hub Server over an OPEN store (the caller owns the store's single-writer lock and its
// lifecycle). now stamps received_at on accepted synced events.
func New(st *state.Store, grants Grants, now func() string) *Server {
	return &Server{store: st, grants: grants, now: now}
}
