#!/usr/bin/env bash
# flaky_watch.sh —— 把"间歇红"记成一条判据，而不是记成一句"刚才红了现在绿了"。
# 判据本体在契约② 判据表 FL（T-119，主控 15:46 定；本脚本按那张表实现，这里不另立口径）。
#
# 四个档位，两两对照才能把"并发形状"分辨出来（主控 15:55 补的那一维就是这里的 isolated）：
#   -m all（默认）跑四档；-m single 只跑一档 —— 串行就能复现的故障不需要付 4× 成本（05:32 实测：-n 50 四档连跑 10 分钟没跑完）
#   single     单独跑那一个包、一个用例                       —— 红 = 稳定复现，是回归不是 flaky#   shuffle    同一测试二进制内 -count=2 -shuffle=on           —— 用例互相踩（同二进制并发）
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
# 用法：bash scripts/flaky_watch.sh [-r 用例名] [-p 主包] [-n 次数] [一起跑的包...]
#        **是 -r，不是 go test 的 -run**：getopts 不认长选项，`-run TestX` 会被拆成
#        `-r un` + 位置参数 TestX ⇒ 用例名变成 "un"、-p/-n 全掉进包列表（05:26 主控实测、
#        我 05:30:58 在本脚本上复现：打印出 `用例=un 主包=./cmd/core/`）。现在直接拒跑。
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

RE=TestSealCoversTheBytesThatLand   # 交给 go test 的 -run 是无锚定子串匹配，所以名字要写全（FL 表头钉的就是这条）
MAIN=./cmd/core/
N=2
AT=""        # 只核对时间线时用：失败时刻（ISO）
SLACK=120    # 往前看多少秒；往后固定看 90 秒（还原也可能发生在失败之后几秒）
# 参数面守卫：`-run` 是 go test 的旗标，不是本脚本的。getopts 见 `-run TestX` 会把它读成
# `-r` + 值 "un"，剩下 TestX 与后面的 -p/-n 全落进位置参数 —— **症状是"探测不成立"而不是报错**，
# 于是抄错的人只看到一句"没抓到 --- 行"，永远拿不到读数（契约① §1.1 第 19 条登记的就是这件事）。
# 宁可拒跑，也不要给出一个看起来像采样的空跑。
for a in "$@"; do
  case "$a" in
    -run|-run=*) printf '%s: `-run` 是 go test 的旗标；本脚本收 `-r 用例名`（getopts 不认长选项，\n                 写 -run 会被拆成 `-r un` + 一堆位置参数，读数全是假的）\n' "$0" >&2; exit 2 ;;
  esac
done

# MODES 的默认值必须落在 getopts **之前**：放后面会把 `-m xxx` 解析出来的值盖掉，
#   于是 `-m single` 照样四档连跑（05:49:31 实测：过滤没生效、四档全跑，判决还说"跨包并发"）。
MODES=all   # -m single / -m single,shuffle / -m all（默认全跑，保持老行为）
while getopts "r:p:n:a:s:hm:" o; do
  case "$o" in
    r) RE="$OPTARG" ;;
    p) MAIN="$OPTARG" ;;
    n) N="$OPTARG" ;;
    a) AT="$OPTARG" ;;
    s) SLACK="$OPTARG" ;;
    m) MODES="$OPTARG" ;;
    h) printf '用法: %s [-r 用例名] [-p 主包] [-n 次数] [一起跑的包...]\n     %s -a <失败时刻ISO> [-s 前看秒数] [要查的目录...]\n     （用例子串只认 -r；写 -run 会被拒绝，因为 getopts 不认长选项）
     -m single|shuffle|isolated|parallel|all（默认 all）：串行可复现的故障只跑一档，别付 4× 成本\n' "$0" "$0"; exit 0 ;;
  esac
