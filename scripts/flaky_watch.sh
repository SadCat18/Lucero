#!/usr/bin/env bash
# flaky_watch.sh —— 把"间歇红"记成一条判据，而不是记成一句"刚才红了现在绿了"。
# 判据本体在契约② 判据表 FL（T-119，主控 15:46 定；本脚本按那张表实现，这里不另立口径）。
#
# 四个档位，两两对照才能把"并发形状"分辨出来（主控 15:55 补的那一维就是这里的 isolated）：
#   single     单独跑那一个包、一个用例                       —— 红 = 稳定复现，是回归不是 flaky
#   shuffle    同一测试二进制内 -count=2 -shuffle=on           —— 用例互相踩（同二进制并发）
#   isolated   多包一起跑但 `-p 1`（一次只编译/运行一个包）    —— 去掉"跨包并发"，保留包内 shuffle
#   parallel   多包一起跑、不限制 -p（`go test ./...` 的默认）  —— 跨包并发：抢 CPU/端口/文件锁
# 判决（FL-1/FL-2 的原样实现）：
#   single 红                        ⇒ REGRESSION，派归属角色
#   shuffle 红、single 绿            ⇒ FLAKY（同二进制并发）
#   parallel 红、isolated 绿         ⇒ FLAKY（跨包并发）—— 只有这一种能怪"一起跑"
#   parallel 红、isolated 也红       ⇒ FLAKY（未定维度）—— 别急着怪跨包，包内共享状态也能造出来
#   四档都不红                       ⇒ 未复现：报采样时刻与次数，**不写成"已绿"**
#   任一档 探测失败（编译不过 / 用例名不存在） ⇒ 探测不成立，exit 2 —— 不许在坏探针上下结论
# 每次 go test 之前都取一次 date：没有时刻的采样数字不算证据（T-43）。
#
# 用法：bash scripts/flaky_watch.sh [-run 用例名] [-p 主包] [-n 次数] [一起跑的包...]
# Exit：0 = 已分类（flaky / 未复现）；1 = 判成回归；2 = 探测本身不成立
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# go.mod 在 core/ 下，所以包参数必须相对那一层，否则 ./cmd/core/ 会指到仓库根的空位置、
# 结果是"编译失败"被误读成"用例不存在"。（第一版就栽在这里。）
MOD="$ROOT/core"
cd "$MOD" || { printf '进不去 Go 模块目录 %s\n' "$MOD" >&2; exit 2; }
GO="${LUCERO_GO:-D:/DevTools/go/bin/go.exe}"
[[ -x "$GO" ]] || GO=go

# --selftest 要在 getopts 之前摘掉（它不是一个选项参数）
SELFTEST=0
# --selftest 必须在 getopts 之前摘掉：它不是选项参数，留着会让 getopts 报 illegal option，
# 而那时 SELFTEST_RUN 还没赋值，`set -u` 直接把这个"能证明分类器不是永真"的分支打死。
SELFTEST_RUN=0
if [[ "${1:-}" == "--selftest" ]]; then SELFTEST_RUN=1; shift; fi

RE=TestSealCoversTheBytesThatLand   # -run 是前缀匹配，所以名字要写全（FL 表头钉的就是这条）
MAIN=./cmd/core/
N=2
AT=""        # 只核对时间线时用：失败时刻（ISO）
SLACK=120    # 往前看多少秒；往后固定看 90 秒（还原也可能发生在失败之后几秒）
while getopts "r:p:n:a:s:h" o; do
  case "$o" in
    r) RE="$OPTARG" ;;
    p) MAIN="$OPTARG" ;;
    n) N="$OPTARG" ;;
    a) AT="$OPTARG" ;;
    s) SLACK="$OPTARG" ;;
    h) printf '用法: %s [-run 用例名] [-p 主包] [-n 次数] [一起跑的包...]\n     %s -a <失败时刻ISO> [-s 前看秒数] [要查的目录...]\n' "$0" "$0"; exit 0 ;;
  esac
