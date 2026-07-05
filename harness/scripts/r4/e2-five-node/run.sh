#!/usr/bin/env bash
# E2 五节点 scripted(C6 会员权益结算延迟联动排查 + C7 并发 PoC 重叠):
# N1 发起+两定向 assignment · N2 调研交付中文文档 blob · N3 支付侧中途
# blocker · N4 账务侧自派(cue 邀请) · N5 整合前 verify 上游 capsule。
# 断言: capsule 计数≥8 · N4 brief 含 signal cue(邀请非指令) · N3 blocker
# 后 N1 brief 现 blocker cue · N5 verify 全绿 · 零 agent↔agent 直连
# (每节点 remotes.json 只含 hub)· C7: 双 PoC 同 scope → overlap cue。
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
WORK="$(mktemp -d)"
cleanup() { pkill -f "$WORK" 2>/dev/null || true; rm -rf "$WORK"; }
trap cleanup EXIT

go build -o "$WORK/mh" ./harness/cmd/mnemon-harness
go build -o "$WORK/hub" ./harness/cmd/mnemon-hub

hubdir="$WORK/hub-store"; mkdir -p "$hubdir"
scopes='[{"kind":"progress_digest","id":"project"},{"kind":"assignment","id":"project"},{"kind":"teamwork_signal","id":"project"},{"kind":"agent_profile","id":"project"}]'
{
  echo '{"schema_version":1,"replicas":['
  for n in 1 2 3 4 5; do
    tok="tok-n$n-aaaabbbbccccddddeeeeffff0000111122223333"
    printf '%s\n' "$tok" >"$hubdir/n$n.token"; chmod 600 "$hubdir/n$n.token"
    sep=','; [ "$n" = 5 ] && sep=''
    echo " {\"principal\":\"replica-n$n@e2\",\"credential_ref\":\"n$n.token\",\"scopes\":$scopes}$sep"
  done
  echo ']}'
} >"$hubdir/replicas.json"
chmod 600 "$hubdir/replicas.json"
"$WORK/hub" --addr 127.0.0.1:9830 --store "$hubdir/hub.db" --replicas "$hubdir/replicas.json" >"$WORK/hub.log" 2>&1 &
HUBPID=$!
sleep 0.5
BASE="http://127.0.0.1:9830"

node_env() { echo "$WORK/n$1"; }
node_port() { echo "88$1"0; }
tokpath() { echo ".mnemon/harness/channel/credentials/agent-n$1-e2.token"; }

start_node() { # $1=n
	local n=$1 dir; dir="$(node_env "$n")"; mkdir -p "$dir"; cd "$dir"
	"$WORK/mh" setup --host codex --principal "agent-n$n@e2" --control-url "http://127.0.0.1:$(node_port "$n")" >/dev/null
	"$WORK/mh" remote add hub --endpoint "$BASE" --token-file "$hubdir/n$n.token" --allow-insecure >/dev/null
	"$WORK/mh" local run >"$WORK/n$n.log" 2>&1 &
	echo $! >"$WORK/n$n.pid"
	local i; for i in $(seq 1 60); do "$WORK/mh" control status --addr "http://127.0.0.1:$(node_port "$n")" --principal "agent-n$n@e2" --token-file "$(tokpath "$n")" >/dev/null 2>&1 && break; sleep 0.1; done
	cd - >/dev/null
}
stop_node() { local pid; pid="$(cat "$WORK/n$1.pid")"; kill "$pid" 2>/dev/null; wait "$pid" 2>/dev/null || true; }
sync_node() { local n=$1; ( cd "$(node_env "$n")" && "$WORK/mh" push >/dev/null && "$WORK/mh" pull >/dev/null ); }
mh_n() { local n=$1; shift; ( cd "$(node_env "$n")" && "$WORK/mh" "$@" --addr "http://127.0.0.1:$(node_port "$n")" --principal "agent-n$n@e2" --token-file "$(tokpath "$n")" ); }

