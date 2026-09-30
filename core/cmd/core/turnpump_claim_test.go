package main

// `turn/claimed` ⇒ `executions.provider_execution_id` 那一条腿的判据（T-408 ① 的发射位那一半）。
//
// 三条硬时机里，域那一侧管住的是"这一笔写什么"（`claim_test.go`），**时机只有泵管得到**：
// 它是全仓唯一看见 `turn/claimed` 帧的地方。所以这里判两条：
//   - 只在 claimed 那一刻 ⇒ 认出 claimed 就动手，认出 dispatched **一次都不动手**
//     （第二条硬时机"不许在 dispatched 补写"的机器形状）；
//   - 落不落成都要出声，且出声必须与库里一致（配对断言，与崩溃那一支同一手法）——
//     今天 `execution_claimed` 还不在平台事件目录里（三处同改的另两半在 db/主控/frontend 手上），
//     所以今天走的是"没落成"那一支；登记之后同一用例自动改走"已落"那一支，**不需要改测试**。
//
// 用不用真子进程两条都跑：第一条不打进程（合成帧，判的是"哪一帧触发"），
// 第二条走真 fake-adapter（判的是"真帧的形状确实被认出来了"，并且 claimed 与 dispatched 带同一个把手时
// 也只动一次手）。

import (
	"strings"
	"testing"
	"time"

	"github.com/SadCat18/lucero/core/internal/adapter"
	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/process"
)

func pumpHandle(t *testing.T, d *db.DB, id string) string {
	t.Helper()
	var p string
	if err := d.Read.QueryRow(`SELECT COALESCE(provider_execution_id, '') FROM executions WHERE id = ?`, id).
		Scan(&p); err != nil {
		t.Fatalf("读 %s 的 provider_execution_id: %v", id, err)
	}
	return p
}

// claimAttempts 数"泵对这一列动过几次手"：三种结局（已写入 / 没落成 / 已是同一把手）都算一次动手，
// 它们共用那一句 `claimed 落库` 就是**等待谓词与被断言的谓词同一件事**（老规矩）。
// 而"没带把手""没接落库面"那两种**不算**动手（它们连事务都没开）。这一计数就是 dispatched 那格的牙。
func claimAttempts(tap *logTap) int {
	return tap.count("claimed 落库")
}

