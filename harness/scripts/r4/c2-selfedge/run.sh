#!/usr/bin/env bash
# C2 中文语料多设备(self-edge): 同一 principal 两台“设备”(两目录),
# 设备 1 十二篇中文文档(新增/修改/未变)经 hub 联邦到设备 2;
# 断言: 未变文档零字节重传(按址命中)· 设备 2 recall「对账窗口」命中 ·
# 无 clamp/authz 拒绝记录(治理退化为完整性校验)。
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
WORK="$(mktemp -d)"
cleanup() { pkill -f "$WORK" 2>/dev/null || true; rm -rf "$WORK"; }
trap cleanup EXIT

go build -o "$WORK/mnemon-harness" ./harness/cmd/mnemon-harness
go build -o "$WORK/mnemon-hub" ./harness/cmd/mnemon-hub

hubdir="$WORK/hub"; mkdir -p "$hubdir"
printf '%s\n' "aaaa1111bbbb2222cccc3333dddd4444eeee5555ffff6666" >"$hubdir/dev.token"
chmod 600 "$hubdir/dev.token"
cat >"$hubdir/replicas.json" <<'JSON'
{"schema_version":1,"replicas":[
 {"principal":"grivn@devices","credential_ref":"dev.token",
  "scopes":[{"kind":"progress_digest","id":"project"}]}]}
JSON
chmod 600 "$hubdir/replicas.json"
"$WORK/mnemon-hub" --addr 127.0.0.1:9810 --store "$hubdir/hub.db" --replicas "$hubdir/replicas.json" >"$WORK/hub.log" 2>&1 &
HUBPID=$!
sleep 0.5
BASE="http://127.0.0.1:9810"

# 设备 1: 12 篇文档(1-8 未变基线, 9-10 将修改, 11-12 新增于第二轮)
dev1="$WORK/dev1"; mkdir -p "$dev1/docs"; cd "$dev1"
"$WORK/mnemon-harness" setup --host codex --principal grivn@dev1 --control-url http://127.0.0.1:8810 >/dev/null
tok=".mnemon/harness/channel/credentials/grivn-dev1.token"
"$WORK/mnemon-harness" local run >"$WORK/dev1.log" 2>&1 &
NODEPID=$!
for i in $(seq 1 60); do "$WORK/mnemon-harness" control status --addr http://127.0.0.1:8810 --principal grivn@dev1 --token-file "$tok" >/dev/null 2>&1 && break; sleep 0.1; done
for n in $(seq 1 10); do
	printf '# 设计文档 %02d\n\n对账窗口保持时间与回调重试参数的推演,第 %d 稿。\n' "$n" "$n" >"docs/设计-$n.md"
	"$WORK/mnemon-harness" teamwork report --addr http://127.0.0.1:8810 --principal grivn@dev1 --token-file "$tok" \
		--outcome result --summary "设计文档 $n:对账窗口推演" --attach "docs/设计-$n.md" --external-id "c2-doc-$n" >/dev/null
done
kill "$NODEPID" 2>/dev/null; wait "$NODEPID" 2>/dev/null || true
"$WORK/mnemon-harness" remote add hub --endpoint "$BASE" --token-file "$hubdir/dev.token" --allow-insecure >/dev/null
"$WORK/mnemon-harness" push >/dev/null
blobs_round1=$(grep -c "verb=blobs.put result=ok" "$WORK/hub.log" || true)
[ "$blobs_round1" = "10" ] || { echo "round1 blob writes = $blobs_round1, want 10"; exit 1; }

# 第二轮: 修改 9-10, 新增 11-12, 1-8 原样重报(内容未变→按址命中零重传)
"$WORK/mnemon-harness" local run >"$WORK/dev1b.log" 2>&1 &
NODEPID=$!
for i in $(seq 1 60); do "$WORK/mnemon-harness" control status --addr http://127.0.0.1:8810 --principal grivn@dev1 --token-file "$tok" >/dev/null 2>&1 && break; sleep 0.1; done
printf '# 设计文档 09(修订)\n\n对账窗口保持时间改回 30000,修订稿。\n' >"docs/设计-9.md"
printf '# 设计文档 10(修订)\n\n回调重试参数复核,修订稿。\n' >"docs/设计-10.md"
printf '# 设计文档 11\n\n新增:多设备清单封装设计。\n' >"docs/设计-11.md"
printf '# 设计文档 12\n\n新增:blob 按址传输核对。\n' >"docs/设计-12.md"
for n in $(seq 1 12); do
	"$WORK/mnemon-harness" teamwork report --addr http://127.0.0.1:8810 --principal grivn@dev1 --token-file "$tok" \
		--outcome result --summary "设计文档 $n:对账窗口推演(第二轮)" --attach "docs/设计-$n.md" --external-id "c2r2-doc-$n" >/dev/null
done
kill "$NODEPID" 2>/dev/null; wait "$NODEPID" 2>/dev/null || true
"$WORK/mnemon-harness" push >/dev/null
blobs_round2=$(grep -c "verb=blobs.put result=ok" "$WORK/hub.log" || true)
# 第二轮只有 4 个新 digest(9/10 修订 + 11/12 新增);1-8 未变 → 204 无审计行
[ "$blobs_round2" = "14" ] || { echo "after round2 blob writes = $blobs_round2, want 14 (10+4 增量)"; exit 1; }
cd - >/dev/null

# 设备 2: 入界物化 + recall 命中
dev2="$WORK/dev2"; mkdir -p "$dev2"; cd "$dev2"
"$WORK/mnemon-harness" setup --host codex --principal grivn@dev2 --control-url http://127.0.0.1:8811 >/dev/null
tok2=".mnemon/harness/channel/credentials/grivn-dev2.token"
"$WORK/mnemon-harness" remote add hub --endpoint "$BASE" --token-file "$hubdir/dev.token" --allow-insecure >/dev/null
"$WORK/mnemon-harness" pull >/dev/null
"$WORK/mnemon-harness" local run >"$WORK/dev2.log" 2>&1 &
NODEPID=$!
for i in $(seq 1 60); do "$WORK/mnemon-harness" control status --addr http://127.0.0.1:8811 --principal grivn@dev2 --token-file "$tok2" >/dev/null 2>&1 && break; sleep 0.1; done
hit="$("$WORK/mnemon-harness" recall "对账窗口" --addr http://127.0.0.1:8811 --principal grivn@dev2 --token-file "$tok2")"
case "$hit" in *"对账窗口"*) ;; *) echo "recall 未命中: $hit"; exit 1;; esac
kill "$NODEPID" 2>/dev/null; wait "$NODEPID" 2>/dev/null || true
# 本地 blob store 物化(内容真的过界)
ls .mnemon/harness/blobs/sha256/ | wc -l | grep -q "14" || { echo "设备 2 blob 物化数 != 14"; exit 1; }
cd - >/dev/null

# 治理退化确认: 无 clamp/authz 拒绝
if grep -q "result=denied\|capsules.clamp\|result=rejected" "$WORK/hub.log"; then
	echo "self-edge 不应出现 clamp/authz 拒绝:"; grep "denied\|clamp\|rejected" "$WORK/hub.log"; exit 1
fi
kill "$HUBPID" 2>/dev/null; wait "$HUBPID" 2>/dev/null || true
echo "r4 C2 self-edge OK (增量 blob=4/12, recall 命中, 零拒绝)"
