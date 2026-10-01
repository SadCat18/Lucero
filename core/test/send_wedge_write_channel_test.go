package test

// REQ-S-1（甲，T-437 裁定）：一条被 adapter 挂住的 `session/send` **不许**把写通道占住。
//
// 形状按主控给的三条边界来，一条都不打折：
// 　① **打到真 handler 层**：起的是真 `cmd/core` 子进程、真 SQLite、真 HTTP —— 请求从
//    `POST /api/v1/executions/{id}/send` 进 `api/http` 的路由，不是直接调 Service（那条腿不算覆盖面）。
//    装配**不是我复刻的**：与我 09-27 给甲写的"复刻装配"相比，这一版更强 —— 生产装配本身在被测。
// 　② 本地一律 `-timeout 90s`（见文末命令），且**子进程残留按 pid 单独查**（`tasklist /FI "PID eq …"`），
//    不按进程名数 —— 名字数会把别人起的同名进程算进来、也会漏掉换了名字的，那是我 09-2x 报过的假绿形状。
// 　③ 这一条**不写 `t.Skip`**：起不来就 `t.Fatalf` 明写"采不到"，因为 `--- SKIP` 不算样本
//    （T-410 那枚被 SKIP 遮了几天的红就是前车之鉴）。它也不需要 `LUCERO_TEST_FAKE_ADAPTER` 旗标 ——
//    挂住的那台是孩子进程本身（本测试二进制），不是 fake-adapter。
//
// 为什么用"握手都不回"的孩子而不是真 fake + `debug/hang`（实测，不是省事）：
//    Core 侧**没有** `debug/*` 的 HTTP 路由（`grep -rn "debug/" core/api/http/*.go` 非测试零命中），
//    从外面 arm 不上那扇门；而真 fake 的 `session/send` 回执来得很快（T-392① 之后回执先于
//    `turn/finished`）⇒ 阻塞窗口≈0，那条"1 秒内 201"的断言会**空过**。
//    所以要一个真的会挂住的往返：`-adapter hang=<本二进制>` 那台孩子不回任何东西（含握手），
//    crash_test 已证这样的请求会真的挂在那里 —— 那才是这条判据要钉的东西。
//
// 数的是"这条无关写请求在 send 还挂着的时候多久拿到 201"，量级 1s 由主控定（N=1s，我不改数）。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const sendWedgeBudget = time.Second // 主控定的量级，不是我自己挑的数

// pidGone 按 **pid** 问操作系统"这个进程还在不在"，不扫进程名。
func pidGone(t *testing.T, pid int) bool {
	t.Helper()
	out, err := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/NH").Output()
	if err != nil {
		t.Fatalf("tasklist 查 pid=%d 失败：%v ⇒ 残留这一腿采不到，不算它通过", pid, err)
	}
	return !strings.Contains(string(out), fmt.Sprintf("%d", pid))
}

// TestSendRoundTripDoesNotHoldTheWriteChannel 是契约① 第 23 条那格的机器断言。
func TestSendRoundTripDoesNotHoldTheWriteChannel(t *testing.T) {
	exe := buildCore(t)
	dir := t.TempDir()
	s := seedCrashFixture(t, dir) // 进程外把 RUNNING 的 Execution 备好（今天没有 HTTP 入口能造它）
	pidFile := filepath.Join(dir, "adapter.pid")

	r := &coreRun{t: t, exe: exe, dir: dir,
		args: []string{"-adapter", "hang=" + os.Args[0]},
		env:  []string{envAdapterChild + "=" + pidFile}}
	r.boot()
	if h := health(t, r.e); h["state"] != "READY" {
		t.Fatalf("Core 没就绪，采不到样本：state=%v", h["state"])
	}

	// ① 挂住的那一发：不等待它返回，先看写通道会不会被它带倒。
	sendDone := make(chan apiResult, 1)
	go func() {
		sendDone <- apiCall(r.e, "POST", "/api/v1/executions/"+s.hangExecution+"/send",
			"local-user", "k-reqs1-send", `{"text":"wedge","delivery":"start_now"}`)
	}()
	pid := r.awaitPID(pidFile, 15*time.Second) // 精确拿到"这一台"孩子的 pid

	// ② 同一时刻的一条无关写：必须自己走完，不被上面那一发拖着。
	t0 := time.Now()
	res := apiCall(r.e, "POST", "/api/v1/projects", "local-user", "k-reqs1-proj",
		`{"name":"REQ-S-1 探针","canonical_path":"d:/probe/req-s-1"}`)
	dt := time.Since(t0)

	// 采样条件本身也要断：send 这会儿**确实还挂着**（否则这一跑什么都没测）
	stillWedged := false
	var sr apiResult
	select {
	case sr = <-sendDone:
		if sr.status == http.StatusRequestTimeout || sr.status == http.StatusServiceUnavailable {
			stillWedged = true // Core 侧超时/无连接的快速失败，也算"这一发走到了 adapter 往返"
		}
	case <-time.After(200 * time.Millisecond):
		stillWedged = true
	}

	t.Logf("REQ-S-1：send 那一发（execution=%s）%s；无关写 POST /api/v1/projects = %d，用时 %s；采样条件成立=%v",
		s.hangExecution, statusOrErr(sr), res.status, dt.Round(time.Millisecond), stillWedged)

	if !stillWedged {
		t.Errorf("采样条件没成立：send 那一发在 %v 内已经返回（%s）⇒ 这一跑没有\"挂着的往返\"，"+
			"1s 的断言是空过，不接受这种绿", dt, statusOrErr(sr))
	} else if dt > sendWedgeBudget {
		t.Errorf("send 还挂着的时候，无关写用了 %s 才回来（%d）⇒ 超过主控定的量级 %s："+
			"写通道被网络往返占住了（契约① 第 23 条）", dt.Round(time.Millisecond), res.status, sendWedgeBudget)
	}
	if res.status != http.StatusCreated {
		t.Errorf("无关写应当 201，实得 %d：%s", res.status, tailOf(res.body, 300))
	}

	// ③ 残留：先让孩子退出（杀 pid），再按 pid 单独查，不扫进程名。
	killPID(t, pid)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && !pidGone(t, pid) {
		time.Sleep(100 * time.Millisecond)
	}
	if !pidGone(t, pid) {
		t.Errorf("adapter 子进程 pid=%d 在 Core 收口之后 10s 仍然存在 ⇒ 连接收尾漏了这台（第 19 条那一族）", pid)
	}
	select {
	case sr = <-sendDone:
		t.Logf("孩子被杀之后 send 那一发返回：%s", statusOrErr(sr))
	case <-time.After(20 * time.Second):
		t.Errorf("杀掉 adapter 之后 20s，那一发 send 还没有返回 ⇒ Core 挂在了它上面")
	}
	var ep map[string]any
	if b, err := os.ReadFile(filepath.Join(dir, "core.endpoint")); err == nil {
		_ = json.Unmarshal(b, &ep)
	}
	t.Logf("收尾核对：endpoint=%v（Core 应还活着；残留核查=%s）", ep, "按 pid 查，未使用进程名计数")
}

func statusOrErr(r apiResult) string {
	if r.err != nil {
		return fmt.Sprintf("请求本身失败：%v", r.err)
	}
	return fmt.Sprintf("HTTP %d", r.status)
}

// 本地跑法（边界②里的 90s）：
//   cd core && CGO_ENABLED=0 go test ./test/ -count=1 -timeout 90s -v -run '^TestSendRoundTripDoesNotHoldTheWriteChannel$'