// seedClaimRow 播一条 RUNNING 的在飞执行（合成帧那条用例的输入；与 armHoldAndSend 同一张形状，
// 只是不需要真连接 —— 所以要自己把那三行补齐：`newBareDiscover` 不跑 probe，实例行不在库里）。
func seedClaimRow(t *testing.T, d *db.DB, id string) {
	t.Helper()
	if _, err := d.Read.Exec(`INSERT INTO adapter_instances (id, adapter_key, display_name, health, created_at, updated_at)
		VALUES ('ad-fake', 'fake', 'Fake Adapter', 'HEALTHY', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Read.Exec(`INSERT INTO tasks (id, title, status, created_at, updated_at)
		VALUES (?, '把手这一格', 'RUNNING', 1, 1)`, "task-"+id); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Read.Exec(`INSERT INTO executions (id, task_id, adapter_instance_id, attempt_no,
		status, state_confidence, trigger_type, created_at, updated_at)
		VALUES (?, ?, 'ad-fake', 1, 'RUNNING', 'HIGH', 'INITIAL', 1, 1)`, id, "task-"+id); err != nil {
		t.Fatal(err)
	}
}

func TestPumpClaimsTheHandleOnlyOnTheClaimedFrame(t *testing.T) {
	_, _, pipe, d := newBareDiscover(t, []adapterSpec{{key: "fake", displayName: "Fake Adapter", command: "不需要孩子"}})
	hub := pipe.hub
	var tap logTap
	hub.logf = tap.f
	seedClaimRow(t, d, "ex-c")
	hub.watch("fake", "ex-c")

	claim := func(method, provider string) {
		hub.handle("fake", 1, process.Inbound{ExecutionID: "ex-c",
			Frame: &adapter.StreamFrame{Method: method, ProviderExecutionID: provider}})
	}

	claim(adapter.MethodTurnClaimed, "prov-p1")
	if n := claimAttempts(&tap); n != 1 {
		t.Fatalf("claimed 那帧该动手恰一次，实际 %d 次：%v", n, tap.lines)
	}
	// 配对：喊落了 ⇒ 列里有值 + 恰一条事件；喊没落成 ⇒ 列还是 NULL + 零事件。两种都不许悄悄。
	landed := tap.count("已写入 provider_execution_id")
	got := pumpHandle(t, d, "ex-c")
	switch {
	case landed == 1:
		if got != "prov-p1" {
			t.Errorf("喊了已落而库里是 %q ⇒ 半真历史", got)
		}
		if n := pumpEventsOf(t, d, "execution_claimed"); n != 1 {
			t.Errorf("落了就该恰一条 execution_claimed，实际 %d 行", n)
		}
		if st := pumpExecStatus(t, d, "ex-c"); st != "RUNNING" {
			t.Errorf("这一笔不许改状态（硬时机第三条），实际 %s", st)
		}
	case landed == 0:
		if got != "" {
			t.Errorf("没喊已落却把列写成了 %q ⇒ 那是「只改列不发事件」那一形", got)
		}
		if n := pumpEventsOf(t, d, "execution_claimed"); n != 0 {
			t.Errorf("落不成就不许留下 execution_claimed 的行，实际 %d 行", n)
		}
		if n := tap.count("落库：没落成"); n != 1 {
			t.Errorf("落不成必须出声，实际 %d 句：%v", n, tap.lines)
		}
	default:
		t.Fatalf("动手的喊话只能在一侧，已落 %d 句（日志：%v）", landed, tap.lines)
	}

	// 硬时机第二条的正身：dispatched 带着**另一个**把手进来，一次手都不许动。
	// （真对端两条帧带的是同一个值，所以这里特意用异值 —— 同值的话"补写"那种改法也能蒙过去。）
	before := claimAttempts(&tap)
	claim(adapter.MethodTurnDispatch, "prov-late")
	if n := claimAttempts(&tap); n != before {
		t.Errorf("dispatched 帧触发了补写（动手次数 %d → %d）⇒ :741 第二条硬时机失效", before, n)
	}
	if got := pumpHandle(t, d, "ex-c"); got == "prov-late" {
		t.Error("把手被 dispatched 那一帧换掉了 ⇒ 补写真的落了库")
	}
	// 收尾帧不许被当成 claimed（它根本没有把手那一格）。
	claim(adapter.MethodTurnFinished, "")
	if n := claimAttempts(&tap); n != before {
		t.Errorf("收尾帧也去动把手列了（%d → %d）", before, n)
	}
}

// TestPumpClaimsTheHandleOverRealAdapter 走真子进程：fake-adapter 一轮里发出 claimed 与 dispatched
// **同一个把手**（`core/cmd/fake-adapter/main.go:542~547`）。这里要的是两件合成帧给不了的证据：
//   - 真帧上那个字段被认出来了（值不是测试自己塞进 `StreamFrame` 的）；
//   - 两条帧都到了，也只动手一次（第二条就是"dispatched 不补写"在真链路那一面）。
func TestPumpClaimsTheHandleOverRealAdapter(t *testing.T) {
	pipe, tap, d, sess := deathFixture(t)
	armHoldAndSend(t, pipe, d, "ex-prov", "task-prov", sess)
	// 等的是 `claimed 落库` 那一声 —— 它是三种结局共用的前缀，所以**等待谓词就是被断言的谓词**：
	// 等到它就等于那一笔写已经跑完（不能等 `claimed 带回`：那是观察面那一腿，先于这笔写）。
	if !tap.wait(t, "claimed 落库", 20*time.Second) {
		t.Fatalf("没等到把手那一笔的结局一声 ⇒ 泵没写这一列：%v", tap.lines)
	}
	if n := claimAttempts(tap); n != 1 {
		t.Errorf("真链路上两条帧（claimed + dispatched）该只动手一次，实际 %d 次：%v", n, tap.lines)
	}
	landed := tap.count("已写入 provider_execution_id")
	got := pumpHandle(t, d, "ex-prov")
	switch {
	case landed == 1:
		if got == "" {
			t.Errorf("喊了已落而库里那一列还是 NULL：%v", tap.lines)
		}
		// 列里的值必须就是泵从那帧上读到的那个（不许是测试编的）。
		shout := tap.lineWith("claimed 带回 provider_execution_id")
		if !strings.Contains(shout, "="+got) {
			t.Errorf("库里是 %q 而观察面读到的是另一份，那一声是 %q", got, shout)
		}
		if n := pumpEventsOf(t, d, "execution_claimed"); n != 1 {
			t.Errorf("恰一条 execution_claimed，实际 %d 行", n)
		}
		if st := pumpExecStatus(t, d, "ex-prov"); st != "RUNNING" {
			t.Errorf("被 hold 扣住的这一条不许被 claimed 改状态，实际 %s", st)
		}
	case landed == 0:
		if got != "" {
			t.Errorf("没落成却写了列（%q）⇒ 半真历史", got)
		}
		if n := pumpEventsOf(t, d, "execution_claimed"); n != 0 {
			t.Errorf("落不成却留了事件行，实际 %d 行", n)
		}
		if n := tap.count("落库：没落成"); n != 1 {
			t.Errorf("落不成必须恰一声，实际 %d：%v", n, tap.lines)
		}
		// 今天的墙必须是**平台事件目录**那一句，不是别的错（别的错就是缺陷，不许当成"门在叫"）。
		if line := tap.lineWith("落库：没落成"); !strings.Contains(line, "is not in the event journal catalogue") {
			t.Errorf("落不成的原因应是事件目录未登记（三处同改的另两半未进树），实际那一声是 %q", line)
		}
	default:
		t.Fatalf("动手的喊话只能在一侧，已落 %d 句（日志：%v）", landed, tap.lines)
	}
	// 观察面那条不能替库这一腿担保：claimed 那一声必须也在（两腿各判各的）。
	if n := tap.count("claimed 带回 provider_execution_id"); n != 1 {
		t.Errorf("在飞名单那一腿该记恰一次，实际 %d 句：%v", n, tap.lines)
	}
}
