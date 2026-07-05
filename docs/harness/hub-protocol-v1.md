# Hub Protocol v1 (capsule-native)

Successor of `sync-abi-v1/v2` (both retired with the legacy `/sync`
three-verb wire at R4 S4). Topology is the hub star: nodes talk to a
hub; nodes never talk to each other.

## Endpoints

```
POST /capsules                 push one DSSE envelope (≤1 MiB)
GET  /capsules?cursor=&limit=  pull after cursor (&origin=<replica id>
                               excludes the puller's own capsules —
                               self-edge devices may share a credential)
GET  /capsules/rejected?cursor=  this replica's rejection history
PUT  /blobs/{digest}           content-addressed blob (push before capsule)
GET  /blobs/{digest}           immutable, ETag = digest
HEAD /                         X-Mnemon-Hub-Protocol: v1
```

## Push semantics

- Content addressing is the idempotency key: no `Idempotency-Key`
  header exists, and a same-id replay of an ACCEPTED capsule returns
  `200` + `Idempotency-Replayed: true`.
- Rejection is NON-terminal (`422` + `application/problem+json`):
  - `blob-missing` → push the blob, re-push the SAME package;
  - content fix → new capsule_id, new package;
  - server-side fix (grant, quota) → the same package re-adjudicates
    as a first submission. Rejected history stays queryable.
- The grant clamp is whole-capsule: a signed atom is never
  record-filtered; partial overreach rejects (push) or hides (pull,
  with a clamped audit line).

## problem+json types (closed)

```
about:mnemon/hub/scope-out-of-grant
about:mnemon/hub/blob-missing
about:mnemon/hub/signature-invalid
about:mnemon/hub/canonical-mismatch
about:mnemon/hub/envelope-malformed
about:mnemon/hub/blob-digest-mismatch   (400, PUT /blobs)
```

`detail` is a human-readable diagnostic; Chinese round-trips intact
(gate: `harness/scripts/r4/hub-contract`).

## Wire freeze

`harness/internal/mnemonhub/testdata/hub-protocol/capsule-v1.json` (+
`.id`) freezes one canonical fixed-seed capsule; drift is a red test
and means a deliberate protocol rev.
