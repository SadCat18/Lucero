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
SCAN_GO="$(find core -name '*.go' -not -path '*/node_modules/*' 2>/dev/null | wc -l | tr -d ' ')"
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
ROOT_ROW_SEEN=0   # §7 里 "`core/`（根目录本身）" 那一行有没有解析到；它只登记、不作归属（CO-36②）
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
      # §7 里 "`core/`（根目录本身）"那一行**不许当兜底归属用**（主控 13:22 裁定）。
      # 它的本意是"core/ 根下只许 go.mod/go.sum，新增任何根级文件都算越界"；可一旦把它注册进表，
      # 任何没被更具体规则命中的 core/** 路径都会最长前缀命中 `core/` ⇒ 判给 orchestrator，
      # 报告于是写"越界方 = 主控"，真越界方永远查不到。落 UNOWNED 才是对的：
      # 显式行（core/go.mod、core/go.sum）仍归 orchestrator，靠的是它们自己那行。
      if [[ "$p" == "core/" || "$p" == "./core/" ]]; then
        ROOT_ROW_SEEN=1
        continue
      fi
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

# strip_comments：把 file:line:text 流里**行内注释那一段**切掉（保留 file:line: 前缀）。
# 主控 09-21 裁定：判"某个字面量在不在代码里"**只看字符串字面量，不看注释**
# （`approval/cancel` 那条"它不存在"的注释因此天然豁免，不需要谁去改措辞）。
# 不做这一步会有假绿：常量删了、注释里还写着 `"PROTOCOL_NOT_INITIALIZED"`，grep 照样命中。
# 按字符扫：引号/反引号里面的 // 不算注释起点，行内 // 之后整段丢掉。
# 已知边界：跨行 raw string 里若单独出现 //，那一行会被截 —— core/ 下实测没有这种形状。
strip_comments() {
  awk '{
    txt=$0; pre=""
    if (match($0, /^[^:]+:[0-9]+:/)) { pre=substr($0,1,RLENGTH); txt=substr($0,RLENGTH+1) }
    out=""; q=""
    for (i=1; i<=length(txt); i++) {
      c=substr(txt,i,1)
      if (q != "") { out=out c; if (c==q) q=""; continue }
      if (c=="\"" || c=="\047" || c=="`") { q=c; out=out c; continue }
      if (c=="/" && substr(txt,i+1,1)=="/") break
      out=out c
    }
    print pre out
  }'
}

# --- check 1: approvals SQL stays in internal/approval ----------------------
APPROVALS_RE='(from|update|insert[[:space:]]+into|join)[[:space:]]+approvals\b'
check_1() {
  hdr "1/9 approvals 表 SQL 只准出现在 internal/approval（契约① §3.1）"
  local hits n
  hits="$(scan_go "$APPROVALS_RE" core || true)"
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

# W13 的写调用模式（契约③ §7.1 判据表原文）：**只匹配"以字面量表名为首参"的调用**。
# 这一条把 Service 方法整个排除掉——Service 调用不传表名字面量，所以不算"往表里写"。
# T-74 之前用的是放宽版 .Append( / .Create( / .Exec(，REQ-WS-3 的第 4 条命中就是它造出的假阳性。
W13_RE='\.Append\("|\.UpdateOnce\("|\.Touch\("|\.Create\("'

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
  local root="$1" tbl want excl hits line f wmeth wtbl
  while IFS='|' read -r tbl want; do
    [[ -z "$tbl" ]] && continue
    # 机制表（event_journal/audit_entries/…）也扫：读走 INFO、写才报红。
    # 之前整类跳过，导致 REQ-WS-2 那种"event_journal 的读视图住在 cmd/core/journal.go"看不见。
    excl="^${root}internal/platform/"          # 机制层的映射表本来就该知道所有表名
    for w in $want; do excl="$excl|^${root}${w}/"; done   # 归属包自己引用自己的表 = 正常
    hits="$(grep -rniE "\b${tbl}\b" "$root" --include='*.go' 2>/dev/null \
             | grep -vE "$excl" \
             | strip_comments \
             | grep -vE ':[0-9]+:[[:space:]]*$' || true)"
    [[ -z "$hits" ]] && continue
    while IFS= read -r line; do
      [[ -z "$line" ]] && continue
      f="${line%%:*}"
      case "$f" in *_test.go) printf 'fixture|%s|%s|%s\n' "$f" "$tbl" "$want"; continue ;; esac
      if printf '%s' "$line" | grep -qE "$W13_RE"; then
        # 写调用只在"表名字面量紧跟左括号"时才算（§7.1 W13-3）。放宽到 .Create( 会把
        # c.Tasks.Create( 这类 Service 方法判成写表——那正是 REQ-WS-3 里我说清的第 4 条假阳性。
        wmeth="$(printf '%s' "$line" | grep -oE '\.(Append|UpdateOnce|Touch|Create)\(' | head -1 | tr -d '.(')"
        wtbl="$(printf '%s' "$line" | grep -oE '\.(Append|UpdateOnce|Touch|Create)\("[a-z_]+' | head -1 | sed -E 's/.*\("//')"
        # §7.1 W13-1 已裁定这两张表在 transport/装配层写是合法的、不再要求下沉：走同一份
        # W13_ALLOW，不在这里另立口头豁免。白名单之外的表仍报 write，闭集那一红由 check_w13 负责。
        if [[ -n "$wtbl" && "$(w13_classify "$f" "$wtbl" "$wmeth")" == "allow" ]]; then
          printf 'w13-allow|%s|%s|%s\n' "$f" "$wtbl" "$want"
        else
          printf 'write|%s|%s|%s\n' "$f" "$tbl" "$want"
        fi
      elif printf '%s' "$line" | grep -qiE '(^|[^[:alnum:]_])UPDATE[[:space:]]|INSERT[[:space:]]+INTO|DELETE[[:space:]]+FROM'; then
        # 真 SQL 写语句：关键字后面必须跟空白。写成 UPDATE|INSERT|DELETE 不带边界的话，
        # Go 的 UpdateOnce( / DeleteRow( 会被大小写无关地吞进来——16:2x 那条假红就是这么来的。
        printf 'write|%s|%s|%s\n' "$f" "$tbl" "$want"
      elif printf '%s' "$line" | grep -qE "[\"'\`]${tbl}[\"'\`]"; then
        # 整个字符串字面量等于表名，还要看它的**语法角色**：
        # Go 结构体 tag（`json:"sessions"`）里的引号是字段序列化名，不是"表名以变量传入"的源头。
        # 今天第二条同源假阳性就在这里：fake-adapter 新写的 struct tag 被 literal 腿判了红。
        tagre="\`[a-zA-Z_]+:\"${tbl}\""
        if printf '%s' "$line" | grep -qE "$tagre"; then
          printf 'tag|%s|%s|%s\n' "$f" "$tbl" "$want"
        else
          # 只认"整个字符串字面量就是表名"——那才是 `const t = "tasks"` + tx.Append(t, …) 这种
          # 变量传表名的源头。放宽到"字符串里含表名"会被 prose 与 CLI flag 名淹掉
          # （"stream frames must be notifications"、--dangerously-bypass-approvals-and-sandbox）。
          printf 'literal|%s|%s|%s\n' "$f" "$tbl" "$want"
        fi
      else
        printf 'read|%s|%s|%s\n' "$f" "$tbl" "$want"
      fi
    done <<< "$hits"
  done <<< "$TABLE_MAP"
}