done
shift $((OPTIND - 1))
# 契约① §1.1 第 15 条：取证命令要"锚定 + 显式包路径"。`-r` 收的是**用例名**不是正则，这里另算一份
# 带 ^…$ 的 RUNRE 交给 go test；**RE 本身保持原样**，因为下面 probe 要拿它去匹配
# `--- PASS: <名字>` 行（把锚塞进那个模式里会让每一次采样都判成"探测不成立"）。
# 锚定不影响子用例：Go 按 `/` 分段匹配，第一段整名命中后子用例照常全跑（09-23 00:12:22 实测）。
# 要跑一族请直接把命令交给 go test（本工具的判决依赖"这一条用例的 --- 行"，多命中会把它自己搞糊涂）。
RUNRE='^'"$RE"'$'
# -timeout 必须带上：09-23 02:43 那趟并发包里 cmd/core 挂到 Go 看门狗 10 分钟，
#   四档 ×3 会被一个别人的挂死拖成 40 分钟；180s 之内拿不到结论就判"探测不成立"，
#   判决交给栈而不是交给等待。
TMO="${LUCERO_FLAKY_TIMEOUT:-180s}"
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
    [[ -z "$reason" ]] && reason="输出里没有这个用例的任何 --- 行（用例改名了？还是把 -r 写成了 -run？）"
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
  # 每一档的计数都可能**压根没被赋值**（`-m single` 就只跑一档），所以全部走 ${x:-0}：
  # 05:50:39 实测旧写法在 `set -u` 下直接 `shuffle_BROKEN: unbound variable` 崩掉，
  # 屏幕上没有判决、退出码 1 —— 一个"看起来像红"的崩溃比漏判更坏，因为它会被读成结论。
  single_RED=${single_RED:-0}; single_BROKEN=${single_BROKEN:-0}
  shuffle_RED=${shuffle_RED:-0}; shuffle_BROKEN=${shuffle_BROKEN:-0}
  isolated_RED=${isolated_RED:-0}; isolated_BROKEN=${isolated_BROKEN:-0}
  parallel_RED=${parallel_RED:-0}; parallel_BROKEN=${parallel_BROKEN:-0}
  SAMPLES=${SAMPLES:-1}
  local total_broken=$(( single_BROKEN + shuffle_BROKEN + isolated_BROKEN + parallel_BROKEN ))
  if (( total_broken > 0 )); then
    printf '判决：探测不成立 —— 有 %s 次采样没拿到该用例的 --- 行（多半是别人的包此刻编不过）。\n' "$total_broken"
    printf '      这种时候既不能报 flaky 也不能报绿；先按 T-68 查被验文件的 mtime 分清"在制品"与"回归"。\n'
    return 2
  fi
  if (( single_RED > 0 )); then
    # 「single 档红过」只说明**与并发形状无关**，不说明稳定复现：发生率 p≈6% 的事件在 n=6 里
    # 出现一次的概率就有 ~30%（05:45:19 实测 5 绿 1 红）。把 1/6 印成"稳定复现"会把人派去修一个
    # 不可复现的 bug，所以按 N/N 与 x<N 分两句话。
    if (( single_RED == SAMPLES )); then
      printf '判决：REGRESSION（稳定复现 %s/%s）—— single 档每次都红，派归属角色，不许标成 flaky。\n' "$single_RED" "$SAMPLES"
    else
      printf '判决：REGRESSION（间歇，single 档 %s/%s ≈ %s%%）—— 与并发形状无关（单独跑就红过），\n' \
        "$single_RED" "$SAMPLES" "$(( 100 * single_RED / SAMPLES ))"
      printf '      但**不是"稳定复现"**：按发生率与归属记档，复采次数要配得上这个量级（§1.1 第 19 条）；\n'
      printf '      也别反过来把它记成 flaky —— 单进程串行就红，说明它不是"跑在一起"造成的。\n'
    fi
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
  # 措辞必须跟着 `-m` 走：只跑了一档却说"四档都没红"，就是拿一档的事实说四档的话。
  printf '判决：未复现 —— 实跑档 [%s] 各 %s 次都没红。**这不等于已绿**（15:46 的口径；\n' "${MODES:-all}" "$SAMPLES"
  printf '      档数与次数没配得上事件量级时，这一句只表示"本次没采到"，不表示"没有故障"）；\n'
  printf '      要判成"已修"需要归属角色交出带时刻的复采，或这条判据换成能稳定复现的形状。\n'
  return 0
}

