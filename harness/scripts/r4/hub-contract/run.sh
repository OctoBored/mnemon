#!/usr/bin/env bash
# R4 hub contract (C5 script level): capsule push/replay/422/rejected/304
# against a live hub over TLS, with Chinese problem+json detail intact.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
WORK="$(mktemp -d)"
cleanup() { pkill -f "$WORK" 2>/dev/null || true; rm -rf "$WORK"; }
trap cleanup EXIT

go build -o "$WORK/mnemon-harness" ./harness/cmd/mnemon-harness
go build -o "$WORK/mnemon-hub" ./harness/cmd/mnemon-hub
"$WORK/mnemon-hub" --dev-selfsigned "$WORK/tls" >/dev/null

hubdir="$WORK/hub"; mkdir -p "$hubdir"
printf '%s\n' "aaaa1111bbbb2222cccc3333dddd4444eeee5555ffff6666" >"$hubdir/replica-a.token"
printf '%s\n' "9999aaaa8888bbbb7777cccc6666dddd5555eeee4444ffff" >"$hubdir/replica-b.token"
printf '%s\n' "cccc1111dddd2222eeee3333ffff4444aaaa5555bbbb6666" >"$hubdir/replica-c.token"
chmod 600 "$hubdir"/replica-*.token
cat >"$hubdir/replicas.json" <<'JSON'
{"schema_version":1,"replicas":[
 {"principal":"replica-a@hub","credential_ref":"replica-a.token",
  "scopes":[{"kind":"progress_digest","id":"project"}]},
 {"principal":"replica-b@hub","credential_ref":"replica-b.token",
  "scopes":[{"kind":"progress_digest","id":"project"}]},
 {"principal":"replica-c@hub","credential_ref":"replica-c.token",
  "scopes":[{"kind":"progress_digest","id":"elsewhere"}]}]}
JSON
chmod 600 "$hubdir/replicas.json"
"$WORK/mnemon-hub" --addr 127.0.0.1:9808 --store "$hubdir/hub.db" --replicas "$hubdir/replicas.json" \
	--tls-cert "$WORK/tls/cert.pem" --tls-key "$WORK/tls/key.pem" >"$WORK/hub.log" 2>&1 &
HUBPID=$!
sleep 0.5
BASE="https://127.0.0.1:9808"
CURL=(curl -sS --cacert "$WORK/tls/cert.pem")
TOKA="Authorization: Bearer $(tr -d '\n' <"$hubdir/replica-a.token")"
TOKB="Authorization: Bearer $(tr -d '\n' <"$hubdir/replica-b.token")"

# 用 A 节点产一颗真 capsule:走产品路径(setup+observe+push)
proj="$WORK/proj-a"; mkdir -p "$proj"; cd "$proj"
"$WORK/mnemon-harness" setup --host codex --principal codex@payments --control-url http://127.0.0.1:8808 >/dev/null
tok=".mnemon/harness/channel/credentials/codex-payments.token"
"$WORK/mnemon-harness" local run >"$WORK/node-a.log" 2>&1 &
NODEPID=$!
for i in $(seq 1 60); do "$WORK/mnemon-harness" control status --addr http://127.0.0.1:8808 --principal codex@payments --token-file "$tok" >/dev/null 2>&1 && break; sleep 0.1; done
"$WORK/mnemon-harness" control observe --addr http://127.0.0.1:8808 --principal codex@payments --token-file "$tok" \
	--type progress_digest.write_candidate.observed --external-id hc1 \
	--payload '{"rule":{"outcome":"result","scope":"payments/reconcile"},"narrative":{"summary":"对账窗口修复完成","result":"恢复 30000"}}' >/dev/null
"$WORK/mnemon-harness" remote add hub --endpoint "$BASE" --token-file "$hubdir/replica-a.token" --ca-file "$WORK/tls/cert.pem" >/dev/null
"$WORK/mnemon-harness" push >/dev/null 2>&1 || true
kill "$NODEPID" 2>/dev/null; wait "$NODEPID" 2>/dev/null || true
"$WORK/mnemon-harness" push >/dev/null   # 离线单趟(store 已释放)
cd - >/dev/null

