#!/usr/bin/env bash
# Lucero ownership / dependency-direction lint.
# Authority: specs/api-contract-v0.1.md §7 — **文件所有权矩阵的正式版**（下面的 owner_of 表
#            就是 §7 的逐行转录；orchestration.md 的角色表已降为操作摘要，冲突时以 §7 为准）。
#            另用 specs/contracts-internal-v0.1.md §0 + §1.1 + §3 rule 1、event-contract §9、
#            api-contract §6.3（三处同步）。
# Owner: test role. Read-only over other roles' code; never edits what it flags.
#
# 基线与"排除主控自己的提交"（主控 17:2x 明确要求，漏了就会天天误报）：
#   基线 = 900038d（Wave-0）。本机所有会话共用一个 git 身份，**author 过滤无效**，
#   所以判据改成结构性的：worker 没有 git 写权限 ⇒ 「未提交的改动」必然是 worker 的，
#   「基线之后已提交的」必然是主控落的盘（51afde6/f007372/…的文档与依赖提交属正常）。
#   → 越界只按未提交集合判；已提交集合只用于 ①冻结件检查 ②归属分布 INFO。
#
# Usage:  bash scripts/check_ownership.sh [--selftest] [<baseline-sha>]
# Exit:   0 clean, 1 violations found, 2 the lint itself is broken.

set -uo pipefail

BASELINE="${BASELINE:-900038d}"
SELFTEST=0
for a in "$@"; do
  case "$a" in
    --selftest) SELFTEST=1 ;;
    --baseline) shift ;;
    *) [[ "$a" =~ ^[0-9a-f]{7,40}$ ]] && BASELINE="$a" ;;
  esac
done

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 2

