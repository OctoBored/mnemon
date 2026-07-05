#!/usr/bin/env bash
# C1 真隔离交接(定义性 gate;E1 中文复跑)——支付回调对账延迟事故:
# A 节点排查完成(文档含 needle: reconcile.window_hold_ms 应从 43200000
# 改回 30000, 干扰项 callback.retry_backoff_ms 无需修改), 教义级交接
# (teamwork report --attach, summary 不内联 needle)后 A 目录物理删除;
# B 节点 agent 真实回合接手。
# 机械 oracle: B 单回合输出含「window_hold_ms」且修复值=30000;
# B 的 brief handoff 节含 expected_work·result 字样·本机 artifact 路径;
# B 全程零访问 A 机路径。
# 预算: 每次运行消耗 1 个真实 Codex 回合(D2 台账记账)。
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
WORK="$(mktemp -d)"
cleanup() { pkill -f "$WORK" 2>/dev/null || true; rm -rf "$WORK"; }
trap cleanup EXIT

go build -o "$WORK/mnemon-harness" ./harness/cmd/mnemon-harness
go build -o "$WORK/mnemon-hub" ./harness/cmd/mnemon-hub

hubdir="$WORK/hub"; mkdir -p "$hubdir"
printf '%s\n' "aaaa1111bbbb2222cccc3333dddd4444eeee5555ffff6666" >"$hubdir/replica-a.token"
printf '%s\n' "9999aaaa8888bbbb7777cccc6666dddd5555eeee4444ffff" >"$hubdir/replica-b.token"
chmod 600 "$hubdir"/replica-*.token
cat >"$hubdir/replicas.json" <<'JSON'
{"schema_version":1,"replicas":[
 {"principal":"replica-a@e1","credential_ref":"replica-a.token",
  "scopes":[{"kind":"progress_digest","id":"project"},{"kind":"assignment","id":"project"},{"kind":"teamwork_signal","id":"project"},{"kind":"agent_profile","id":"project"}]},
 {"principal":"replica-b@e1","credential_ref":"replica-b.token",
  "scopes":[{"kind":"progress_digest","id":"project"},{"kind":"assignment","id":"project"},{"kind":"teamwork_signal","id":"project"},{"kind":"agent_profile","id":"project"}]}]}
JSON
chmod 600 "$hubdir/replicas.json"
"$WORK/mnemon-hub" --addr 127.0.0.1:9820 --store "$hubdir/hub.db" --replicas "$hubdir/replicas.json" >"$WORK/hub.log" 2>&1 &
HUBPID=$!
sleep 0.5

# ── A 节点: 排查 + 教义级交接 ───────────────────────────────
proja="$WORK/node-a"; mkdir -p "$proja"; cd "$proja"
"$WORK/mnemon-harness" setup --host codex --principal agent-a@e1 --control-url http://127.0.0.1:8820 >/dev/null
toka=".mnemon/harness/channel/credentials/agent-a-e1.token"
"$WORK/mnemon-harness" local run >"$WORK/node-a.log" 2>&1 &
NP=$!
for i in $(seq 1 60); do "$WORK/mnemon-harness" control status --addr http://127.0.0.1:8820 --principal agent-a@e1 --token-file "$toka" >/dev/null 2>&1 && break; sleep 0.1; done
cat > 排查文档.md <<'DOC'
# 支付回调对账延迟事故排查

## 根因
reconcile.window_hold_ms 被误设为 43200000(12 小时),应改回 30000。

## 干扰项
callback.retry_backoff_ms 现值合理,无需修改。请勿混淆两个参数。

## 复核要点
修复动作 = 将 reconcile.window_hold_ms 从 43200000 改回 30000。
DOC
# summary 不内联 needle(教义级交接: 内容走附件)
"$WORK/mnemon-harness" teamwork report --addr http://127.0.0.1:8820 --principal agent-a@e1 --token-file "$toka" \
	--outcome result --summary "对账延迟事故排查完成,根因与修复值见附件文档。" \
	--result "result: 修复参数与数值见附件排查文档。" \
	--attach 排查文档.md --external-id c1-report >/dev/null
"$WORK/mnemon-harness" teamwork assign --addr http://127.0.0.1:8820 --principal agent-a@e1 --token-file "$toka" \
	--assignee agent-b@e1 --scope payments/reconcile --ttl 2h \
	--work "复核对账延迟事故的 result:从附件排查文档确认修复参数名与应设数值。" \
	--feedback "报告参数名与数值" --evidence "排查文档附件" --external-id c1-assign >/dev/null
