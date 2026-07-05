#!/usr/bin/env bash
# Runs inside the container: node A federates a governed report + blob to
# node B through a hub, and B verifies the pulled capsule offline.
set -euo pipefail
WORK="$(mktemp -d)"; cd "$WORK"
mkdir -p hub
printf '%s\n' "aaaa1111bbbb2222cccc3333dddd4444eeee5555ffff6666" >hub/a.token
printf '%s\n' "9999aaaa8888bbbb7777cccc6666dddd5555eeee4444ffff" >hub/b.token
chmod 600 hub/*.token
cat >hub/replicas.json <<'JSON'
{"schema_version":1,"replicas":[
 {"principal":"a@dock","credential_ref":"a.token","scopes":[{"kind":"progress_digest","id":"project"}]},
 {"principal":"b@dock","credential_ref":"b.token","scopes":[{"kind":"progress_digest","id":"project"}]}]}
JSON
chmod 600 hub/replicas.json
mnemon-hub --addr 127.0.0.1:9840 --store hub/hub.db --replicas hub/replicas.json >hub.log 2>&1 &
sleep 0.6
BASE=http://127.0.0.1:9840

mkdir -p a; ( cd a
  mnemon-harness setup --host codex --principal codex@a --control-url http://127.0.0.1:8840 >/dev/null
  tok=.mnemon/harness/channel/credentials/codex-a.token
  mnemon-harness local run >run.log 2>&1 & NP=$!
  for i in $(seq 1 60); do mnemon-harness control status --addr http://127.0.0.1:8840 --principal codex@a --token-file "$tok" >/dev/null 2>&1 && break; sleep 0.1; done
  printf '# 排查\n\n对账窗口保持时间应改回 30000。\n' > doc.md
  mnemon-harness teamwork report --addr http://127.0.0.1:8840 --principal codex@a --token-file "$tok" \
    --outcome result --summary "docker 双节点:对账窗口保持时间排查完成,详见附件。" --attach doc.md --external-id dock1 >/dev/null
  kill $NP 2>/dev/null; wait $NP 2>/dev/null || true
  mnemon-harness remote add hub --endpoint "$BASE" --token-file "$WORK/hub/a.token" --allow-insecure >/dev/null
  mnemon-harness push >/dev/null )
echo "node A pushed."

mkdir -p b; ( cd b
  mnemon-harness setup --host codex --principal codex@b --control-url http://127.0.0.1:8841 >/dev/null
  mnemon-harness remote add hub --endpoint "$BASE" --token-file "$WORK/hub/b.token" --allow-insecure >/dev/null
  mnemon-harness pull >/dev/null
  tok=.mnemon/harness/channel/credentials/codex-b.token
  mnemon-harness local run >run.log 2>&1 & NP=$!
  for i in $(seq 1 60); do mnemon-harness control status --addr http://127.0.0.1:8841 --principal codex@b --token-file "$tok" >/dev/null 2>&1 && break; sleep 0.1; done
  hit="$(mnemon-harness recall "对账窗口" --addr http://127.0.0.1:8841 --principal codex@b --token-file "$tok")"
  kill $NP 2>/dev/null; wait $NP 2>/dev/null || true
  case "$hit" in *"对账窗口"*) ;; *) echo "node B recall miss: $hit"; exit 1;; esac
  ls .mnemon/harness/blobs/sha256/ | grep -q . || { echo "node B blob not materialized"; exit 1; } )
echo "docker double-node OK (A→hub→B federation + recall + blob closure)"
