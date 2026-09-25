#!/usr/bin/env bash
# build_stamp.sh —— 给"一批采样"盖一个**可复用的可归因戳**：构建时刻 + HEAD + 编译输入指纹。
# 为什么要有它（契约① §1.1 第 19 条验收② 的补句，2026-09-24 主控收编）：
#   **采样必须同时交"被验树指纹 + 构建时刻"，没有指纹的 0 红不算证据。**
#   出生现场：同一条命令、同一版 `lock.go`（旧的那版）空机上串行 122 次可以一次不红 ⇒
#   "跑之前是不是那版码"这件事必须机器算，不能靠采样的人回忆；而 FL-8 那档问的是
#   "窗口里有没有人在改"，这一格问的是"**改的是不是被验的那份码**"——两问不同，都要在。
# 判据本体不在这里（这里只产读数）；谁都能单独调：
#   bash scripts/build_stamp.sh                 # 默认 ./cmd/core ./test
#   bash scripts/build_stamp.sh ./api/http      # 换被验包
#   eval "$(bash scripts/build_stamp.sh --sh)"  # 要变量不要那行文本时
# Exit：0 = 取到了；2 = 清单没取到 / go 不在（**这时宁可 exit 2 也不给一个"看着像"的戳**）。
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MOD="$ROOT/core"                     # go.mod 在 core/ 下，包路径要相对那一层
cd "$MOD" || { printf '进不去 Go 模块目录 %s\n' "$MOD" >&2; exit 2; }
GO="${LUCERO_GO:-D:/DevTools/go/bin/go.exe}"; [[ -x "$GO" ]] || GO=go
[[ -n "$(command -v "$GO" 2>/dev/null)" ]] || { printf '找不到 go（%s）\n' "$GO" >&2; exit 2; }

AS_SH=0
[[ "${1:-}" == "--sh" ]] && { AS_SH=1; shift; }
PKGS=("$@"); [[ ${#PKGS[@]} -eq 0 ]] && PKGS=(./cmd/core ./test)
MIN_FILES="${LUCERO_STAMP_MIN_FILES:-50}"   # 一个真依赖图至少几百个；低于它就是清单没取到

# ⚠ 模板必须是 `{{$.Dir}}/{{.}}`。写成 `{{$_.Dir}}` 时 go list 报 `undefined variable "$_"`，
#    而这条管道的报错被咽在下游 ⇒ 清单为空 ⇒ 指纹是对**空集**算的 ⇒ **前后必然相等**，
#    那就是一条永真的门。下面的 files 计数闸就是为它而设（09-24 07:3x 我自己差点交出去）。
list_inputs() {
  "$GO" list -e -deps -f '{{range .GoFiles}}{{$.Dir}}/{{.}}
{{end}}{{range .TestGoFiles}}{{$.Dir}}/{{.}}
{{end}}{{range .XTestGoFiles}}{{$.Dir}}/{{.}}
{{end}}' "${PKGS[@]}" 2>/dev/null | sed 's/\\/\//g' | sort -u
}

STAMP_AT="$(date -Iseconds)"
LIST="$(list_inputs)"
N_FILES="$(printf '%s' "$LIST" | grep -c .)"
if (( N_FILES < MIN_FILES )); then
  printf 'build_stamp: 只数到 %s 个编译输入（阈值 %s，包=%s）⇒ 清单没取到，拒绝给戳。\n' \
    "$N_FILES" "$MIN_FILES" "${PKGS[*]}" >&2
  printf '  先单独跑一次看报错：cd core && go list -e -deps %s\n' "${PKGS[*]}" >&2
  exit 2
fi
# 逐文件 md5 再聚一次：任何一字节变动（包括别人把未提交的在途码换进去）都会换掉这个戳。
FP="$(printf '%s\n' "$LIST" | tr '\n' '\0' | xargs -0 md5sum 2>/dev/null | md5sum | cut -c1-32)"
HEAD="$(cd "$ROOT" && git log -1 --format=%h 2>/dev/null || echo nogit)"
DIRTY="$(cd "$ROOT" && git status --porcelain 2>/dev/null | wc -l)"   # 用 wc -l：grep -c 数到 0 时自己也 exit 1，会把"0"印两遍

if (( AS_SH == 1 )); then
  printf 'STAMP_AT=%s\nSTAMP_HEAD=%s\nSTAMP_DIRTY=%s\nSTAMP_FILES=%s\nSTAMP_FP=%s\n' \
    "$STAMP_AT" "$HEAD" "$DIRTY" "$N_FILES" "$FP"
else
  printf '构建时刻=%s HEAD=%s 工作树未提交=%s处 编译输入=%s个 指纹=%s 包=[%s]\n' \
    "$STAMP_AT" "$HEAD" "$DIRTY" "$N_FILES" "$FP" "${PKGS[*]}"
fi
