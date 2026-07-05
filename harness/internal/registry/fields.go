// Package registry is the R4 field-consumer registry (r4-registries §1):
// the closed 11-seat rule-zone vocabulary, each seat naming at least one
// REAL deterministic consumer. The Fn column is a typed function reference
// — deleting the consumer breaks this package's compile (guard-1); the
// Package/Func columns are the AST-queryable symbol (guard-2 liveness in
// coreguard verifies a non-test call site exists). Method consumers are
// referenced as METHOD EXPRESSIONS (receiver as first parameter): still a
// plain compile-checked function, never a bound closure.
//
// Wanting a new rule-zone field starts HERE: without a real consumer
// function the seat cannot be declared, and guard-3 keeps the policy
// decoders inside this vocabulary.
package registry

import (
	"github.com/mnemon-dev/mnemon/harness/internal/contract"
	eventmodel "github.com/mnemon-dev/mnemon/harness/internal/event"
	"github.com/mnemon-dev/mnemon/harness/internal/mnemond/policy"
	"github.com/mnemon-dev/mnemon/harness/internal/mnemond/presentation"
	"github.com/mnemon-dev/mnemon/harness/internal/mnemond/presentation/view"
	"github.com/mnemon-dev/mnemon/harness/internal/mnemond/state"
)

type FieldSpec struct {
	Name      string
	Purpose   string
	Consumers []ConsumerRef
}

type ConsumerRef struct {
	Fn      any    // typed function reference (compile-time existence)
	Package string // import path suffix for AST queries
	Func    string // symbol name for AST queries
	Reason  string
}

var Fields = []FieldSpec{
	{
		Name:    "verb",
		Purpose: "admission routing, risk tier, authz whitelist",
		Consumers: []ConsumerRef{{
			Fn: policy.StandardRegistry, Package: "mnemond/policy", Func: "StandardRegistry",
			Reason: "the catalog routes observed types to packages and carries each kind's risk tier",
		}},
	},
	{
		Name:    "subject",
		Purpose: "CAS / clamp / merge identity (kind,id); decode aliases signal_id and assignment_id are its item-level material",
		Consumers: []ConsumerRef{{
			Fn: view.ScopedView, Package: "mnemond/presentation/view", Func: "ScopedView",
			Reason: "server-enforced scope materializes exactly the subject refs",
		}},
	},
	{
		Name:    "actor",
		Purpose: "authorization and ownership predicates",
		Consumers: []ConsumerRef{{
			Fn: presentation.DeriveEventEnvelopes, Package: "mnemond/presentation", Func: "DeriveEventEnvelopes",
			Reason: "owner/assignee branching in the cue engine",
		}},
	},
	{
		Name:    "audience",
		Purpose: "scoped render and wake-candidate addressing",
		Consumers: []ConsumerRef{{
			Fn: eventmodel.DerivedEnvelope, Package: "event", Func: "DerivedEnvelope",
			Reason: "derived envelopes are audience-addressed",
		}},
	},
	{
		Name:    "assignee",
		Purpose: "work_available predicate",
		Consumers: []ConsumerRef{{
			Fn: presentation.DeriveEventEnvelopes, Package: "mnemond/presentation", Func: "DeriveEventEnvelopes",
			Reason: "assignee==principal derives the merged work cue",
		}},
	},
	{
		Name:    "ttl",
		Purpose: "expiry cue (single duration representation)",
		Consumers: []ConsumerRef{{
			Fn: presentation.DeriveEventEnvelopes, Package: "mnemond/presentation", Func: "DeriveEventEnvelopes",
			Reason: "assignment expiry and profile staleness derive from ttl",
		}},
	},
	{
		Name:    "assignment_ref",
		Purpose: "progress↔assignment join (Link rel=assignment)",
		Consumers: []ConsumerRef{{
			Fn: presentation.DeriveEventEnvelopes, Package: "mnemond/presentation", Func: "DeriveEventEnvelopes",
			Reason: "links reports to assignments for integrate/blocker cues",
		}},
	},
	{
		Name:    "evidence_refs",
		Purpose: "risk-gate non-empty check (Link rel=evidence); single enforcement layer",
		Consumers: []ConsumerRef{{
			Fn: policy.RiskEvidenceGate, Package: "mnemond/policy", Func: "RiskEvidenceGate",
			Reason: "mid-risk candidates must carry evidence — the one place this is law",
		}},
	},
	{
		Name:    "external_id",
		Purpose: "idempotency (inbox dedupe)",
		Consumers: []ConsumerRef{{
			Fn: (*state.Store).IngestObservation, Package: "mnemond/state", Func: "IngestObservation",
			Reason: "duplicate external ids return the original receipt instead of re-ingesting",
		}},
	},
	{
		Name:    "outcome",
		Purpose: "progress|result|blocker (renamed feedback_kind)",
		Consumers: []ConsumerRef{{
			Fn: presentation.ReduceDeliveries, Package: "mnemond/presentation", Func: "ReduceDeliveries",
			Reason: "delivery milestone judgment (result/blocker deliver, progress stays silent)",
		}, {
			Fn: presentation.DeriveEventEnvelopes, Package: "mnemond/presentation", Func: "DeriveEventEnvelopes",
			Reason: "outcome=blocker derives the deterministic blocker cue (C6)",
		}},
	},
	{
		Name:    "scope",
		Purpose: "overlap cue material + Multica session scope filter",
		Consumers: []ConsumerRef{{
			Fn: presentation.DeriveEventEnvelopes, Package: "mnemond/presentation", Func: "DeriveEventEnvelopes",
			Reason: "scope rides cue bodies and the multica in-scope filter",
		}},
	},
}

// SubjectDecodeAliases are the per-kind item-identity fields that occupy the
// subject seat at decode time (they become the item id, i.e. subject
// material — not independent seats).
var SubjectDecodeAliases = map[string]bool{
	"signal_id":     true,
	"assignment_id": true,
}

// LinkRels is the closed Link rel vocabulary (capsule-format §2); guard-3
// enforces it for the refs lane.
var LinkRels = []string{"evidence", "artifact", "caused-by", "assignment", "external"}

// RuleZoneAllowed reports whether a policy decoder rule-zone field name is
// inside the registry vocabulary.
func RuleZoneAllowed(name string) bool {
	if SubjectDecodeAliases[name] {
		return true
	}
	for _, f := range Fields {
		if f.Name == name {
			return true
		}
	}
	return false
}

// compile-time pins: the method-expression consumers keep their signatures.
var _ func(*state.Store, contract.ObservationEnvelope) (int64, bool, error) = (*state.Store).IngestObservation
