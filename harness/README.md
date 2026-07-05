# Mnemon Harness

`mnemon-harness` is the Agent Integration layer around Local Mnemon: a
governed event node per project, a capsule-native federation edge, and a
single boundary brief for host agents.

## Command surface (R4)

Protocol layer:

- `setup` — install Agent Integration (hooks, guide, credentials) for
  Codex or Claude Code. Host lifecycle mapping comes from each host's
  `host.json` (`enter` / `mid` / `exit`).
- `node up|down|reload|status|logs|doctor|serve|wake` — the local event
  node lifecycle (one boot face; `mnemond` is a thin compat shim).
- `emit` — generic two-zone submission: `--schema` picks the kind,
  `--rule k=v` fills the rule zone, stdin JSON or `--narrative k=v` is
  the semantic zone.
- `recall` — keyword search over this principal's own governed store.
- `view` — render the boundary brief (`[mnemon:context]`,
  `[mnemon:handoff]`, `[mnemon:contract]`); hooks call it at lifecycle
  edges.
- `verify` — offline capsule verification: DSSE signature, canonical
  id, blob closure.
- `push` / `pull` · `remote add|list|remove` — capsule federation over
  the hub star (`hub serve` hosts one; protocol v1 under
  `docs/harness/hub-protocol-v1.md`).
- `watch` — the read-only four-page operator boundary.
- `api` — the only hidden escape hatch (credential-managed raw calls).

Capability dialect (teamwork, the flagship tenant):

- `teamwork signal|assign|report|profile` — `report` carries
  `--outcome progress|result|blocker` and `--attach <file>` (content
  travels as content-addressed blobs, never as dead path references).

Display adapters live in their own binary: `mnemon-multica`
(`dispatch`, `import-issue`, `surface-report`, `activation-carrier`,
`provision`, `participant`, plus the managed runtime RPC face).

## Build

```sh
go build -o mnemon-harness ./harness/cmd/mnemon-harness
go build -o mnemon-hub ./harness/cmd/mnemon-hub
go build -o mnemon-multica ./harness/cmd/mnemon-multica
```

## Quick start

```sh
./mnemon-harness setup --host codex
./mnemon-harness node up
./mnemon-harness teamwork report --outcome result \
  --summary "排查完成,修复值见附件。" --attach 排查文档.md
./mnemon-harness view
```

Federate two nodes through a hub:

```sh
./mnemon-harness hub serve --addr 127.0.0.1:9787 \
  --store hub.db --replicas replicas.json
./mnemon-harness remote add team --endpoint https://hub:9787 \
  --token-file replica.token --ca-file cert.pem
./mnemon-harness push && ./mnemon-harness pull
```

## Verification

- `go test ./harness/...` — unit + guard suites (command registry,
  field-consumer registry, test layout, trust-domain boundaries).
- `bash harness/scripts/e2e.sh` — full end-to-end (both hosts, sync
  pair over TLS, daemon lifecycle, tower).
- `harness/scripts/r4/` — protocol acceptance: `preflight`,
  `hub-contract` (C5), `c2-selfedge`, `c1-isolation` (the definitional
  true-handoff gate; spends one real Codex turn), `e2-five-node`
  (C6/C7).

Runtime state lives under `.mnemon/harness/`; host directories such as
`.codex/` and `.claude/` are projection surfaces the uninstaller can
cleanly remove.