kill $NP 2>/dev/null; wait $NP 2>/dev/null || true
"$WORK/mnemon-harness" remote add hub --endpoint http://127.0.0.1:9820 --token-file "$hubdir/replica-a.token" --allow-insecure >/dev/null
"$WORK/mnemon-harness" push >/dev/null
cd - >/dev/null

# ── A 目录物理删除(真隔离) ─────────────────────────────────
rm -rf "$proja"
echo "A 节点目录已删除。"

# ── B 节点: 入界 + brief 断言 ───────────────────────────────
projb="$WORK/node-b"; mkdir -p "$projb"; cd "$projb"
"$WORK/mnemon-harness" setup --host codex --principal agent-b@e1 --control-url http://127.0.0.1:8821 >/dev/null
tokb=".mnemon/harness/channel/credentials/agent-b-e1.token"
"$WORK/mnemon-harness" remote add hub --endpoint http://127.0.0.1:9820 --token-file "$hubdir/replica-b.token" --allow-insecure >/dev/null
"$WORK/mnemon-harness" pull >/dev/null
"$WORK/mnemon-harness" local run >"$WORK/node-b.log" 2>&1 &
NP=$!
for i in $(seq 1 60); do "$WORK/mnemon-harness" control status --addr http://127.0.0.1:8821 --principal agent-b@e1 --token-file "$tokb" >/dev/null 2>&1 && break; sleep 0.1; done
brief="$("$WORK/mnemon-harness" view --addr http://127.0.0.1:8821 --principal agent-b@e1 --token-file "$tokb")"
echo "$brief" >"$WORK/brief-b.txt"
case "$brief" in *"[mnemon:handoff]"*) ;; *) echo "brief 缺 handoff 节:"; echo "$brief"; exit 1;; esac
case "$brief" in *"复核对账延迟事故"*) ;; *) echo "handoff 缺 expected_work:"; echo "$brief"; exit 1;; esac
case "$brief" in *"result"*) ;; *) echo "handoff 缺 result 字样:"; echo "$brief"; exit 1;; esac
case "$brief" in *".mnemon/harness/blobs/sha256/"*) ;; *) echo "handoff 缺本机 artifact 路径:"; echo "$brief"; exit 1;; esac
echo "brief 三断言绿(expected_work / result / artifact 本机路径)。"

# ── B 真实回合(oracle) ─────────────────────────────────────
prompt="你是 agent-b@e1,接手支付回调对账延迟事故。先运行: . .mnemon/harness/local/env.sh && \"$WORK/mnemon-harness\" view --addr http://127.0.0.1:8821 --principal agent-b@e1 --token-file \"$tokb\" 阅读交接简报;简报 handoff 节列出了附件文档的本机路径(.mnemon/harness/blobs/sha256/…),读取该文档后,只回答:修复参数名是什么?应设数值是多少?"
set +e
codex exec --cd "$projb" --dangerously-bypass-approvals-and-sandbox "$prompt" >"$WORK/turn-b.out" 2>"$WORK/turn-b.err"
rc=$?
set -e
kill $NP 2>/dev/null; wait $NP 2>/dev/null || true
cd - >/dev/null
echo "── B 回合输出(尾部)──"; tail -12 "$WORK/turn-b.out"
[ $rc -eq 0 ] || { echo "B 回合非零退出($rc)"; sed -n '1,10p' "$WORK/turn-b.err"; exit 1; }

# 机械 oracle
grep -q "window_hold_ms" "$WORK/turn-b.out" || { echo "oracle 失败: 输出缺 window_hold_ms"; exit 1; }
grep -q "30000" "$WORK/turn-b.out" || { echo "oracle 失败: 输出缺 30000"; exit 1; }
# 干扰项不得被当成答案(答案行不应把 retry_backoff 当修复参数)
if grep -q "修复参数.*retry_backoff\|retry_backoff.*应设" "$WORK/turn-b.out"; then
	echo "oracle 失败: 干扰项被误当修复参数"; exit 1
fi
# 零访问 A 机路径
if grep -q "node-a" "$WORK/turn-b.out" "$WORK/turn-b.err"; then
	echo "隔离失败: B 回合触及 A 机路径"; exit 1
fi
kill $HUBPID 2>/dev/null; wait $HUBPID 2>/dev/null || true
echo "r4 C1 真隔离交接 OK (oracle: window_hold_ms=30000; 零 A 路径访问)"