check_2() {
  hdr "2/9 跨包表引用（T-24：按表名查，不看函数名写法）"
  local all kind f tbl want
  all="$(cross_package_refs core/)"
  local n=0 reads=0 fx=0 lits=0 w13=0 tags=0 readfiles='' fxf='' litfiles='' litwhat='' tagfiles=''
  while IFS='|' read -r kind f tbl want snippet; do
    [[ -z "$kind" ]] && continue
    case "$kind" in
      write)   n=$((n+1)); bad "$f 在归属包（$want）之外引用表 $tbl，且这一行是写语句/写调用（写调用按 §7.1 W13 判：只认「字面量表名作首参」，Service 方法不算；打开该文件核对）" ;;
      fixture) fx=$((fx+1)); fxf="$fxf $f" ;;
      literal) lits=$((lits+1)); litfiles="$litfiles $f"; litwhat="$litwhat $tbl@$f" ;;
      read)    reads=$((reads+1)); readfiles="$readfiles $f" ;;
      tag)     tags=$((tags+1)); tagfiles="$tagfiles $f" ;;
      w13-allow) w13=$((w13+1)) ;;
    esac
  done <<< "$all"
  (( w13 > 0 )) && info "$w13 处写在 transport/装配层但落在 §7.1 W13-1 白名单内（idempotency_keys/principals，不发事件不推版本），已裁定合法、不再要求下沉；闭集那一腿见 2b/8"
  (( fx > 0 )) && info "$fx 处跨包写在 _test.go 里（造外键父行，按 §1.3 允许，不判红）：$(printf '%s\n' $fxf | sort -u | tr '\n' ' ')"
  (( lits > 0 )) && bad "$lits 处：整个表名字符串出现在非归属包里（变量传表名的典型形状，需人工判是不是绕过了归属）：$(printf '%s
' $litwhat | sort -u | tr '
' ' ')"
  (( tags > 0 )) && info "另有 $tags 处表名只出现在 Go 结构体 tag 里（序列化名，不是表引用）：$(printf '%s\n' $tagfiles | sort -u | tr '\n' ' ')"
  (( reads > 0 )) && info "另有 $reads 处只读引用（§1.3/§4 允许跨模块读与 JOIN，不判越界）：$(printf '%s\n' $readfiles | sort -u | tr '\n' ' ')"
  (( n == 0 && reads == 0 && fx == 0 )) && info "没有任何包在自己的目录之外提到别人的表名"
  local ex
  ex="$(scan_go '\.Exec(Context)?\(' core | grep -v '/platform/db/' | grep -v '_test\.go:' || true)"
  if [[ -n "$ex" ]]; then
    while IFS= read -r line; do
      bad "业务包绕开平台层自行执行写语句（§1）: ${line%%:*}"
    done <<< "$ex"
  else
    info "非 platform/db 的生产代码 0 处 .Exec( —— 写路径没有旁门（_test.go 建 fixture 不算）"
  fi
  tailor
}