echo "=== hub-contract: feed + 304 ==="
feed="$("${CURL[@]}" -D "$WORK/h1" -H "$TOKB" "$BASE/capsules?cursor=0")"
case "$feed" in *payloadType*) ;; *) echo "feed missing capsule: $feed"; exit 1;; esac
etag="$(tr -d '\r' <"$WORK/h1" | grep -i '^ETag:' | awk '{print $2}')"
code="$("${CURL[@]}" -o /dev/null -w '%{http_code}' -H "$TOKB" -H "If-None-Match: $etag" "$BASE/capsules?cursor=0")"
[ "$code" = "304" ] || { echo "ETag revalidation = $code, want 304"; exit 1; }

echo "=== hub-contract: replay idempotent ==="
one="$(printf '%s' "$feed" | python3 -c 'import json,sys; print(json.dumps(json.load(sys.stdin)["items"][0]))')"
code="$(printf '%s' "$one" | "${CURL[@]}" -o "$WORK/replay.json" -w '%{http_code}' -H "$TOKA" -H 'Content-Type: application/vnd.dsse+json' -X POST --data-binary @- "$BASE/capsules")"
[ "$code" = "200" ] || { echo "replay = $code, want 200"; cat "$WORK/replay.json"; exit 1; }

echo "=== hub-contract: 422 problem 中文往返 + rejected 可查 ==="
# B 越界推送(scope billing 不在 grant):篡改 payload 不可行(签名),用 B 节点产 billing capsule
projb="$WORK/proj-b"; mkdir -p "$projb"; cd "$projb"
"$WORK/mnemon-harness" setup --host codex --principal codex@billing --control-url http://127.0.0.1:8809 >/dev/null
tokb=".mnemon/harness/channel/credentials/codex-billing.token"
"$WORK/mnemon-harness" local run >"$WORK/node-b.log" 2>&1 &
NODEPID=$!
for i in $(seq 1 60); do "$WORK/mnemon-harness" control status --addr http://127.0.0.1:8809 --principal codex@billing --token-file "$tokb" >/dev/null 2>&1 && break; sleep 0.1; done
"$WORK/mnemon-harness" control observe --addr http://127.0.0.1:8809 --principal codex@billing --token-file "$tokb" \
	--type progress_digest.write_candidate.observed --external-id hc2 \
	--payload '{"rule":{"outcome":"blocker","scope":"billing/settle"},"narrative":{"summary":"会员权益结算延迟联动排查","blocker":"缺少 staging 权限"}}' >/dev/null
kill "$NODEPID" 2>/dev/null; wait "$NODEPID" 2>/dev/null || true
"$WORK/mnemon-harness" remote add hub --endpoint "$BASE" --token-file "$hubdir/replica-c.token" --ca-file "$WORK/tls/cert.pem" >/dev/null
if "$WORK/mnemon-harness" push >"$WORK/push-b.log" 2>&1; then :; fi
cd - >/dev/null
# grant 只有 payments → progress_digest/billing? 注意 subject id = billing?
TOKC="Authorization: Bearer $(tr -d '\n' <"$hubdir/replica-c.token")"
rejected="$("${CURL[@]}" -H "$TOKC" "$BASE/capsules/rejected?cursor=0")"
case "$rejected" in
	*scope-out-of-grant*) ;;
	*) echo "rejected feed missing scope problem: $rejected"; exit 1;;
esac
case "$rejected" in
	*"越出授权范围"*) ;;
	*) echo "Chinese diagnostic mangled in rejected feed: $rejected"; exit 1;;
esac
kill "$HUBPID" 2>/dev/null; wait "$HUBPID" 2>/dev/null || true
echo "r4 hub-contract OK (C5 中文往返含拒收非终态可查)"
