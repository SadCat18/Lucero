#!/usr/bin/env bash
# FIRST SPRINT §7 的 Crash 清单跑法。owner: test 角色。
# 说明：本机放弃了 sqlite3 CLI（见 orchestration 账本「现实约束」），
# 所以 PRAGMA integrity_check 由 Go 侧执行——不要在这脚本里再找一个 sqlite3。
#
# 用法:  bash scripts/crash_recovery.sh
set -uo pipefail
GO="${GO:-/d/DevTools/go/bin/go.exe}"
CORE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../core" && pwd)"

cd "$CORE_DIR" || exit 2
export CGO_ENABLED=0 PATH="$(dirname "$GO"):$PATH"

echo "# crash 相关用例（$($GO version))"
# 命令写成锚定形（契约① §1.1 第 15 条）：`-run TestCrash` 是无锚定子串匹配，
# 今天恰好只有这一族，明天别人加一个 `TestCrashRecoveryWhatever` 就会被静默算进我的清单。
# 整族是**故意**的，所以用 `^(TestCrash…)$` 而不是 `-run TestCrash`；包路径也写死到 ./test/。
$GO test -count=1 -v -run '^(TestCrash[A-Za-z0-9]*)$' ./test/ 2>&1 | grep -E '^(--- |ok|FAIL)'
status=${PIPESTATUS[0]}

echo
echo "# 未落地的 §7 条目（SKIP 就是 blocked，别当通过）"
echo "#   kill Core 后识别 interrupted/recoverable  -> 等 cmd/core + Recovery Engine（backend）"
echo "#   kill Adapter 不带崩 Core                  -> 等 adapters/fake + Process Supervisor（adapter）"
echo "#   重启后不重复发消息（Idempotency-Key）      -> 等 cmd/core 的 HTTP 写端点（backend）"
echo "#   Approval 不丢                             -> 等 cmd/core + approval Service（backend/audit）"
echo "#   Event Journal 不丢（seq 连续性）           -> 已覆盖提交集完整性，WS 补发等 backend"
echo "#   PRAGMA integrity_check ok                 -> 已由 TestCrashProcessDeathKeepsDurableData 真跑"
exit "$status"
