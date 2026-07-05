#!/usr/bin/env bash
# R4 preflight: binaries build, node boots and serves the local edge,
# hub boots and reports the capsule protocol version.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
WORK="$(mktemp -d)"
cleanup() { pkill -f "$WORK" 2>/dev/null || true; rm -rf "$WORK"; }
trap cleanup EXIT

echo "=== r4 preflight: build ==="
go build -o "$WORK/mnemon-harness" ./harness/cmd/mnemon-harness
go build -o "$WORK/mnemond" ./harness/cmd/mnemond
go build -o "$WORK/mnemon-hub" ./harness/cmd/mnemon-hub
go build -o "$WORK/mnemon-multica" ./harness/cmd/mnemon-multica

echo "=== r4 preflight: node boots ==="
proj="$WORK/proj"; mkdir -p "$proj"; cd "$proj"
"$WORK/mnemon-harness" setup --host codex --principal codex@project --control-url http://127.0.0.1:8807 >/dev/null
"$WORK/mnemon-harness" local run >"$WORK/node.log" 2>&1 &
NODEPID=$!
tok=".mnemon/harness/channel/credentials/codex-project.token"
for i in $(seq 1 60); do
	"$WORK/mnemon-harness" control status --addr http://127.0.0.1:8807 --principal codex@project --token-file "$tok" >/dev/null 2>&1 && break
	sleep 0.1
done
"$WORK/mnemon-harness" view --addr http://127.0.0.1:8807 --principal codex@project --token-file "$tok" >/dev/null
kill "$NODEPID" 2>/dev/null; wait "$NODEPID" 2>/dev/null || true
cd - >/dev/null

echo "=== r4 preflight: hub protocol ==="
hubdir="$WORK/hub"; mkdir -p "$hubdir"
printf '%s\n' "aaaa1111bbbb2222cccc3333dddd4444eeee5555ffff6666" >"$hubdir/replica-a.token"
chmod 600 "$hubdir/replica-a.token"
cat >"$hubdir/replicas.json" <<'JSON'
{"schema_version":1,"replicas":[{"principal":"replica-a@hub","credential_ref":"replica-a.token",
 "scopes":[{"kind":"progress_digest","id":"project"}]}]}
JSON
chmod 600 "$hubdir/replicas.json"
"$WORK/mnemon-hub" --addr 127.0.0.1:9807 --store "$hubdir/hub.db" --replicas "$hubdir/replicas.json" >"$WORK/hub.log" 2>&1 &
HUBPID=$!
sleep 0.5
proto="$(curl -sSI http://127.0.0.1:9807/ -X HEAD 2>/dev/null | tr -d '\r' | grep -i '^X-Mnemon-Hub-Protocol:' | awk '{print $2}')"
[ "$proto" = "v1" ] || { echo "hub protocol header = '$proto', want v1"; exit 1; }
kill "$HUBPID" 2>/dev/null; wait "$HUBPID" 2>/dev/null || true
echo "r4 preflight OK"