# 反向用例：喂五组合成计数，判决必须各不相同。跑这个不需要等真的红一次。
if (( SELFTEST_RUN )); then
  fails=0
  SELFTEST_CASES=0   # 实际断言条数；收尾那句不许再手写"五句"（05:53 时它已经过期）
  check_verdict() {  # check_verdict <期望关键字> <s RED> <sh RED> <is RED> <pa RED> <broken>
    local want="$1"
    SELFTEST_CASES=$((SELFTEST_CASES + 1))
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
  check_verdict "REGRESSION（稳定复现 3/3）" 3 0 0 0 0
  check_verdict "REGRESSION（间歇"      1 0 0 0 0
  # 反面对：部分红里出现"稳定复现"就是我在撒谎，所以这一条不许含那个词。
  partial=$(SAMPLES=3; single_RED=1; shuffle_RED=0; isolated_RED=0; parallel_RED=0; \
            single_BROKEN=0; classify | head -1)
  SELFTEST_CASES=$((SELFTEST_CASES + 1))
  case "$partial" in
    *稳定复现*) printf '  [SELFTEST FAIL] 1/3 的样本被判成"稳定复现"：%s\n' "$partial"; fails=$((fails + 1)) ;;
    *) printf '  [ok] 间歇不误标稳定复现     ← %s\n' "${partial:0:78}" ;;
  esac
  check_verdict "FLAKY（同二进制并发）" 0 1 0 0 0
  check_verdict "FLAKY（跨包并发）"   0 0 0 1 0
  check_verdict "FLAKY（未定维度）"   0 0 1 1 0
  check_verdict 未复现              0 0 0 0 0
  check_verdict 探测不成立           0 0 0 0 1
  # `-m single` 的形状：另外三档连变量都不存在。旧写法在这里直接崩（unbound variable），
  # 现在必须照样给出判决。
  onlyone=$(unset shuffle_RED isolated_RED parallel_RED shuffle_BROKEN isolated_BROKEN parallel_BROKEN; \
            MODES=single SAMPLES=2 single_RED=0 single_BROKEN=0; classify | head -1)
  SELFTEST_CASES=$((SELFTEST_CASES + 1))
  case "$onlyone" in
    *未复现*) printf '  [ok] 只跑一档也有判决     ← %s\n' "${onlyone:0:70}" ;;
    *) printf '  [SELFTEST FAIL] 只跑一档时没拿到判决：%s\n' "$onlyone"; fails=$((fails + 1)) ;;
  esac
  (( fails == 0 )) && printf '
  [SELFTEST OK] %s 条断言各就各位（分类器不是永真式；条数由自增算出，不手写）
' "$SELFTEST_CASES"
  exit $(( fails > 0 ? 2 : 0 ))
fi

SAMPLES="$N"
RUN_FROM="$(date -Iseconds)"
printf 'flaky_watch  用例=%s  主包=%s  每档 %s 次  并发跑的包：%s  窗口起点=%s\n\n' "$RE" "$MAIN" "$N" "${PEERS[*]}" "$RUN_FROM"

want_mode() { [[ "$MODES" == all || ",${MODES// /}," == *",$1,"* ]]; }

if want_mode single; then
  run_mode single   "$N" -count=1 -v -timeout "$TMO" -run "$RUNRE" "$MAIN"
fi
if want_mode shuffle; then
  run_mode shuffle  "$N" -count=2 -shuffle=on -v -timeout "$TMO" -run "$RUNRE" "$MAIN"
fi
if want_mode isolated; then
  run_mode isolated "$N" -count=1 -shuffle=on -p 1 -v -timeout "$TMO" -run "$RUNRE" "$MAIN" "${PEERS[@]}"
fi
if want_mode parallel; then
  run_mode parallel "$N" -count=2 -shuffle=on -v -timeout "$TMO" -run "$RUNRE" "$MAIN" "${PEERS[@]}"
fi
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
printf '  cmd/core 测试里 Config/DataDir/TempDir 三个计数（**只证形状、不证结论**）：\n'
for f in cmd/core/*_test.go; do
  a="$(grep -c 'Config{' "$f")"; b="$(grep -c 'DataDir:' "$f")"
  t="$(grep -c 't.TempDir()' "$f")"
  # a>b 才是真候选：构造了 Config 却没给 DataDir ⇒ 吃 defaultDataDir()，那才是跨包抢锁的形状。
  if (( a > b )); then printf '      %-40s Config{=%s DataDir:=%s TempDir=%s  ← 需人工看（构造了 Config 却没给 DataDir）\n' "$f" "$a" "$b" "$t"; fi
  # a==0 不算证据：这个包可能用 helper 或小写结构体构造配置，"文件里没有 Config{ 字面量"
  # 与"有测试吃了默认目录"是两回事。上一版把这行印成后者，差点对 backend 报一条不存在的风险。
  (( a == 0 )) && printf '      %-40s 无 Config{ 字面量（TempDir=%s）—— 不构成证据，只是这一档扫不到\n' "$f" "$t"
done
printf '\n'

classify
exit $?