done
shift $((OPTIND - 1))
PEERS=("$@")
[[ ${#PEERS[@]} -eq 0 ]] && PEERS=(./test/ ./api/http/)
# -at 模式下要查的目录：**默认全仓扫**（core/ + specs/ + scripts/）。
# 理由不是谨慎，是我自己撞的教训：15:58 那次我只看了被验包和它 import 的包，判"不是变异窗口"，
# 而真正在改写的是 `internal/adapter` —— 别的角色在写别的包。**只看被验包一定会漏。**
TL_DIRS=()
for p in "$MAIN" "${PEERS[@]}"; do TL_DIRS+=("$(printf '%s' "$p" | sed -e 's#^\./##' -e 's#/$##')"); done
[[ -n "$AT" ]] && TL_DIRS=(../core ../specs ../scripts)
[[ -n "$AT" ]] && [[ ${#@} -gt 0 ]] && TL_DIRS=("$@")

export CGO_ENABLED=0
[[ -n "${LUCERO_TEST_FAKE_ADAPTER:-}" ]] || export LUCERO_TEST_FAKE_ADAPTER=1

# 一次采样。判的是那一行 `--- PASS/FAIL/SKIP: <用例>`，**不判包级的 ok/FAIL** ——
# 不带 -v 时包照样报 ok、SKIP 看不见，这正是 FL 表头写着的那个假绿现场。
probe() {  # probe <档位名> <go test 参数...>
  local label="$1"; shift
  local stamp out rc mark reason
  stamp="$(date -Iseconds)"
  out="$("$GO" test "$@" 2>&1)"; rc=$?
  mark="$(printf '%s\n' "$out" | grep -E "^[[:space:]]*--- (PASS|FAIL|SKIP): ${RE}([[:space:]]|$)" | tail -1 \
          | sed -E 's/^[[:space:]]*--- ([A-Z]+).*/\1/')"
  if [[ -z "$mark" ]]; then
    mark=BROKEN
    reason="$(printf '%s\n' "$out" | grep -E '\.go:[0-9]+:[[:space:]]+(undefined|.*expected|.*cannot|.*no required)|^# |no such file|build failed' | head -2 | tr '\n' ' ')"
    [[ -z "$reason" ]] && reason="输出里没有这个用例的任何 --- 行（用例改名了？-run 前缀写错了？）"
  fi
  printf '  %s  %-9s %-7s 包退出码=%s%s\n' "$stamp" "$label" "$mark" "$rc" "${reason:+  ← ${reason:0:120}}"
  case "$mark" in
    PASS)   eval "${label}_GREEN=\$(( ${label}_GREEN + 1 ))" ;;
    FAIL)   eval "${label}_RED=\$(( ${label}_RED + 1 ))" ;;
    SKIP)   eval "${label}_SKIP=\$(( ${label}_SKIP + 1 ))" ;;
    *)      eval "${label}_BROKEN=\$(( ${label}_BROKEN + 1 ))" ;;
  esac
}

run_mode() {  # run_mode <档位名> <次数> <go test 参数...>
  local label="$1" n="$2"; shift 2
  local i
  eval "${label}_GREEN=0; ${label}_RED=0; ${label}_SKIP=0; ${label}_BROKEN=0"
  for (( i = 1; i <= n; i++ )); do probe "$label" "$@"; done
  eval "printf '  %-9s 小计：PASS=%s FAIL=%s SKIP=%s 探测失败=%s\n\n' \"\$label\" \
    \"\${${label}_GREEN}\" \"\${${label}_RED}\" \"\${${label}_SKIP}\" \"\${${label}_BROKEN}\""
}

# 时间线前置（T-121 主控 16:0x 要的那一步：记 flaky 之前先对 mtime 与失败时刻）。
# 一次"复现窗口"只有在**期间没有任何相关文件被改写**时才算数；否则你采的是别人的半成品，
# 既不能记 flaky 也不能记已修。我自己 15:58 那次就撞上过：`internal/adapter` 报 setup failed，
# 而那个文件的 mtime 正好落在我那次跑的中间，14 秒后 go vet 就 0 了 —— 那是在制品，不是 flaky。
timeline() {  # timeline <窗口起点 ISO> <窗口终点 ISO> <要看的包目录...>
  local from="$1" to="$2"; shift 2
  local f stamp dirty=0
  printf '时间线核对（复现窗口 %s → %s 内不许有相关文件被改写）：\n' "$from" "$to"
  for f in "$@"; do
    while IFS= read -r line; do
      stamp="${line%% *}"
      if [[ "$stamp" > "$from" && "$stamp" < "$to" ]]; then
        printf '      采样期间被改写：%s  mtime=%s  ← 这次采样不算复现窗口\n' "${line#* }" "$stamp"
        dirty=$((dirty + 1))
      fi
    done < <(find "$f" -name '*.go' -newermt "${from}" ! -newermt "${to}" -printf '%TY-%Tm-%TdTH:%TM:%TS %p\n' 2>/dev/null | cut -c1-16,33-)
  done
  (( dirty == 0 )) && printf '      干净：窗口内没有相关文件被改写（这一条采样可用）\n'
  return 0
}

# T-121 前置核对（`-at <失败时刻>`）：**记 flaky 之前**先看这次观察落没落在别人的变异窗口里。
# 规则很简单：失败时刻往前的 ±SLACK 秒内，只要有一个相关文件被改写过，这次观察就不能当证据——
# 采到的是半成品，既不是 flaky 也不是回归。（15:29:31 那次红就是这个形状：backend 第 123 条自陈
# 变异电池有两次把 `cmd/core` 写成编不过，还原时间 15:30:29，正好把那次"唯一红"包在窗口里。）
if [[ -n "${AT:-}" ]]; then
  printf '时间线核对（只看，不跑测试）：失败时刻=%s  容差=前 %s 秒 / 后 90 秒  扫描范围=%s\n' "$AT" "$SLACK" "${TL_DIRS[*]}"
  at_epoch="$(date -d "$AT" +%s 2>/dev/null)" || { printf '  时刻解析失败：%s\n' "$AT"; exit 2; }
  dirty=0; scanned=0
  for dir in "${TL_DIRS[@]}"; do
    while IFS= read -r line; do
      m_epoch="${line%% *}"; path="${line#* }"
      scanned=$((scanned + 1))
      delta=$(( (at_epoch - m_epoch) ))
      if (( delta > -90 && delta < SLACK )); then
        printf '  相关：%-52s mtime=%s（失败%s %ss 被改写）← 这次观察不算证据\n' "$path" \
          "$(date -d "@$m_epoch" -Iseconds)" "$( (( delta > 0 )) && echo 前 || echo 后 )" "$(( delta > 0 ? delta : -delta ))"
        dirty=$((dirty + 1))
      fi
    done < <(find "$dir" -name '*.go' -printf '%T@ %p\n' 2>/dev/null | awk '{printf "%d %s\n", $1, $2}')
  done
  if (( dirty == 0 )); then
    printf '  干净：扫了 %s 个 .go 文件，窗口内没有一个被改写 ⇒ 这次红可以作为 flaky/回归的候选继续查\n' "$scanned"
    exit 0
  fi
  printf '\n  扫了 %s 个 .go 文件，其中 %s 个落在改写窗口里 ⇒ 观察不算证据，**既不计 flaky 也不计回归**；\n' "$scanned" "$dirty"
  printf '  先请改动方交"还原之后"的复采（带时刻），再决定这条要不要留档。\n'
  exit 0
fi

# classify 读四个档的 RED/GREEN 计数，吐出口径 + 退出码。
# 单独成函数只为了让 --selftest 能喂合成计数验它五句判决都能落地 —— 一个只会印"未复现"的分类器
# 等于没有分类器（这条判据的绿红两边都在这儿）。
classify() {
  local total_broken=$(( single_BROKEN + shuffle_BROKEN + isolated_BROKEN + parallel_BROKEN ))
  if (( total_broken > 0 )); then
    printf '判决：探测不成立 —— 有 %s 次采样没拿到该用例的 --- 行（多半是别人的包此刻编不过）。\n' "$total_broken"
    printf '      这种时候既不能报 flaky 也不能报绿；先按 T-68 查被验文件的 mtime 分清"在制品"与"回归"。\n'
    return 2
  fi
  if (( single_RED > 0 )); then
    printf '判决：REGRESSION —— 单独跑也红（%s/%s）。这是稳定复现，派归属角色，不许标成 flaky。\n' "$single_RED" "$SAMPLES"
    return 1
  fi
  if (( shuffle_RED > 0 )); then
    printf '判决：FLAKY（同二进制并发）—— shuffle 红 %s 次而 single 全绿；不计绿也不计回归。\n' "$shuffle_RED"
    return 0
  fi
  if (( parallel_RED > 0 )); then
    if (( isolated_RED == 0 )); then
      printf '判决：FLAKY（跨包并发）—— 只有不限 -p 的那档红 %s/%s 次；-p 1 全绿 ⇒ 才是"一起跑"造成的。\n' "$parallel_RED" "$SAMPLES"
    else
      printf '判决：FLAKY（未定维度）—— parallel 与 isolated 都红（%s/%s 与 %s/%s），\n' "$parallel_RED" "$SAMPLES" "$isolated_RED" "$SAMPLES"
      printf '      说明不是跨包并发：同一批包里共享的东西（见上面的枚举）才是方向，别急着怪 -p。\n'
    fi
    return 0
  fi
  printf '判决：未复现 —— 四档各 %s 次都没红。**这不等于已绿**（主控 15:46 的口径）；\n' "$SAMPLES"
  printf '      要判成"已修"需要归属角色交出带时刻的复采，或这条判据换成能稳定复现的形状。\n'
  return 0
}

# 反向用例：喂五组合成计数，判决必须各不相同。跑这个不需要等真的红一次。
if (( SELFTEST_RUN )); then
  fails=0
  check_verdict() {  # check_verdict <期望关键字> <s RED> <sh RED> <is RED> <pa RED> <broken>
    local want="$1"
    single_RED=$2; shuffle_RED=$3; isolated_RED=$4; parallel_RED=$5
    single_BROKEN=$6; shuffle_BROKEN=0; isolated_BROKEN=0; parallel_BROKEN=0
    local got; got="$(classify | head -1)"
    if [[ "$got" != *"$want"* ]]; then
      printf '  [SELFTEST FAIL] 计数(%s,%s,%s,%s,b=%s) 判成「%s」，期望含「%s」\n' "$2" "$3" "$4" "$5" "$6" "$got" "$want"
      fails=$((fails + 1))
    else
      printf '  [ok] %-22s ← %s\n' "$want" "${got:0:78}"
    fi
  }
  SAMPLES=3
  check_verdict REGRESSION        1 0 0 0 0
  check_verdict "FLAKY（同二进制并发）" 0 1 0 0 0
  check_verdict "FLAKY（跨包并发）"   0 0 0 1 0
  check_verdict "FLAKY（未定维度）"   0 0 1 1 0
  check_verdict 未复现              0 0 0 0 0
  check_verdict 探测不成立           0 0 0 0 1
  (( fails == 0 )) && printf '\n  [SELFTEST OK] 五句判决各就各位（分类器不是永真式）\n'
  exit $(( fails > 0 ? 2 : 0 ))
fi

SAMPLES="$N"
RUN_FROM="$(date -Iseconds)"
printf 'flaky_watch  用例=%s  主包=%s  每档 %s 次  并发跑的包：%s  窗口起点=%s\n\n' "$RE" "$MAIN" "$N" "${PEERS[*]}" "$RUN_FROM"

run_mode single   "$N" -count=1 -v -run "$RE" "$MAIN"
run_mode shuffle  "$N" -count=2 -shuffle=on -v -run "$RE" "$MAIN"
run_mode isolated "$N" -count=1 -shuffle=on -p 1 -v -run "$RE" "$MAIN" "${PEERS[@]}"
run_mode parallel "$N" -count=2 -shuffle=on -v -run "$RE" "$MAIN" "${PEERS[@]}"
timeline "$RUN_FROM" "$(date -Iseconds)" cmd/core internal/adapter

# 共享面枚举（判据要求"可枚举的几个"，不许一个一个猜）。
# 这里只报机器能判的两件事：① 有没有测试构造 Config 却没给 DataDir（那才是真跨包冲突源）；
# ② 那三个文件名各自住在哪儿。注意 backend 不是没枚举过：`.qoder/progress/backend.md` 第 110 条
# （约 2242~2258 行）就记着 `<TempDir>/audit.head` 这条链，只是它没写成一张四条的表。
printf '共享面枚举（真冲突候选 = 落在进程之间共享的地方，不是 t.TempDir() 里）：\n'
for art in core.lock core.endpoint audit.head; do
  printf '  %-16s 引用处（非测试）：\n' "$art"
  grep -rn "$art" cmd/core/*.go internal/adapter/*.go 2>/dev/null | grep -v '_test\.go' | sed 's/^/      /' | head -2
done
printf '  没用 t.TempDir() 的 cmd/core 测试（DataDir 计数少于 Config{ 计数 = 有测试吃了默认目录）：\n'
for f in cmd/core/*_test.go; do
  a="$(grep -c 'Config{' "$f")"; b="$(grep -c 'DataDir:' "$f")"
  t="$(grep -c 't.TempDir()' "$f")"
  if (( a > b )); then printf '      %-40s Config{=%s DataDir:=%s TempDir=%s  ← 需人工看\n' "$f" "$a" "$b" "$t"; fi
  (( a == 0 )) && printf '      %-40s 没构造 Config（TempDir=%s）\n' "$f" "$t"
done
printf '\n'

classify
exit $?