# --- check 2b: 写路径归属（契约③ §7.1 判据表 W13）---------------------------
# 判据本体住在 §7.1，这里只解析它，别处不再抄一份：
#   W13-1 transport/装配层允许写的表只有 idempotency_keys（Append/UpdateOnce）与 principals（Append）；
#   W13-2 那 3 处之外，core/api/** 或 core/cmd/core/** 写任何聚合表即越界；
#   W13-3 模式不得把 Service 方法当写表 —— 反向用例在 --selftest 里（没那条这就是空判据）。
# CO-36（主控 13:22 纠我）：W13 的"业务表全集"原来是我手抄的 13 张聚合表 —— 那是**新的假绿来源**：
# 迁移里新增第 34 张表时，它不在我清单上就会掉进 not-a-table 只报 INFO。
# 现在运行时从 core/migrations/*.sql 解析表全集，权威在文件里、脚本内不留副本（同 §7 的做法）。
# 解析出的集合只用来判"这是不是一张真表"；允许在 transport/装配层写的仍只有 W13_ALLOW 那几处。
biz_tables() {
  grep -rhoiE 'CREATE TABLE( IF NOT EXISTS)?[[:space:]]+`?[a-z_]+`?' core/migrations/*.sql 2>/dev/null \
    | sed -E 's/.*[`( ]([a-z_]+)`?$/\1/' | sort -u
}
MIG_TABLES="$(biz_tables)"
MIG_N="$(printf '%s\n' "$MIG_TABLES" | grep -c .)"

W13_ALLOW='idempotency_keys:Append
idempotency_keys:UpdateOnce
principals:Append'

# w13_hits <root...>：按 §7.1 的模式取命中（_test.go 造 fixture 允许，不算越界）。
# 单独成函数只为了让 selftest 和真检查跑的是同一个表达式，而不是各测各的。
w13_hits() {
  # strip_comments 这一道**必须在这里**，不是在下游：09-23 01:02 实测漏了它，backend 注释里那句
  # "这里出现 `Tx.Append(\"audit_entries\"` 字面量即 W13-2 红"被当成代码，造出一条**引用自己判据的假红**。
  # 同一条腿的假绿方向也一样成立（表写被删了而注释还留着 ⇒ 不 strip 就报绿）。
  { grep -rn --include='*.go' -E "$W13_RE" "$@" 2>/dev/null || true; } | grep -v '_test\.go:' \
    | strip_comments | grep -vE '^[^:]+:[0-9]+:[[:space:]]*$' || true
}

# w13_classify <file> <table> <method> → allow | table-red | out-of-scope | not-a-table | parse-broken
# W13-1 = 白名单（闭集，精确到「表+方法」）；W13-2 = 白名单之外、迁移里存在的任何表在此红。
w13_classify() {
  local f="$1" tbl="$2" meth="$3"
  printf '%s\n' "$W13_ALLOW" | grep -qxF "$tbl:$meth" && { printf 'allow'; return; }
  case "$f" in core/api/*|core/cmd/core/*) : ;; *) printf 'out-of-scope'; return ;; esac
  [[ -n "$MIG_TABLES" ]] || { printf 'parse-broken'; return; }
  printf '%s\n' "$MIG_TABLES" | grep -qxF "$tbl" && { printf 'table-red'; return; }
  printf 'not-a-table'
}

check_w13() {
  hdr "2b/9 transport/装配层写路径（契约③ §7.1 W13；CO-36 后拆成 W13-1 闭集白名单 + W13-2 一行一表）"
  local d
  for d in core/api core/cmd; do
    [[ -d "$d" ]] || { bad "§7.1 W13 扫描面缺 $d —— 这一腿会伪装成全绿，拒绝出结论"; return; }
  done
  local nfiles hits line f meth tbl key verdict
  nfiles="$(find core/api core/cmd/core -name '*.go' -not -name '*_test.go' 2>/dev/null | wc -l | tr -d ' ')"
  hits="$(w13_hits core/api core/cmd)"
  # 扫描面非空（CO-36 的核心教训）：目录在、里面却几乎没生产代码，这条腿同样是空的。
  (( nfiles >= 3 )) || { bad "W13 扫描面只有 $nfiles 个生产 .go（core/api + core/cmd/core）—— 空集上的「全绿」不是绿"; return; }
  # 解析面非空同理：迁移里读不出 >=30 张表，说明 biz_tables 的锚点坏了，W13-2 会整批漏判。
  (( MIG_N >= 30 )) || { bad "core/migrations 只解析出 $MIG_N 张表（期望 >=30）—— 表全集解析坏了，W13-2 判不了"; return; }
  report "  判据面：$MIG_N 张迁移表逐个扫 × transport/装配层 $nfiles 个生产 .go；白名单 3 处（W13-1 是闭集）"
  local n=0 allowed=0 tred=0 out=0 note=0
  while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    n=$((n+1))
    f="${line%%:*}"
    meth="$(printf '%s' "$line" | grep -oE '\.(Append|UpdateOnce|Touch|Create)\(' | head -1 | tr -d '.(')"
    tbl="$(printf '%s' "$line" | grep -oE '\.(Append|UpdateOnce|Touch|Create)\("[a-z_]+' | head -1 | sed -E 's/.*\("//')"
    key="$tbl:$meth"
    if [[ -z "$tbl" || -z "$meth" ]]; then
      bad "W13 命中却解析不出表名/方法（模式与解析脱节，会漏判）: $line"
      continue
    fi
    verdict="$(w13_classify "$f" "$tbl" "$meth")"
    case "$verdict" in
      allow)          allowed=$((allowed+1)); report "  [ok W13-1 白名单] $f  ${meth}(\"$tbl\")" ;;
      table-red)      tred=$((tred+1));   bad "W13-2 越界：$f 用 ${meth}(\"$tbl\") 写迁移里的表 —— 表写只能发生在归属包内，transport/装配层只做参数校验与投递（要豁免请改 §7.1 并说明理由，不许口头豁免）" ;;
      out-of-scope)   out=$((out+1));   info "W13-2 声明范围外命中：$f 写 ${tbl}（${meth}）。§7.1 只声明 core/api/** 与 core/cmd/core/**，这里不判红；要扩范围请改判据表" ;;
      not-a-table)    note=$((note+1)); info "模式命中但 \"$tbl\" 不是迁移里的任何表名（$f ${meth}）—— 按 W13-3 判为绿（多半是 Service 方法的字符串首参）" ;;
      parse-broken)   bad "W13 判不了 $tbl：迁移表全集解析为空（查 biz_tables 的锚点）" ;;
    esac
  done <<< "$hits"
  # W13-2 一行一表：每张迁移表在红区里的命中数都摊开给人看（CO-36：整批扫过才可读）。
  # 0 命中不等于"通过"，它等于"今天没人这么写"——所以这组数字必须和上面那行「判据面」一起读：
  # 面非空 + 逐表都扫过，0 才是有内容的 0。
  local t cnt
  while IFS= read -r t; do
    [[ -z "$t" ]] && continue
    case "$t" in idempotency_keys|principals) continue ;; esac   # 这两张走 W13-1 白名单
    cnt="$(printf '%s\n' "$hits" | grep -cE "\.(Append|UpdateOnce|Touch|Create)\(\"${t}\"" || true)"
    if (( cnt > 0 )); then
      bad "W13-2 表 $t：transport/装配层有 $cnt 处字面量写调用（逐条已在上面列出）"
    else
      report "  [W13-2] $t: 0"
    fi
  done <<< "$MIG_TABLES"
  info "命中 $n 处（扫描面 $nfiles 个生产 .go）：白名单 $allowed / 表越界 $tred / 范围外 $out / 非表名 $note"
  (( n == 0 )) && info "transport/装配层一处「字面量表名作首参」的写调用都没有 —— 这不是白名单生效，是还没人写（别当通过）"
  (( allowed == 0 && n > 0 )) && info "W13-1 白名单一条没命中，它那一腿暂无证据（现状列里记的 idempotency_keys/principals 那几处不见了或被移走）"
  tailor
}

# --- check 3: dirty paths vs ownership matrix -------------------------------
FROZEN='core/migrations/000001_core_schema.sql'
check_3() {
  hdr "3/9 变更文件 vs 角色所有权矩阵（契约③ §7；基线 $BASELINE）"
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
  hdr "4/9 core/go.mod 只准主控变；go.sum 按 T-11 放宽后的判据"
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
  hdr "5/9 .qoder/evidence/ 与 .scratch/ 不得进入 git 索引"
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
  hdr "6/9 依赖方向（eventjournal 不 import 业务包；platform/db 只允许 eventjournal；audit 不 import platform/db）"
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
  hdr "7/9 /api/v1 面变更的三处同步（契约③ §6.3：yaml + 契约③ + 契约① 事件目录）"
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

# 从 Go 源码里提 symbol -> §15.1 码名。锚点按参数给（domainCodes / protocolCodeNames）。

# --- check 8: 错误码一致（判据 = 契约② §15.2 末尾的算法 + §15.1.1 生产方表）----------
# T-45④ 定的形状：§15.1 是**协议词汇表**，不许按我方实现完成度裁剪（Z Code 没有写入口，
# WRITE_ENTRY_NONE/READ_ONLY 照样得存在）。所以：
#   · 门面只判三件 —— ①§15.1 ↔ §15.2 码名集合双向相等；②每个码在 §15.1.1 有生产方声明；
#     ③生产方是 Core:internal/X 的行，那个包里必须真有对应符号，且「有 Go 常量」列与磁盘一致、
#     数字与 protocol.go 逐项相等（§15.3 规则 3）。
#   · `Adapter` 行是 Core 只识别不生产（Symbol() 读对端 data.code），`result-frame` 永不上 wire，
#     两者都**不要求**常量 —— 给它们造没人发的常量才是问题。
SPEC2='specs/adapter-protocol/adapter-protocol-v0.1.md'
PROTO_GO='core/internal/adapter/protocol.go'
# c85_err_hits <码名> <root>：码名字符串落在 error 帧构造形状上的行（判据表 C8-5 静态半边）。
# 单独成函数是为了 --selftest 能拿合成件验它 —— 一条只能报"没命中"的 leg 是永真式。
c85_err_hits() {
  grep -rn --include='*.go' "\"$1\"" "$2" 2>/dev/null | strip_comments \
    | grep -v '_test\.go' | grep -E 'errReply|Error\{|NewErrorReply|ErrorCode' || true
}

check_8() {
  hdr "8/9 错误码一致（§15.1 ↔ §15.2 映射 ↔ §15.1.1 生产方/数字表 ↔ protocol.go）"
  [[ -f "$SPEC2" ]] || { bad "找不到 $SPEC2"; return; }
  local c1 c2 only1 only2 rows n1 n2
  c1="$(sed -n '/^### 15\.1/,/^#### 15\.1\.1/p' "$SPEC2" | awk -F'|' '/^\| `/ {gsub(/[ `]/,"",$2); print $2}' | grep -E '^[A-Z][A-Z_]{3,}$' | sort -u)"
  c2="$(sed -n '/^### 15\.2/,/^### 15\.3/p' "$SPEC2" | awk -F'|' '/^\| `/ {gsub(/[ `]/,"",$4); print $4}' | grep -E '^[A-Z][A-Z_]{3,}$' | sort -u)"
  n1="$(printf '%s\n' $c1 | grep -c .)"; n2="$(printf '%s\n' $c2 | grep -c .)"
  info "§15.1 = $n1 码，§15.2 来自列 = $n2 码（『来自』以 — 开头的行按形状天然不进集合，C8-7 不点名放行）"
  (( n1 >= 8 && n2 >= 8 )) || { bad "码集合提取异常（$n1/$n2，期望各 >=8）—— 判据会空转，先修 lint"; return; }
  only1="$(comm -23 <(printf '%s\n' $c1) <(printf '%s\n' $c2))"
  only2="$(comm -13 <(printf '%s\n' $c1) <(printf '%s\n' $c2))"
  for row in $only1; do bad "§15.1 有 $row 但 §15.2 无映射行（新增码两处必须同改）"; done
  for row in $only2; do bad "§15.2 来自列出现 $row，但 §15.1 没这个码"; done
  # §15.1.1：数字码 | 码 | 生产方 | 有 Go 常量
  rows="$(sed -n '/^#### 15\.1\.1/,/^### 15\.2/p' "$SPEC2" | awk -F'|' '/^\| `-3/ {
      for(i=2;i<=5;i++){gsub(" ","",$i); gsub("`","",$i)}; print $2"|"$3"|"$4"|"$5}')"
  local num nm producer hasc pkg dir real
  (( $(printf '%s\n' "$rows" | grep -c .) >= 8 )) || { bad "§15.1.1 生产方表提取异常——判据会空转"; return; }
  while IFS='|' read -r num nm producer hasc; do
    [[ -z "$nm" ]] && continue
    if [[ "$producer" != *协议层* ]] && ! printf '%s\n' "$c1" | grep -qxF "$nm"; then
      bad "§15.1.1 有 $nm，但 §15.1 没这个码（两张表漂移）"
    fi
    case "$num" in
      -320[0-9][0-9]) ;;
      -326[0-9][0-9]) case "$producer" in *协议层*) ;; *) bad "$nm 用预定义码 $num 却不是协议层行（§15.3 规则 1）" ;; esac ;;
      *) bad "$nm 的数字码 $num 不在 -32000~-32099，也不在协议层区间（§15.3 规则 2）" ;;
    esac
    case "$producer" in
      *Core:internal/*)
        pkg="$(printf '%s' "$producer" | sed -E 's/.*Core:(internal\/[a-z_]+).*/\1/')"
        # §15.1.1 要点 3：「有 Go 常量」列看的是 internal/adapter 里有没有这个名字
        # （词汇由协议包统一持有、由 process 触发），按生产方包搜会判出一条根本不存在的错。
        dir=core/internal/adapter
        [[ -d "$dir" ]] || { bad "$nm 声明生产方 $pkg，但目录不存在"; continue; }
        real=no; grep -rnF "\"$nm\"" "$dir" --include='*.go' 2>/dev/null | strip_comments | grep -qF "\"$nm\"" && real=yes
        # ↑ 必须过 strip_comments：常量删了而注释里还留着这个名字时，裸 grep 会给出假绿。
        # C8-3（判据表定稿）：正向 —— 生产方 Core:* 且表标『有 Go 常量』的行，
        # core/internal/adapter 里必须搜到该码名（常量表就是 protocol.go 的 map 字面量），判红。
        # 反向（表标 ✗ 却出现字符串）只报 INFO：字符串可能只是数字表的一行，契约未定义那种情形。
        [[ "$hasc" == "✓" && "$real" == "no" ]] && \
          bad "C8-3：$nm 生产方 $pkg 且标『有 Go 常量』，但 core/internal/adapter 里搜不到 \"$nm\""
        [[ "$hasc" == "✗" && "$real" == "yes" ]] && \
          info "  待判据：$nm 表说没常量，$dir 里出现该字符串（是常量还是数字表？契约未给判定命令）"
        # 数字与 protocol.go 的逐项相等**不在这里判**：§15.1.1 自己写明那张表的来源就是
        # `protocol_test.go` 的 codeTable（13:22 实读），adapter 已经在钉它；我重复实现只会
        # 多一处会漂移的判据（这一轮我在这条腿上烧了 5 次，全部记在案）。
        # 这里只保留 §15.3 规则 1/2 的区间判据与"表说有常量、包里就得有"的存在性判据。
        ;;
      *Adapter*|*result-frame*)
        [[ "$hasc" == "✓" ]] && info "  $nm：生产方 $producer 却标『有 Go 常量』——Core 只识别不生产，请确认不是投机常量"
        ;;
      *) bad "$nm 的生产方声明『$producer』不认识（合法值：Core:internal/…、Adapter、result-frame）" ;;
    esac
  done <<< "$rows"
  # T-51① 精确形状：两表行数**天然不等**（§15.1=17 / §15.1.1=19），判据是反向包含 §15.1 ⊆ §15.1.1，
  # 且差集必须**恰好**是那两个协议层码——不写 17==19，也不写集合相等。
  local extra missing
  extra="$(comm -13 <(printf '%s\n' $c1) <(printf '%s\n' "$rows" | cut -d'|' -f2 | sort -u) | sort)"
  [[ "$(printf '%s ' $extra)" == "INVALID_PARAMS METHOD_NOT_FOUND " ]] || \
    bad "§15.1.1 比 §15.1 多出的码不是恰好的 {INVALID_PARAMS, METHOD_NOT_FOUND}，实得：$(printf '%s ' $extra)"
  missing="$(comm -23 <(printf '%s\n' $c1) <(printf '%s\n' "$rows" | cut -d'|' -f2 | sort -u))"
  [[ -z "$missing" ]] || for row in $missing; do
    bad "§15.1 的 $row 在 §15.1.1 里没有行（缺数字与生产方声明）"
  done
  # T-51③：标 result-frame 的行若又写"有 Go 常量 ✓"，是表自相矛盾；
  # 而"CANCELLED 永不出现在 error 响应"这条要真测，先把静态一半钉住：任何生产代码
  # 拿它构造 error 回复即红（今天全仓无 ErrCancelled，实测过）。
  local rf
  while IFS='|' read -r num nm producer hasc; do
    [[ -z "$nm" ]] && continue
    case "$producer" in
      *result-frame*)
        [[ "$hasc" == "✓" ]] && bad "$nm 标成 result-frame（永不作 error 回复）却又标『有 Go 常量』，§15.1.1 自相矛盾"
        # 锚点用**号码**不用码名：CANCELLED 同时是 Task/Execution 的状态字符串，
        # 按码名搜必然假红（我第一版就红了）。真缺陷的形状是"别处拿这个号造 error 回复"。
        grep -rn -- "$num" core/internal core/cmd --include='*.go' 2>/dev/null \
          | grep -v '_test\.go' | grep -v 'internal/adapter/protocol' | grep -q . \
          && bad "$nm（$num）是结果帧专用码，却有别处生产代码引用该号造 error 回复（§15.3 规则 5）"
        # C8-5 的另一半（判据表当场命令的后一条）：`"CANCELLED"` 这个码名不得出现在
        # FakeAdapter 构造 error 帧的位置。按码名搜要带形状判据 —— 它同时是 Task/Execution
        # 的状态字符串，只有落在 error 构造那一侧才算违反。帧级那一半（$/cancel 的回执必须
        # 是结果帧、线上没有 error 键）在 core/test/adapter_contract_test.go 的
        # TestCancelReplyIsResultFrame 里对真子进程实测，本 lint 不重复实现。
        if [[ "$nm" == "CANCELLED" && -d core/cmd/fake-adapter ]]; then
          local total hits5
          total="$(grep -rn --include='*.go' "\"$nm\"" core/cmd/fake-adapter/ 2>/dev/null | grep -vc '_test\.go' || true)"
          hits5="$(c85_err_hits "$nm" core/cmd/fake-adapter/)"
          if [[ -n "$hits5" ]]; then
            while IFS= read -r line; do bad "C8-5：${line%%:*} 在构造 error 帧的位置上出现 \"$nm\"：${line#*:}"; done <<< "$hits5"
          elif (( total > 0 )); then
            info "C8-5 静态半边：core/cmd/fake-adapter/ 里 \"$nm\" 有 $total 处引用，都不在 error 帧构造形状上"
          else
            info "C8-5 静态半边：core/cmd/fake-adapter/ 的生产代码里没出现过 \"$nm\" —— 这项暂无证据，不是通过"
          fi
        fi
        ;;
    esac
  done <<< "$rows"
  tailor
}
# --- check 9: 事件目录接线探针（CO-35）---------------------------------------
# 主控 13:22 明确：这行**不是**"test 去实现的契约项"，是"backend 接线时漏没漏"的归属探针。
# 所以命中 0 的时候报 [SCOPE 之外] 请派归，而不是报缺陷 —— "没派过接线"和"接线接坏了"是两回事。
#
# 两处收紧，都写在这儿免得被人当成我擅自改判据：
# ① 主控给的命令是 `grep -rn <事件名> core/ specs/`，但只在 specs/ 命中 = 文档登记过、代码里没人发，
#    那同样不等于接上了 ⇒ 分开计数，**以 core/ 为准**。
# ② 名字本身今天也还不在契约里（13:2x 实测，全仓去掉 .git/node_modules）：
#      grep -rniI delivery_unacknowledged specs/ docs/  ⇒ 0 命中
#      grep -rn   "规则 6"                specs/ docs/  ⇒ 0 命中
#    事件名与那两点判据只存在于派发文本（T-50 / T-98③）里。所以我**不引用"§6 规则 6"当出处**，
#    只引用派发；等主控把名字回写进事件目录 §6，这条的期望值才算有了契约锚点（REQ-T13）。
check_9() {
  hdr "9/9 事件目录接线探针（CO-35；0 命中报「SCOPE 之外」不报缺陷）"
  local ev code doc n dn
  ev="delivery_unacknowledged"
  code="$(grep -rn -- "$ev" core/ --include='*.go' 2>/dev/null || true)"
  doc="$(grep -rnI -- "$ev" specs/ docs/ 2>/dev/null || true)"
  n=$([[ -n "$code" ]] && printf '%s\n' "$code" | wc -l | tr -d ' ' || echo 0)
  dn=$([[ -n "$doc" ]] && printf '%s\n' "$doc" | wc -l | tr -d ' ' || echo 0)
  if (( n == 0 )); then
    report "  [SCOPE 之外] core/ 扫到 0 行，这条判据不成立，请派归（发出方与消费方都没有；specs+docs $dn 行）"
    (( dn == 0 )) && info "  而且事件名本身还没回写进契约（REQ-T13）：$ev 在 specs/ docs/ 里 0 命中 ⇒ 这条探针现在只能对派发文本负责"
    return
  fi
  report "  core/ 命中 $n 行 ⇒ 按派发文本（T-50/T-98③）给的两点复核：①WS 断连时 payload 要带全三个游标字段 ②notification 的 dedup_key 必须带 seq 范围、不许固定值"
  local line
  while IFS= read -r line; do printf '    %s\n' "$line"; done <<< "$code"
  info "  这两点今天只报命中位置、不判红：期望值还没有契约锚点（等 REQ-T13 把名字与规则回写进事件目录 §6）"
  tailor
}