for n in 1 2 3 4 5; do start_node "$n"; done

echo "=== C6: N1 发起 + 两定向 assignment ==="
mh_n 1 teamwork signal --scope member/settlement --ttl 4h \
	--statement "会员权益结算延迟联动排查:背景=结算批延迟 40 分钟;现象=权益到账滞后;影响范围=会员侧全量;已知约束=灰度环境紧张;验收标准=定位根因并给出修复参数。" \
	--why-teamwork "需要调研、支付、账务三侧并行" --evidence "OA-会员权益结算延迟" --external-id e2-signal >/dev/null
mh_n 1 teamwork assign --assignee agent-n2@e2 --scope member/settlement --ttl 3h \
	--assignment-id e2-asg-n2 --work "调研侧:梳理结算链路并交付中文调研文档" --feedback "result+附件" --evidence "OA-会员权益结算延迟" --external-id e2-asg-n2 >/dev/null
mh_n 1 teamwork assign --assignee agent-n3@e2 --scope member/settlement --ttl 3h \
	--assignment-id e2-asg-n3 --work "支付侧:核对结算批触发条件" --feedback "result 或 blocker" --evidence "OA-会员权益结算延迟" --external-id e2-asg-n3 >/dev/null
stop_node 1; sync_node 1; start_node 1

for n in 2 3 4; do stop_node "$n"; sync_node "$n"; start_node "$n"; done

echo "=== N4 brief: cue 邀请(非指令)==="
brief4="$(mh_n 4 view)"
echo "$brief4" >"$WORK/brief-n4-before.txt"
case "$brief4" in *"[mnemon:signal]"*) ;; *) echo "N4 brief 缺 signal cue:"; echo "$brief4"; exit 1;; esac
case "$brief4" in *"Assignment or self-assignment may be useful"*) ;; *) echo "N4 brief 缺自派邀请句:"; echo "$brief4"; exit 1;; esac
echo "=== N4 自派(源于 cue)==="
mh_n 4 teamwork assign --assignee agent-n4@e2 --scope member/settlement --ttl 3h \
	--assignment-id e2-asg-n4 --work "账务侧自派:核对权益到账台账" --feedback "result" --evidence "OA-会员权益结算延迟" --external-id e2-asg-n4 >/dev/null

echo "=== N2 调研交付(中文文档 blob)==="
( cd "$(node_env 2)"; printf '# 结算链路调研\n\n结算批调度参数 settle.batch_window_ms 现值过大,建议压缩;详见对账窗口联动。\n' > 调研文档.md )
mh_n 2 teamwork report --outcome result --summary "调研完成:结算链路梳理见附件。" --attach 调研文档.md --external-id e2-n2-result >/dev/null

echo "=== N3 中途 blocker ==="
mh_n 3 teamwork report --outcome blocker --assignment-ref e2-asg-n3 \
	--summary "支付侧核对受阻。" --blocker "缺灰度环境,无法复现结算批触发。" --external-id e2-n3-blocker >/dev/null

echo "=== N4 账务结论 ==="
mh_n 4 teamwork report --outcome result --assignment-ref e2-asg-n4 --summary "账务台账核对完成:到账滞后与结算批窗口一致。" --external-id e2-n4-result >/dev/null

for n in 2 3 4; do stop_node "$n"; sync_node "$n"; start_node "$n"; done
stop_node 1; sync_node 1; start_node 1

echo "=== N1 brief 顶部 blocker cue ==="
brief1="$(mh_n 1 view)"
echo "$brief1" >"$WORK/brief-n1.txt"
case "$brief1" in *"[mnemon:blocker]"*) ;; *) echo "N1 brief 缺 blocker cue:"; echo "$brief1"; exit 1;; esac
case "$brief1" in *"缺灰度环境"*) ;; *) echo "blocker cue 缺原文:"; echo "$brief1"; exit 1;; esac