# 前置检查（T-25 附带教训：脚本里的路径要么绝对、要么显式从仓库根拼）。
# 比误报更危险的失效模式是"扫描面为空 → 每条检查都 0 命中 → 全绿"：cwd 错、
# 目录改名、被从别的仓库调用，都会以那种方式伪装成功。所以这里 fail closed。
for req in core/internal core/go.mod specs/api-contract-v0.1.md scripts/check_ownership.sh; do
  [[ -e "$req" ]] || { printf '前置检查失败：%s 下缺 %s —— 扫描会静默全绿，拒绝出报告
' "$ROOT" "$req" >&2; exit 2; }
done
SCAN_GO="$(find core/internal -name '*.go' 2>/dev/null | wc -l | tr -d ' ')"
(( SCAN_GO >= 20 )) || { printf '前置检查失败：core/internal 只找到 %s 个 .go 文件（期望 >=20），扫描面异常
' "$SCAN_GO" >&2; exit 2; }

VIOL=0
BCNT=0
fails=0
report() { printf '%s\n' "$*"; }
# 每类越界只详列前 10 条：一个刷屏的报告等于没有报告。
bad()    { VIOL=$((VIOL+1)); BCNT=$((BCNT+1)); (( BCNT <= 10 )) && printf '  [VIOLATION] %s\n' "$*"; }
tailor() { (( BCNT > 10 )) && printf '  [VIOLATION] …本类共 %d 条，仅列出前 10（总数计入结论）\n' "$BCNT"; BCNT=0; }
info()   { printf '  [INFO] %s\n' "$*"; }
hdr()    { printf '\n== %s ==\n' "$*"; }

# --- scan helpers -----------------------------------------------------------
# $1 = ERE, rest = files. Emits "path:line:text" for every hit, case-insensitive.
scan_go() {
  local re="$1"; shift
  [[ $# -eq 0 ]] && return 0
  grep -rinE --include='*.go' "$re" "$@" 2>/dev/null
}

# 所有权表：**运行时从 specs/api-contract-v0.1.md §7 解析**，不内嵌副本。
# 主控改一行 §7，本 lint 下一跑就用新表；解析失败一律 exit 2，绝不退回旧数据。
API_SPEC="specs/api-contract-v0.1.md"
declare -A OWNER
OWNER_ROWS=0
# §7 尚未覆盖、但摘要表/现实需要的路径（补齐后请从这张表里删）：
#   core/internal/message   ← orchestration 摘要表第 2 行（backend），§7 漏列
#   docs/adapters/phase0    ← survey 的报告目录，§7 的 docs/** 行自己写了"角色自己的报告除外"却没列行
#   README.md               ← 仓库门面，归主控（§7 未列）
declare -A OWNER_HOLE=(
  [core/internal/message]=backend
  [docs/adapters/phase0]=survey
  [README.md]=orchestrator
)
load_owner_table() {
  [[ -f "$API_SPEC" ]] || { printf '§7 数据源缺失：%s 不存在\n' "$API_SPEC" >&2; exit 2; }
  local row c1 c2 role p
  while IFS= read -r row; do
    row="${row%%$'
'}"
    [[ "$row" == "|"* ]] || continue
    c1="$(printf '%s' "$row" | cut -d'|' -f2)"
    c2="$(printf '%s' "$row" | cut -d'|' -f3)"
    role="$(printf '%s' "$c2" | tr -d '`*[:space:]' | tr -d ' ')"
    [[ -z "$role" || "$role" =~ ^-+$ || "$role" == "owner" ]] && continue
    case "$role" in
      *主控*|*orchestrator*) role=orchestrator ;;
      *对应role*|*对应角色*) role=PROGRESS_ROLE ;;
      *任意角色*)             role=ANYROLE ;;
    esac
    # 路径型行：cell1 里每个反引号片段是一个前缀。
    # §7 的缩写约定（"`core/internal/task/**`、`execution/**`、`session/**`"）里那种
    # "不带目录但带 /**" 的片段是相对本行第一个路径的，必须展开成 core/internal/execution/，
    # 否则 backend 自己的包会被判成无归属 = 天天误报。行内的根文件（.gitignore、core/go.mod）
    # 没有 /** 所以不会被误展开。
    local raw p base="" head rest
    while IFS= read -r raw; do
      raw="${raw%%$'\r'}"
      [[ -z "$raw" ]] && continue
      case "$raw" in *"<role>"*|*.sql|*.go|*.md) continue ;; esac
      head="${raw%%/*}"; rest="${raw#*/}"
      if [[ "$raw" == */\*\* && "$rest" != */* ]]; then
        # 单层 `X/**`：可能是 §7 的缩写（execution/** 指 core/internal/execution），也可能是
        # 根目录（specs/** docs/** scripts/** .scratch/**）。用文件系统判，不硬编码目录清单：
        # 拼在本行第一个路径后面真实存在的才算缩写。
        # 判据用"根目录存不存在"而不是"拼上去存不存在"：后者在目标包还没被创建时
        # （如 internal/process 尚不存在）会误判成根目录，等 adapter 真建出来之前一直假报越界。
        if [[ -d "$head" ]]; then p="$head/"; else p="${base:+$base/}$head/"; fi
      elif [[ "$raw" == */\*\* ]]; then
        p="${raw%/\*\*}/"
      else
        p="$raw"
      fi
      # base 只在它是多级目录时才认（`dirname specs/` 得到 "."，用它拼会造出 ./docs/ 这种键）
      [[ -z "$base" ]] && { base="$(dirname -- "${p%/}")"; [[ "$base" == */* ]] || base=""; }
      OWNER["$p"]="$role"; OWNER_ROWS=$((OWNER_ROWS+1))
    done < <(printf '%s' "$c1" | tr -d '\r' | grep -oE '`[^`]+`' | tr -d '`')
  done < <(sed -n '/^## 7\./,$p' "$API_SPEC")
  if (( OWNER_ROWS < 12 )); then
    printf '§7 只解析出 %d 条归属规则（期望 >=12），八成是表格格式变了 —— 修 lint，不要带着半张表继续判\n' "$OWNER_ROWS" >&2
    exit 2
  fi
}

# 最长前缀匹配；查不到就是 UNOWNED（真越界），不做任何猜测。
owner_of() {
  local p="${1#./}" best="" bo="" pref
  case "$p" in
    .qoder/progress/orchestration.md) printf 'orchestrator'; return ;;
    .qoder/progress/*.md)             local r="${p#.qoder/progress/}"; printf '%s' "${r%.md}"; return ;;
  esac
  for pref in "${!OWNER[@]}"; do
    case "$p" in
      "$pref"*) (( ${#pref} > ${#best} )) && { best="$pref"; bo="${OWNER[$pref]}"; } ;;
    esac
  done
  for pref in "${!OWNER_HOLE[@]}"; do
    case "$p" in
      "$pref"*) (( ${#pref} > ${#best} )) && { best="$pref"; bo="${OWNER_HOLE[$pref]}"; } ;;
    esac
  done
  printf '%s' "${bo:-UNOWNED}"
}

# --- check 1: approvals SQL stays in internal/approval ----------------------
APPROVALS_RE='(from|update|insert[[:space:]]+into|join)[[:space:]]+approvals\b'
check_1() {
  hdr "1/8 approvals 表 SQL 只准出现在 internal/approval（契约① §3.1）"
  local hits n
  hits="$(scan_go "$APPROVALS_RE" core/internal || true)"
  n=$([[ -n "$hits" ]] && printf '%s\n' "$hits" | wc -l | tr -d ' ' || echo 0)
  (( n == 0 )) && { info "0 hits — internal/approval 尚无 store.go，此项暂无证据（不是通过，是还没写）"; return; }
  local p
  while IFS= read -r line; do
    p="${line%%:*}"
    case "$p" in
      core/internal/approval/*) printf '  [ok-in-owner] %s\n' "$line" ;;
      *) bad "approvals SQL 出现在非归属包: $p  ->  ${line#*:}" ;;
    esac
  done <<< "$hits"
  tailor
}

# --- check 2: no cross-package table SQL ------------------------------------
# 表 → 归属包。TABLE_MAP 是唯一数据源：既能查也能按表名遍历（T-24 要求扫描绑在表名上，
# 不绑函数名字面量——绑函数名的判据会随重构静默失效）。
# 归属依据：契约①/§7 的角色域划分 + §7 的路径表。handoffs 还没有独立包，暂挂 quota 名下。
# 注意：§7 会加行（17:45 就补了 core/internal/notification/**），这张表跟着 §7 改，别自己发明归属。
TABLE_MAP='schema_migrations|internal/platform/db
event_journal|internal/platform/db
blobs|internal/platform/db
idempotency_keys|internal/platform/db
settings|internal/platform/db
principals|internal/platform/db
trust_domains|internal/platform/db
projects|internal/platform/db
project_paths|internal/platform/db
attachments|internal/platform/db
message_attachments|internal/platform/db
task_attachments|internal/platform/db
project_attachments|internal/platform/db
audit_entries|internal/audit
tasks|internal/task
task_dependencies|internal/task
task_session_links|internal/task
executions|internal/execution
execution_leases|internal/execution
execution_output_chunks|internal/execution
sessions|internal/session
messages|internal/session
quota_accounts|internal/quota
quota_snapshots|internal/quota
notifications|internal/notification
handoffs|internal/quota
approvals|internal/approval
temporary_grants|internal/approval internal/permission
policy_rules|internal/permission
adapter_instances|internal/adapter
adapter_capabilities|internal/adapter
models|internal/adapter
process_instances|internal/adapter'

owner_of_table() { printf '%s\n' "$TABLE_MAP" | awk -F'|' -v t="$1" '$1==t{print $2}'; }

# cross_package_refs <root>：按表名遍历 TABLE_MAP，输出 "<kind>|<file>|<table>|<owner>"。
# kind: write=生产代码里的写、fixture=_test.go 里的写、read=只读引用。
# 单独成函数只为了 --selftest 能拿合成件验它，而不是信它"应该能抓到"。
cross_package_refs() {
  local root="$1" tbl want excl hits line f
  while IFS='|' read -r tbl want; do
    [[ -z "$tbl" || "$want" == "internal/platform/db" ]] && continue
    excl="^${root}internal/platform/"          # 机制层的映射表本来就该知道所有表名
    for w in $want; do excl="$excl|^${root}${w}/"; done   # 归属包自己引用自己的表 = 正常
    hits="$(grep -rniE "\b${tbl}\b" "$root"internal --include='*.go' 2>/dev/null \
             | grep -vE "$excl" \
             | grep -vE ':[0-9]+:[[:space:]]*(//|\*|/\*)' || true)"
    [[ -z "$hits" ]] && continue
    while IFS= read -r line; do
      [[ -z "$line" ]] && continue
      f="${line%%:*}"
      case "$f" in *_test.go) printf 'fixture|%s|%s|%s\n' "$f" "$tbl" "$want"; continue ;; esac
      if printf '%s' "$line" | grep -qiE 'UPDATE|INSERT[[:space:]]+INTO|DELETE[[:space:]]+FROM|\.Append\(|\.Create\(|\.Exec\('; then
        printf 'write|%s|%s|%s\n' "$f" "$tbl" "$want"
      elif printf '%s' "$line" | grep -qE "[\"'\`]${tbl}[\"'\`]"; then
        # 只认"整个字符串字面量就是表名"——那才是 `const t = "tasks"` + tx.Append(t, …) 这种
        # 变量传表名的源头。放宽到"字符串里含表名"会被 prose 与 CLI flag 名淹掉
        # （"stream frames must be notifications"、--dangerously-bypass-approvals-and-sandbox）。
        printf 'literal|%s|%s|%s\n' "$f" "$tbl" "$want"
      else
        printf 'read|%s|%s|%s\n' "$f" "$tbl" "$want"
      fi
    done <<< "$hits"
  done <<< "$TABLE_MAP"
}

check_2() {
  hdr "2/8 跨包表引用（T-24：按表名查，不看函数名写法）"
  local all kind f tbl want
  all="$(cross_package_refs core/)"
  local n=0 reads=0 fx=0 lits=0 readfiles='' fxf='' litfiles='' litwhat=''
  while IFS='|' read -r kind f tbl want; do
    [[ -z "$kind" ]] && continue
    case "$kind" in
      write)   n=$((n+1)); bad "$f 在归属包（$want）之外引用表 $tbl，且这一行是写语句/写调用" ;;
      fixture) fx=$((fx+1)); fxf="$fxf $f" ;;
      literal) lits=$((lits+1)); litfiles="$litfiles $f"; litwhat="$litwhat $tbl@$f" ;;
      read)    reads=$((reads+1)); readfiles="$readfiles $f" ;;
    esac
  done <<< "$all"
  (( fx > 0 )) && info "$fx 处跨包写在 _test.go 里（造外键父行，按 §1.3 允许，不判红）：$(printf '%s\n' $fxf | sort -u | tr '\n' ' ')"
  (( lits > 0 )) && bad "$lits 处：整个表名字符串出现在非归属包里（变量传表名的典型形状，需人工判是不是绕过了归属）：$(printf '%s
' $litwhat | sort -u | tr '
' ' ')"
  (( reads > 0 )) && info "另有 $reads 处只读引用（§1.3/§4 允许跨模块读与 JOIN，不判越界）：$(printf '%s\n' $readfiles | sort -u | tr '\n' ' ')"
  (( n == 0 && reads == 0 && fx == 0 )) && info "没有任何包在自己的目录之外提到别人的表名"
  local ex
  ex="$(scan_go '\.Exec(Context)?\(' core/internal | grep -v '/platform/db/' | grep -v '_test\.go:' || true)"
  if [[ -n "$ex" ]]; then
    while IFS= read -r line; do
      bad "业务包绕开平台层自行执行写语句（§1）: ${line%%:*}"
    done <<< "$ex"
  else
    info "非 platform/db 的生产代码 0 处 .Exec( —— 写路径没有旁门（_test.go 建 fixture 不算）"
  fi
  tailor
}

# --- check 3: dirty paths vs ownership matrix -------------------------------
FROZEN='core/migrations/000001_core_schema.sql'
check_3() {
  hdr "3/8 变更文件 vs 角色所有权矩阵（契约③ §7；基线 $BASELINE）"
  if ! git rev-parse --verify -q "$BASELINE" >/dev/null; then bad "基线 $BASELINE 不存在"; return; fi
  # WORK = 未提交的（只能是 worker 干的，worker 无 git 写权限）；
  # COMMITTED = 基线之后已提交且当前工作树不再改的（只能是主控落的盘，不计越界）。
  local WORK COMMITTED ALL p o NM=0 NMROOT=''
  WORK="$( { git diff --name-only; git ls-files --others --exclude-standard; } 2>/dev/null | sort -u | grep . || true)"
  COMMITTED="$( { git diff --name-only "$BASELINE"; git log --format= --name-only "$BASELINE"..HEAD; } 2>/dev/null \
                | sort -u | grep . | comm -23 - <(printf '%s\n' "$WORK") || true)"
  ALL="$(printf '%s\n%s\n' "$WORK" "$COMMITTED" | sort -u | grep . || true)"
  [[ -z "$ALL" ]] && { info "工作树与基线之后都没有变更"; return; }
  declare -A seen_owner
  while IFS= read -r p; do
    [[ -z "$p" ]] && continue
    # 第三方依赖树单独汇总：它是一条真发现，但不该以几千行刷屏。
    case "$p" in
      *node_modules/*) NM=$((NM+1)); NMROOT="${p%%node_modules/*}"; continue ;;
    esac
    o="$(owner_of "$p" | head -1)"
    seen_owner["$o"]="$(( ${seen_owner["$o"]:-0} + 1 ))"
    [[ "$p" == "$FROZEN" ]] && bad "冻结件被改动: $p（红线①，无论谁提交）"
    printf '%s\n' "$WORK" | grep -qxF "$p" || continue
    case "$o" in
      UNOWNED)    bad "无归属路径（§7 没有这一行，worker 写了就是越界）: $p" ;;
      orchestrator)
        git ls-files --error-unmatch "$p" >/dev/null 2>&1 || \
          bad "主控独占文件以未跟踪状态出现（worker 不得新建，需变更写 Coordination REQ）: $p"
        [[ "$p" == specs/adapter-protocol/* ]] && \
          report "  [契约冲突] $p：§7 判给主控、orchestration.md 摘要表判给 adapter，且 adapter 已据此在改。请主控裁定，本项不计越界。" ;;
    esac
  done <<< "$ALL"
  (( NM > 0 )) && bad "${NMROOT}node_modules/ 下有 $NM 个第三方文件既没被忽略也没被跟踪——.gitignore 第 4 行是根锚定的 /node_modules/，子目录不吃；任何一次 git add -A 都会把依赖树写进版本库"
  report "  按 owner 统计（含主控已提交的部分，仅分布用）:"
  for o in "${!seen_owner[@]}"; do printf '    %-12s %s 个文件\n' "$o" "${seen_owner[$o]}"; done
  info "未提交（=worker 在做）$(printf '%s\n' "$WORK" | grep -c . ) 个；基线之后已提交（=主控落盘）$(printf '%s\n' "$COMMITTED" | grep -c . ) 个"
  tailor
}

# orphan_modules: stdin = go.sum 新增的模块路径，$1 = 模块图文件描述（这里是 stdin 竞争，
# 所以图用变量传）；输出不在图里的那些。单独成函数是为了 --selftest 能验它不是永真式。
orphan_modules() {
  local graph="$1" mod
  shift
  while IFS= read -r mod; do
    [[ -z "$mod" ]] && continue
    printf '%s
' "$graph" | grep -qxF "$mod" || printf '%s ' "$mod"
  done
}

# --- check 4: go.mod / go.sum 只允许主控变 ----------------------------------
check_4() {
  hdr "4/8 core/go.mod 只准主控变；go.sum 按 T-11 放宽后的判据"
  # T-11 裁定（17:12 投递，出处 commit 2530517）：go.sum 单独增长**不算**越界——
  # go.mod 一字未动就说明没人跑 go get，那 15 行全是 modernc.org/sqlite 的传递依赖，
  # 由 go build/test 解析依赖图时自动写入，属预期。硬判据只有两条：
  #   ① go.mod 有未提交变更 = 越界；
  #   ② go.sum 里出现 go.mod 未声明的**顶层**模块 = 越界（防的是"顺手加依赖"，不是自动补全）。
  local dirty
  dirty="$(git status --porcelain -- core/go.mod | awk '{print $NF}')"
  [[ -n "$dirty" ]] && bad "core/go.mod 有未提交改动（worker 需要依赖请写 Coordination REQ）"
  # 判据②：go.sum 新增的模块路径必须在 go.mod 的模块图里（go list -m all）。
  # 传递依赖本身就是别的模块，用"路径前缀"判不通（modernc.org/libc 不在 modernc.org/sqlite 下面），
  # 只有模块图能区分"既有依赖的自动补全"与"有人塞了新依赖"。
  local graph orphan=""
  local GO="${GO:-/d/DevTools/go/bin/go.exe}"
  graph="$( cd core && "$GO" list -m -f '{{.Path}}' all 2>/dev/null | sort -u )"
  if [[ -z "$graph" ]]; then
    info "判据② 未能执行（go list -m all 空/离线），只判了 go.mod 那一条；不猜、不报红"
  else
    added="$(git diff "$BASELINE" -- core/go.sum | grep -E '^\+[^+]' | sed 's/^+//' | awk '{print $1}' | sort -u)"
    orphan="$(orphan_modules "$graph" <<< "$added")"
    if [[ -n "$orphan" ]]; then
      bad "go.sum 新增了模块图里没有的依赖（有人绕过 go.mod 塞依赖）:$orphan"
    else
      info "go.sum 的增量全在模块图内（传递依赖自动补全，按 T-11 不算越界）"
    fi
  fi
  git log --format='    来路 commit %h %ad %an %s' --date=format:%H:%M "$BASELINE"..HEAD -- core/go.mod core/go.sum
  tailor
}

# --- check 5: evidence / scratch must not be tracked ------------------------
check_5() {
  hdr "5/8 .qoder/evidence/ 与 .scratch/ 不得进入 git 索引"
  local tracked
  tracked="$(git ls-files | grep -E '^(\.qoder/evidence/|\.scratch/)' || true)"
  if [[ -n "$tracked" ]]; then
    while IFS= read -r p; do bad "已入库的本地产物: $p"; done <<< "$tracked"
  else
    info "git ls-files 中 0 条命中"
  fi
  local d
  for d in .qoder/evidence .scratch; do
    git check-ignore -q "$d/probe" || bad ".gitignore 未忽略 $d/（本地产物会漏进版本库）"
  done
  tailor
}

# --- check 6: dependency direction (ADR-004 / 契约 §0) ----------------------
CORE_IMPORT='github\.com/SadCat18/lucero/core/internal'
check_6() {
  hdr "6/8 依赖方向（eventjournal 不 import 业务包；platform/db 只允许 eventjournal；audit 不 import platform/db）"
  local hits line p imp

  hits="$(scan_go "import|\"$CORE_IMPORT" core/internal/eventjournal | grep -E "\"$CORE_IMPORT" || true)"
  [[ -n "$hits" ]] && while IFS= read -r line; do bad "eventjournal 引入了内部包: $line"; done <<< "$hits"
  [[ -z "$hits" ]] && info "eventjournal: 0 个内部 import（目录层，合法）"

  hits="$(scan_go "\"$CORE_IMPORT" core/internal/platform | grep -v '/eventjournal"' || true)"
  [[ -n "$hits" ]] && while IFS= read -r line; do bad "platform/db 引入了业务包: $line"; done <<< "$hits"
  [[ -z "$hits" ]] && info "platform/db: 仅 internal/eventjournal（§1.1 第 8 条明确允许）"

  hits="$(scan_go "\"$CORE_IMPORT/platform/db" core/internal/audit | grep -v _test.go || true)"
  [[ -n "$hits" ]] && while IFS= read -r line; do bad "internal/audit 不得 import platform/db（§0）: $line"; done <<< "$hits"
  [[ -z "$hits" ]] && info "internal/audit: 未 import platform/db"
  tailor
}

# api_prefix_diff <yaml集合> <契约③集合>：任一方向缺一条就是 §6.3 没同步。
api_prefix_diff() {
  local p
  for p in $(comm -13 <(printf '%s
' "$2") <(printf '%s
' "$1")); do bad "yaml 有 $p 但契约③ §3 没写"; done
  for p in $(comm -23 <(printf '%s
' "$2") <(printf '%s
' "$1")); do bad "契约③ §3 有 $p 但 yaml 里没有"; done
}

# --- check 7: /api/v1 面三处同步（契约③ §6.3） --------------------------------
SPEC3='specs/api-contract-v0.1.md'
SPEC1='specs/events/event-contract-v0.1.md'
YAML='specs/openapi/core-v1.yaml'
check_7() {
  hdr "7/8 /api/v1 面变更的三处同步（契约③ §6.3：yaml + 契约③ + 契约① 事件目录）"
  # 第一腿（现在就能判）：契约③ §4 提到的事件类型必须已在契约① §6 登记。
  local ev n=0 miss=0
  while read -r ev; do
    [[ -z "$ev" ]] && continue
    n=$((n+1))
    if ! grep -qwF -- "$ev" "$SPEC1"; then
      bad "契约③ §4 用了事件 $ev，契约① §6 目录里没有它"
      miss=$((miss+1))
    fi
  done < <(sed -n '/^## 4\./,/^## 5\./p' "$SPEC3" | grep -oE '[a-z][a-z0-9]*_[a-z0-9_]+' | sort -u)
  (( miss == 0 )) && info "事件目录那一腿：$n 个事件名全部已在契约① §6 登记"
  # 第二腿（等 frontend）：yaml 的资源前缀集合必须与契约③ §3 双向一致。
  if [[ ! -f "$YAML" ]]; then
    info "未满足→标记：$YAML 尚未生成（frontend 未派）。这一腿本轮不判，等它首次落地即自动生效。"
    tailor; return
  fi
  local a b
  a="$(grep -oE '/api/v1/[a-z][a-z0-9-]*' "$YAML" | sort -u)"
  # 比对范围是整份契约③，不只 §3 的端点清单：ws-ticket 这类端点是主控写在 §2.1 的，
  # 只读 §3 会把合法端点报成"没同步"。
  b="$(grep -oE '/api/v1/[a-z][a-z0-9-]*' "$SPEC3" | sort -u)"
  api_prefix_diff "$a" "$b"
  info "资源前缀比对：yaml $(printf '%s\n' "$a" | grep -c .) 条 / 契约③ $(printf '%s\n' "$b" | grep -c .) 条"
  tailor
}

# --- check 8: 错误码三方一致（契约② §15.1 集合 ↔ §15.2 HTTP 映射 ↔ Go codeNames）----
SPEC2='specs/adapter-protocol/adapter-protocol-v0.1.md'
# 命名分裂是故意的（§15：Adapter 大写蛇形 / HTTP 小写蛇形），所以不比对字符串，比对"集合归属"。
# version_conflict 是 HTTP 层专有、不经 Adapter，按 T-31① 不许按行数硬抹平。
TRANSPORT_ONLY='INVALID_PARAMS METHOD_NOT_FOUND'   # JSON-RPC/传输层符号，§15.1 本就不收
check_8() {
  hdr "8/8 错误码三方一致（§15.1 集合 ↔ §15.2 HTTP 映射 ↔ internal/adapter 的 codeNames）"
  [[ -f "$SPEC2" ]] || { bad "找不到 $SPEC2"; return; }
  local spec_codes map_src map_http go_names c g row code stat n=0
  spec_codes="$(sed -n '/^### 15\.1/,/^### 15\.2/p' "$SPEC2" | awk -F'|' '/^\| `/ {gsub(/[ `]/,"",$2); print $2}' | grep -E '^[A-Z][A-Z_]{3,}$' | sort -u)"
  map_src="$(sed -n '/^### 15\.2/,/^## /p' "$SPEC2" | awk -F'|' '/^\| `/ {gsub(/[ `]/,"",$4); print $4}' | grep -E '^[A-Z][A-Z_]{3,}$' | sort -u)"
  # 只取"首列是小写码、第三列以数字开头"的真数据行：§15.2 里还有一行图例
  # （"领域错误的唯一数字码：…真判别符在 data.code"），不筛就会被当 HTTP 码判成违规。
  map_http="$(sed -n '/^### 15\.2/,/^## /p' "$SPEC2" | grep -E '^\| `[a-z_]+`[[:space:]]*\|[[:space:]]*[0-9]'                 | awk -F'|' '{gsub(/[ `]/,"",$3); print $3}' | sort -u)"
  go_names="$(sed -n '/^var codeNames/,/^}/p' core/internal/adapter/protocol.go | grep -oE '"[A-Z][A-Z_]{3,}"' | tr -d '"' | sort -u)"
  local nspec nmap ngo
  nspec="$(printf '%s\n' $spec_codes | grep -c .)"
  nmap="$(printf '%s\n' $map_src | grep -c .)"
  ngo="$(printf '%s\n' $go_names | grep -c .)"
  info "§15.1 声明 $nspec 个 Adapter 码；§15.2 的「来自」列 $nmap 个；Go codeNames $ngo 个符号"
  # 防空转：三个集合任意一个为空就说明提取失败（sed/grep 或文档结构变了），
  # 那时每条腿都会"0 命中 = 通过"，是比误报更坏的伪装成功。
  (( nspec >= 8 && nmap >= 8 && ngo >= 8 )) || \
    bad "错误码三方对账的输入集合异常（§15.1=$nspec 来自列=$nmap Go=$ngo，期望各 >=8）—— 判据会空转，先修 lint"
  # 腿 A：规范里承诺的码，Go 侧必须发得出来
  for c in $spec_codes; do
    printf '%s\n' "$go_names" | grep -qxF "$c" || bad "§15.1 有 $c，但 internal/adapter 的 codeNames 里没这个符号（Adapter 发不出它承诺的码）"
  done
  # 腿 B：每个 Adapter 码都必须有一行 HTTP 映射
  for c in $spec_codes; do
    printf '%s\n' "$map_src" | grep -qxF "$c" || bad "§15.1 的 $c 在 §15.2 映射表里没有行（HTTP 层无处安放它）"
  done
  # 腿 C：Go 侧多出来的符号要么在 §15.1，要么是已登记的传输层专用码
  for g in $go_names; do
    printf '%s\n' "$spec_codes" | grep -qxF "$g" && continue
    case " $TRANSPORT_ONLY " in *" $g "*) continue ;; esac
    bad "internal/adapter 有 $g，但 §15.1 与传输层专用清单里都没有它（规范外凭空多一个码）"
  done
  # 腿 D：映射表的 HTTP 码必须是三位数字
  for row in $map_http; do
    [[ "$row" =~ ^[1-5][0-9][0-9]$ ]] || bad "§15.2 的 HTTP 码列出现非三位数字：$row"
  done
  # 腿 E：HTTP 专有行只许 version_conflict，且它不得有 Go 符号
  for code in $(sed -n '/^### 15\.2/,/^## /p' "$SPEC2" | awk -F'|' '/^\| `/ && $4 ~ /—/ {gsub(/[ `]/,"",$2); print $2}'); do
    [[ "$code" == "version_conflict" ]] || bad "§15.2 出现第二个"无 Adapter 来源"的行 $code —— 要么补 §15.1 要么说明理由"
    printf '%s\n' "$go_names" | grep -qxF "VERSION_CONFLICT" && bad "version_conflict 声称 HTTP 专有，Go 侧却定义了它"
  done
  tailor
}

# --- selftest: the scanners must be able to fail ----------------------------
run_all() { check_1; check_2; check_3; check_4; check_5; check_6; check_7; check_8; }

selftest() {
  hdr "SELFTEST（证明上面几条不是永真式）"
  local tmp; tmp="$(mktemp -d)" || exit 2
  mkdir -p "$tmp/internal/task" "$tmp/internal/approval" "$tmp/internal/execution"
  printf 'package task\n\nconst q = "SELECT id FROM approvals WHERE s = ?"\n'          > "$tmp/internal/task/repo.go"
  printf 'package approval\n\nconst q = "UPDATE APPROVALS SET state = ? WHERE id = ?"\n' > "$tmp/internal/approval/store.go"
  printf 'package execution\n\nconst q = "insert into approvals (id) values (?)"\n'     > "$tmp/internal/execution/x.go"
  printf 'package task\n\nconst q = "UPDATE " + tbl + " SET x = ?"\n'                   > "$tmp/internal/task/dyn.go"
  printf 'package task\n\n_, _ = r.db.Exec("whatever", 1)\n'                            > "$tmp/internal/task/exec.go"
  printf 'package task\n\nimport "github.com/SadCat18/lucero/core/internal/eventjournal"\n' > "$tmp/internal/task/imp.go"

  local a b c d
  # 3 = 两条越界 + 一条在归属包内；大小写与 insert into 变体都要抓到
  a="$(scan_go "$APPROVALS_RE" "$tmp" | wc -l | tr -d ' ')"
  (( a == 3 )) || { report "  [SELFTEST FAIL] approvals 扫描命中 $a，期望 3"; fails=$((fails+1)); }
  # 写表扫描要能抓到 UPDATE executions，并且归属判据正确
  [[ "$(owner_of core/internal/eventjournal/events.go)" == "db" ]] || { report "  [SELFTEST FAIL] 路径归属映射错"; fails=$((fails+1)); }
  # 绕开平台层直接 Exec 必须抓到
  c="$(scan_go '\.Exec(Context)?\(' "$tmp" | grep -v '_test\.go:' | wc -l | tr -d ' ')"
  (( c == 1 )) || { report "  [SELFTEST FAIL] Exec 旁门扫描命中 $c，期望 1"; fails=$((fails+1)); }
  # import 方向必须抓到
  d="$(scan_go "\"$CORE_IMPORT" "$tmp" | wc -l | tr -d ' ')"
  (( d == 1 )) || { report "  [SELFTEST FAIL] import 方向扫描命中 $d，期望 1"; fails=$((fails+1)); }
  # 已知盲点：拼接出的表名扫不出来。若哪天它能扫出来，说明规则变严了，改期望值。
  if grep -qiE 'UPDATE[[:space:]]+[a-z_][a-z0-9_]*' "$tmp/internal/task/dyn.go"; then
    report "  [SELFTEST FAIL] 拼接盲点已不再成立，请把 check_2 的 INFO 说明删掉并收紧规则"; fails=$((fails+1))
  fi
  # §7 解析器：键必须能命中真实路径，且解析不出时必须报错退出而不是静默放行
  for probe in "core/internal/task/statemachine.go|backend" "core/internal/execution/x.go|backend"                "core/internal/eventjournal/events.go|db" "apps/desktop/x.ts|frontend"                "specs/openapi/core-v1.yaml|frontend" "core/go.mod|orchestrator" "core/test/x_test.go|test"                "core/internal/process/x.go|adapter" "docs/handoff/x.md|orchestrator"                "apps/desktop/a.tsx|frontend" "adapters/fake/main.go|adapter"; do
    [[ "$(owner_of "${probe%%|*}")" == "${probe##*|}" ]] ||       { report "  [SELFTEST FAIL] §7 解析后的归属判错：${probe%%|*} -> $(owner_of "${probe%%|*}")，期望 ${probe##*|}"; fails=$((fails+1)); }
  done
  [[ "$(owner_of packages/toolchain/package.json)" == "UNOWNED" ]] ||     { report "  [SELFTEST FAIL] 无归属路径没被判成 UNOWNED（§7 缺行的检测失效了）"; fails=$((fails+1)); }
  # 前置检查：脚本按自身位置推仓库根，换 cwd 不影响，所以只能把它拷进一个没有 core/internal
  # 的空仓库里验"扫描面为空必须 exit 2"，而不是在临时目录里调它（那样测的是同一份真仓库）。
  local bare; bare="$(mktemp -d)"
  mkdir -p "$bare/scripts" && cp scripts/check_ownership.sh "$bare/scripts/"
  bash "$bare/scripts/check_ownership.sh" >/dev/null 2>&1
  [[ $? -eq 2 ]] || { report "  [SELFTEST FAIL] 在没有 core/internal 的仓库副本里跑，脚本没以 2 号错退出——它会伪装成全绿"; fails=$((fails+1)); }
  rm -rf "$bare"
  # T-11 的 go.sum 判据：图里有的放行、没有的必须报出来
  [[ "$(printf 'a
c
' | orphan_modules "$(printf 'a
b')")" == "c " ]] ||     { report "  [SELFTEST FAIL] go.sum 孤儿模块判据不灵（永真或漏报）"; fails=$((fails+1)); }
  # T-24 的按表名扫描：合成件里"task 包写 quota_accounts"必须被判成 write 类命中
  local fx2; fx2="$(mktemp -d)/internal"
  mkdir -p "$fx2/task" "$fx2/quota"
  printf 'package task

const tblName = "quota_accounts"
func f() { _ = tblName }
' > "$fx2/task/dyn.go"
  printf 'package task

const q = `UPDATE quota_accounts SET label = ?`
' > "$fx2/task/w.go"
  printf 'package quota

const q = `SELECT id FROM quota_accounts`
' > "$fx2/quota/own.go"
  local out; out="$(cross_package_refs "$(dirname "$fx2")/")"
  printf '%s
' "$out" | grep -qE '^write\|.*task/w\.go\|quota_accounts\|' ||     { report "  [SELFTEST FAIL] 按表名扫描抓不到 task 包写 quota_accounts"; fails=$((fails+1)); }
  printf '%s
' "$out" | grep -qE '^literal\|.*task/dyn\.go\|quota_accounts\|' ||     { report "  [SELFTEST FAIL] 表名以字符串形式存在（变量传参的源头）没被单列出来"; fails=$((fails+1)); }
  printf '%s
' "$out" | grep -qE 'quota/own\.go' &&     { report "  [SELFTEST FAIL] 归属包自己引用自己的表被判进来了（判据过严）"; fails=$((fails+1)); }
  rm -rf "$(dirname "$fx2")"
  # §6.3 那一腿：yaml 少一条前缀必须报出来（真 yaml 归 frontend，这里用合成件）
  local before=$VIOL
  api_prefix_diff "/api/v1/a
/api/v1/b" "/api/v1/a"
  (( VIOL == before + 1 )) || { report "  [SELFTEST FAIL] 前缀差集只报出 $((VIOL-before)) 条，期望 1"; fails=$((fails+1)); }
  VIOL=$before
  grep -qwF project_created "$SPEC1" || { report "  [SELFTEST FAIL] 事件目录那一腿读不到契约①（$SPEC1），它成了永真式"; fails=$((fails+1)); }
  rm -rf "$tmp"
  (( fails == 0 )) && report "  [SELFTEST OK] 扫描器都能对合成越界报警"
  return $(( fails > 0 ? 2 : 0 ))
}

load_owner_table
# --explain <path>: 打印某个路径判给谁、命中哪条规则（被 lint 点名时先跑这个自查）
if [[ "${1:-}" == "--rules" ]]; then
  for k in "${!OWNER[@]}"; do printf '[%s] -> %s
' "$k" "${OWNER[$k]}"; done | sort; exit 0
fi
if [[ "${1:-}" == "--explain" ]]; then
  shift
  for arg in "$@"; do
    local_src=""
    for pref in "${!OWNER[@]}"; do case "$arg" in "$pref"*) [[ "${#pref}" -gt "${#local_src}" ]] && local_src="$pref";; esac; done
    for pref in "${!OWNER_HOLE[@]}"; do case "$arg" in "$pref"*) [[ "${#pref}" -gt "${#local_src}" ]] && local_src="$pref (§7 空洞补偿表)";; esac; done
    printf '%-52s -> %-12s [%s]
' "$arg" "$(owner_of "$arg")" "${local_src:-无规则命中}"
  done
  exit 0
fi
if (( SELFTEST )); then selftest; exit $?; fi

report "Lucero ownership lint  baseline=$BASELINE  HEAD=$(git rev-parse --short HEAD)  $(date -Iseconds)"
report "  扫描面：core/internal 下 $SCAN_GO 个 .go 文件（少于 20 个本脚本直接拒绝出报告）"
report "  归属表：$OWNER_ROWS 条规则，运行时解析自 $API_SPEC §7（无内嵌副本；--explain <path> 可自查判定依据）"
run_all
hdr "结论"
if (( VIOL > 0 )); then report "  $VIOL 项越界（见上）"; exit 1
else report "  本次扫描 0 项越界。注意 INFO 行：命中数为 0 的检查可能只是对应实现尚未落地。"; exit 0; fi