# --- selftest: the scanners must be able to fail ----------------------------
run_all() { check_1; check_2; check_w13; check_3; check_4; check_5; check_6; check_7; check_8; check_9; }

selftest() {
  hdr "SELFTEST（证明上面几条不是永真式）"
  local tmp; tmp="$(mktemp -d -p "$SCRATCH")" || exit 2
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
  # CO-36②：§7 的 "`core/`（根目录本身）" 那一行**不许**变成兜底归属。
  # 三条断言各管一个失效方向：键根本没注册、根下新文件落 UNOWNED、显式行仍然归 orchestrator。
  [[ -z "${OWNER[core/]+x}" ]] || { report "  [SELFTEST FAIL] `core/` 又被注册成归属键了 —— 根行当兜底会让所有未映射路径都判给主控"; fails=$((fails+1)); }
  [[ "$(owner_of core/zz.go)" == "UNOWNED" ]] || { report "  [SELFTEST FAIL] core/ 根下的新文件被判成「${OWNER[core/]:-?}」而不是 UNOWNED（CO-36② 的误判会复发）"; fails=$((fails+1)); }
  [[ "$(owner_of core/go.mod)" == "orchestrator" ]] || { report "  [SELFTEST FAIL] core/go.mod 不再归 orchestrator —— 收紧根行时把显式行也一起弄丢了"; fails=$((fails+1)); }
  (( ROOT_ROW_SEEN == 1 )) || info "  [INFO] §7 里没有 `core/` 根目录那一行了；这一腿暂时没东西可验（不是通过）"
  # 前置检查：脚本按自身位置推仓库根，换 cwd 不影响，所以只能把它拷进一个没有 core/internal
  # 的空仓库里验"扫描面为空必须 exit 2"，而不是在临时目录里调它（那样测的是同一份真仓库）。
  local bare; bare="$(mktemp -d -p "$SCRATCH")"
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
  local fx2; fx2="$(mktemp -d -p "$SCRATCH")/internal"
  mkdir -p "$fx2/task" "$fx2/quota"
  printf 'package task

const tblName = "quota_accounts"
func f() { _ = tblName }
' > "$fx2/task/dyn.go"
  printf 'package task

const q = `UPDATE quota_accounts SET label = ?`
' > "$fx2/task/w.go"
  # 结构体 tag 里的表名是**序列化名**，不是变量传表名的源头（今天实测的假阳性形状）。
  printf 'package task

type row struct {
	QA []string `json:"quota_accounts"`
}
' > "$fx2/task/tag.go"
  # 注释里的表名不算代码（主控 09-21：只看字符串字面量）。两侧都要断：
  # 纯注释必须被剥掉，行内注释后面的**真代码**不许被一起剥掉。
  printf 'package task\n\n// const t = "quota_accounts" 只是注释，不是写表也不是变量传表名\n'          > "$fx2/task/comment.go"
  printf 'package task\n\nfunc f() { _, _ = tx.Append("quota_accounts", m) // quota_accounts 注释\n}\n' > "$fx2/task/trailing.go"
  printf 'package quota

const q = `SELECT id FROM quota_accounts`
' > "$fx2/quota/own.go"
  local out; out="$(cross_package_refs "$(dirname "$fx2")/")"
  printf '%s
' "$out" | grep -qE '^write\|.*task/w\.go\|quota_accounts\|' ||     { report "  [SELFTEST FAIL] 按表名扫描抓不到 task 包写 quota_accounts"; fails=$((fails+1)); }
  printf '%s
' "$out" | grep -qE '^literal\|.*task/dyn\.go\|quota_accounts\|' ||     { report "  [SELFTEST FAIL] 表名以字符串形式存在（变量传参的源头）没被单列出来"; fails=$((fails+1)); }
  printf '%s\n' "$out" | grep -qE '^tag\|.*task/tag\.go\|quota_accounts\|' || \
    { report "  [SELFTEST FAIL] 结构体 tag 里的表名没被单列成 tag —— literal 腿会把序列化名判成写表（今天实测的第二条假阳性）"; fails=$((fails+1)); }
  printf '%s\n' "$out" | grep -qE 'comment\.go' && \
    { report "  [SELFTEST FAIL] 只在注释里出现的表名被当成了代码（strip_comments 没生效 ⇒ 假红与假绿都会来）"; fails=$((fails+1)); }
  printf '%s\n' "$out" | grep -qE '^write\|.*task/trailing\.go\|quota_accounts\|' || \
    { report "  [SELFTEST FAIL] 行内注释之后的真写调用被一起剥掉了（剥过头 ⇒ 漏报比误报更坏）"; fails=$((fails+1)); }
  printf '%s\n' "$out" | grep -qE '^literal\|.*task/tag\.go\|' && \
    { report "  [SELFTEST FAIL] tag 那一腿没生效：tag.go 仍被判成 literal"; fails=$((fails+1)); }
  printf '%s
' "$out" | grep -qE 'quota/own\.go' &&     { report "  [SELFTEST FAIL] 归属包自己引用自己的表被判进来了（判据过严）"; fails=$((fails+1)); }
  rm -rf "$(dirname "$fx2")"
  # §7.1 W13-3 反向用例：模式必须放过 Service 方法、必须抓住「字面量表名作首参」。
  # 只断红的那半边仍是空判据 —— 绿/红两边都要有断言，所以 classify 也各测一条。
  local w3; w3="$(mktemp -d -p "$SCRATCH")"
  mkdir -p "$w3/api"
  printf 'package http\n\nfunc (c *counter) do() {\n\t_, _ = c.Tasks.Create(ctx, in)\n\t_, _ = c.Sessions.Touch(ctx, id)\n}\n' > "$w3/api/service.go"
  printf 'package http\n\nfunc (h *handler) write() {\n\t_, _ = tx.Append("tasks", map[string]any{})\n}\n'              > "$w3/api/agg.go"
  # T-166/T-170 要的成对判决照 §7.1 写死：**同一张表 `tasks` 两种角色** ——
  # agg.go 里 `Append("tasks"` 是真写 ⇒ 必须红；comment.go 里同一字面量只出现在注释 ⇒ 必须不红。
  # （09-23 01:02 真炸过的那条假红来自 backend 用注释复述本规则：`// 这里出现 Tx.Append("audit_entries"…`，
  #   所以第二种形状也保留，它是这条判据的"作者写规则"用例。）
  printf 'package http\n\n// 这里出现 Tx.Append("tasks" 字面量即 W13-2 红）\n// 以及真实那次：Tx.Append("audit_entries"\n' > "$w3/api/comment.go"
  printf 'package http\n\nfunc (h *handler) w2() {\n\t_, _ = tx.Append("sessions", m) // sessions 是注释\n}\n'          > "$w3/api/trailing.go"
  local wn; wn="$(w13_hits "$w3" | wc -l | tr -d ' ')"
  (( wn == 2 )) || { report "  [SELFTEST FAIL] W13 模式命中 $wn 处，期望 2（agg.go + trailing.go）—— 注释里的表名又被当成代码，或者行内注释后的真调用被剥过头"; fails=$((fails+1)); }
  local wh; wh="$(w13_hits "$w3")"
  printf '%s\n' "$wh" | grep -q 'comment\.go' && { report "  [SELFTEST FAIL] 只在注释里出现的 Append(\"tasks\" / Append(\"audit_entries\" 被 W13 判成写调用（w13_hits 里 strip_comments 没生效）"; fails=$((fails+1)); }
  printf '%s\n' "$wh" | grep -q 'agg\.go' || { report "  [SELFTEST FAIL] 真写 Append(\"tasks\"（agg.go）没被判红 —— 与上一条合起来就是「成对判决」塌了一边"; fails=$((fails+1)); }
  printf '%s\n' "$wh" | grep -q 'trailing\.go' || { report "  [SELFTEST FAIL] 行内注释之后的真写调用没被 W13 抓到（剥过头 ⇒ 漏报比误报更坏）"; fails=$((fails+1)); }
  # T-166 要的是"两个判决都打出来"：只在失败时才有输出，等于让"没红"和"没判"长一样。
  info "  [W13 注释自证·成对] 同一张表 tasks：真写(agg.go)判红、只出现在注释(comment.go)不红；trailing.go 行内注释之后的真写判红；comment.go 里 audit_entries 的注释复述不红（合成树命中 $wn 处）"
  # （原来这条是 `wn == 1`；09-23 加了 comment.go / trailing.go 两个形状之后期望变 2，
  #   而"Service 调用不算写表"那一半由下面的 wz == 0 单独守着，不在这里重复计数。）
  # CO-36 的 W13-3 另一半（反向断言本身也要被反向验一次）：整棵树只有 Service 形状时命中必须是 **0**。
  # 只有"混合目录里命中 1 处"那半边证明不了什么 —— 模式若还残留 .Create( 这种宽形状，
  # 那 1 处命中会把它一起糊过去。0 这一侧单独造一棵树才测得到。
  local w3o wz; w3o="$(mktemp -d -p "$SCRATCH")"
  printf 'package http\n\nfunc (c *counter) f() {\n\t_, _ = c.Tasks.Create(ctx, in)\n\t_, _ = c.Sessions.Touch(ctx, id)\n\t_, _ = q.UpdateOnce(ctx, id)\n}\n' > "$w3o/service_only.go"
  wz="$(w13_hits "$w3o" | wc -l | tr -d ' ')"
  (( wz == 0 )) || { report "  [SELFTEST FAIL] 纯 Service 形状的树里 W13 命中 $wz 处，期望 0 —— W13-3 的反向断言没成立"; fails=$((fails+1)); }
  rm -rf "$w3o"
  [[ "$(w13_classify core/api/http/x.go tasks Append)" == "table-red" ]] ||                 { report "  [SELFTEST FAIL] W13-2：transport 层写业务表没判红"; fails=$((fails+1)); }
  [[ "$(w13_classify core/api/http/x.go idempotency_keys UpdateOnce)" == "allow" ]] ||          { report "  [SELFTEST FAIL] W13-1 白名单里声明过的写法被判红了"; fails=$((fails+1)); }
  [[ "$(w13_classify core/api/http/x.go principals UpdateOnce)" != "allow" ]] ||                { report "  [SELFTEST FAIL] 白名单是「表+方法」级的（principals 只准 Append），方法级收紧没生效"; fails=$((fails+1)); }
  [[ "$(w13_classify core/api/http/x.go quota_accounts Touch)" == "table-red" ]] ||             { report "  [SELFTEST FAIL] 聚合表走 Touch 没算越界（表全集没进判据）"; fails=$((fails+1)); }
  [[ "$(w13_classify core/api/http/x.go schema_migrations Append)" == "table-red" ]] ||         { report "  [SELFTEST FAIL] 白名单外的迁移表写调用没判红（W13-2 的「一行一表」没生效）"; fails=$((fails+1)); }
  # CO-36 的解析活性：表全集必须从迁移里解析出来，且必须含主控点名的那两张。
  # 少了这两条，"每表 0 命中"就可能只是解析器什么都没读到 —— 那正是 CO-36 说的那种假绿。
  (( MIG_N >= 30 )) || { report "  [SELFTEST FAIL] 只解析出 $MIG_N 张迁移表（期望 >=30），biz_tables 的锚点坏了"; fails=$((fails+1)); }
  printf '%s\n' "$MIG_TABLES" | grep -qxF messages || { report "  [SELFTEST FAIL] 表全集里没有 messages —— CO-36 点名的假绿会复发"; fails=$((fails+1)); }
  printf '%s\n' "$MIG_TABLES" | grep -qxF handoffs || { report "  [SELFTEST FAIL] 表全集里没有 handoffs —— 同上"; fails=$((fails+1)); }
  [[ "$(w13_classify core/api/http/x.go notATable Create)" == "not-a-table" ]] ||               { report "  [SELFTEST FAIL] Service 方法的字符串首参被判成写表（W13-3 要求这里是绿）"; fails=$((fails+1)); }
  [[ "$(w13_classify core/cmd/fake-adapter/main.go tasks Append)" == "out-of-scope" ]] ||       { report "  [SELFTEST FAIL] §7.1 声明范围（core/api/**、core/cmd/core/**）之外的命中被判红了"; fails=$((fails+1)); }
  rm -rf "$w3"
  # C8-5 静态半边能不能咬人：error 构造形状里的 "CANCELLED" 必须命中，状态侧必须不命中。
  local c5 c5h; c5="$(mktemp -d -p "$SCRATCH")"
  printf 'package fake\n\nfunc (f *fake) g(req *adapter.Frame) error {\n\treturn f.reply(errReply(req, &adapter.Error{Code: -32010, Message: "CANCELLED"}))\n}\n' > "$c5/bad.go"
  printf 'package fake\n\nvar s = "CANCELLED" // Execution 的状态，不是错误码\n'                                                     > "$c5/ok.go"
  c5h="$(c85_err_hits CANCELLED "$c5")"
  [[ "$c5h" == *bad.go* ]] || { report "  [SELFTEST FAIL] C8-5 静态半边抓不到 error 帧构造里的 \"CANCELLED\"（永真式）"; fails=$((fails+1)); }
  [[ "$c5h" != *ok.go* ]] || { report "  [SELFTEST FAIL] C8-5 静态半边把状态字符串误判成错误帧（假阳性）"; fails=$((fails+1)); }
  rm -rf "$c5"
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

# T-52：一次性产物（含 mktemp 出来的自测夹具）只准落在仓库内被 ignore 的角色目录，
# 不碰整机共享 Temp；删除只针对自己刚建的那一个具体路径，绝不通配。
SCRATCH="$ROOT/.scratch/test"
mkdir -p "$SCRATCH" || { printf "无法建 %s
" "$SCRATCH" >&2; exit 2; }

load_owner_table
# --explain <path>: 打印某个路径判给谁、命中哪条规则（被 lint 点名时先跑这个自查）
if [[ "${1:-}" == "--rules" ]]; then
  for k in "${!OWNER[@]}"; do printf '[%s] -> %s\n' "$k" "${OWNER[$k]}"; done | sort; exit 0
fi
if [[ "${1:-}" == "--explain" ]]; then
  shift
  for arg in "$@"; do
    hit=""
    for pref in "${!OWNER[@]}"; do
      case "$arg" in "$pref"*) [[ ${#pref} -gt ${#hit} ]] && hit="$pref" ;; esac
    done
    for pref in "${!OWNER_HOLE[@]}"; do
      case "$arg" in "$pref"*) [[ ${#pref} -gt ${#hit} ]] && hit="§7 空洞补偿表 $pref" ;; esac
    done
    printf '%-52s -> %-12s [%s]\n' "$arg" "$(owner_of "$arg")" "${hit:-无规则命中}"
  done
  exit 0
fi
if (( SELFTEST )); then selftest; exit $?; fi

report "Lucero ownership lint  baseline=$BASELINE  HEAD=$(git rev-parse --short HEAD)  $(date -Iseconds)"
report "  扫描面：core/ 下 $SCAN_GO 个 .go 文件（含 cmd/ 与 api/；T-41③ 扩面：transport 层直查表以前看不见）"
report "  归属表：$OWNER_ROWS 条规则，运行时解析自 $API_SPEC §7（无内嵌副本；--explain <path> 可自查判定依据）"
run_all
hdr "结论"
if (( VIOL > 0 )); then report "  $VIOL 项越界（见上）"; exit 1
else report "  本次扫描 0 项越界。注意 INFO 行：命中数为 0 的检查可能只是对应实现尚未落地。"; exit 0; fi