echo "=== N5 整合前 verify 上游 capsule 链 ==="
stop_node 5; sync_node 5
( cd "$(node_env 5)"
  curl -sS -H "Authorization: Bearer $(tr -d '\n' <"$hubdir/n5.token")" "$BASE/capsules?cursor=0&limit=100" \
    | python3 -c '
import json,sys,os
feed=json.load(sys.stdin)
os.makedirs("upstream",exist_ok=True)
for i,item in enumerate(feed["items"]):
    open(f"upstream/c{i:02d}.json","w").write(json.dumps(item))
print(len(feed["items"]))' >"$WORK/capsule-count.txt"
  count=$(cat "$WORK/capsule-count.txt")
  [ "$count" -ge 7 ] || { echo "上游 capsule 计数 $count < 7"; exit 1; }
  echo "上游 capsule 计数: $count"
  for f in upstream/*.json; do
    "$WORK/mh" verify "$f" --blobs .mnemon/harness/blobs >/dev/null || { echo "verify 红: $f"; exit 1; }
  done
  echo "N5 verify 全绿($count 颗)"
)
start_node 5

echo "=== N5 整合记录(第 8 次边界穿越)==="
mh_n 5 teamwork report --outcome result --summary "整合:结算批窗口参数与权益到账台账对齐,调研/支付/账务三侧结论并入。" --external-id e2-n5-integrate >/dev/null
stop_node 5; sync_node 5; start_node 5
total="$(curl -sS -H "Authorization: Bearer $(tr -d '\n' <"$hubdir/n1.token")" "$BASE/capsules?cursor=0&limit=100" | python3 -c 'import json,sys; print(len(json.load(sys.stdin)["items"]))')"
[ "$total" -ge 8 ] || { echo "全程 capsule 计数 $total < 8"; exit 1; }
echo "全程 capsule 计数: $total(≥8 边界穿越)"

echo "=== C7: 并发 PoC 同 scope → overlap cue ==="
mh_n 2 teamwork signal --scope ledger/reconcile-schema --ttl 2h \
	--statement "库存偏差 PoC:需调整对账表 schema。" --why-teamwork "涉及共享 schema" --evidence "poc-inventory" --external-id e2-poc-a >/dev/null
mh_n 3 teamwork signal --scope ledger/reconcile-schema --ttl 2h \
	--statement "退款风控 PoC:同样触及对账表 schema。" --why-teamwork "涉及共享 schema" --evidence "poc-refund" --external-id e2-poc-b >/dev/null
for n in 2 3; do stop_node "$n"; sync_node "$n"; start_node "$n"; done
brief2="$(mh_n 2 view)"
case "$brief2" in *"[mnemon:overlap]"*) ;; *) echo "N2 brief 缺 overlap cue:"; echo "$brief2"; exit 1;; esac
case "$brief2" in *"scope 相同"*) ;; *) echo "overlap 判据缺失:"; echo "$brief2"; exit 1;; esac
echo "=== 整合裁决成为一条 record ==="
mh_n 5 teamwork report --outcome result --summary "整合裁决:两 PoC 的 schema 变更合并为单一迁移,由账务侧执行。" --external-id e2-ruling >/dev/null

echo "=== 零 agent↔agent 直连 ==="
for n in 1 2 3 4 5; do
  endpoints="$(python3 -c "
import json
doc=json.load(open('$(node_env "$n")/.mnemon/harness/sync/remotes.json'))
print(' '.join(e['endpoint'] for e in doc['remotes']))")"
  [ "$endpoints" = "$BASE" ] || { echo "N$n 存在非 hub 端点: $endpoints"; exit 1; }
done
for n in 1 2 3 4 5; do stop_node "$n"; done
kill $HUBPID 2>/dev/null; wait $HUBPID 2>/dev/null || true
echo "r4 E2 five-node OK (C6: 计数/邀请/blocker/verify; C7: overlap 暴露+裁决记录; 零直连)"
