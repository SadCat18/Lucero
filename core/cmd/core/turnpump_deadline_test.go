package main

// 泵的外呼期限判据（T-414 ①②）。守的是契约① 第 23 条⑧款那句
// 「连接面上每一次调用必须自带期限」（`specs/contracts-internal-v0.1.md:593`）：
// 平台层不检测也不自救，`h.ctx` 又**不会到期**（它是 Core 的生命周期），
// 而写面只有 `platform/db` 那一条串行连接（`db.go:48` `write.SetMaxOpenConns(1)`）。
// 三者叠在一起的后果不是"慢"，是**泵被一次写吃掉**：它不再回到 `select`，入站帧堆在缓冲里没人收。
//
// 三条判据各管一段，谁也不替谁：
//   - `TestPumpFactsCallCarriesItsDeadline` —— 钉"外呼拿到的 ctx 真带期限"这件事本身（确定性、不起进程）；
//   - `TestPumpKeepsDrainingWhileTheWriterIsBusy` —— 真占住那条写连接，看泵**到期放开之后还收不收第二帧**
//     （这一条才是"没被吃掉"的正面证据，也是第 306 条那处负载下单次复现的可判形状）；
//   - `TestPumpCallDeadlineMatchesItsFamily` —— 量级不是一枚孤零零的数：与 `sendAttemptDeadline`、
//     `probeAttemptDeadline` 同值（主控 ① 那句"出处是同仓两个同族常量现值"就钉在这里）。

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/SadCat18/lucero/core/internal/execution"
	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// TestPumpFactsCallCarriesItsDeadline 把 `handleDeath` 那一腿实际交给外呼的 ctx 抓出来看：
// 必须**带期限**，且期限不超过那个常量（下界给一点点余量，避免调度抖动把它判成红）。
// 为什么挑 facts 这一腿做静态抓形：`execution.ProcessFacts` 是本层注入的函数类型，
// 测试可以就地换掉它并读到自己那一次拿到的 ctx —— 健康写口与 `store.Write` 都是具体类型，
// 换不动，所以它们由下面那条真占写面的用例覆盖（三条腿合起来才叫全）。
func TestPumpFactsCallCarriesItsDeadline(t *testing.T) {
	_, _, pipe, d := newBareDiscover(t, []adapterSpec{{key: "fake", displayName: "Fake Adapter", command: "不需要孩子"}})
	seedClaimRow(t, d, "ex-dl")
	pipe.hub.watch("fake", "ex-dl")
	var tap logTap
	pipe.hub.logf = tap.f

	var got []string
	pipe.hub.facts = func(ctx context.Context, _ string) ([]execution.ProcessFact, error) {
		dl, ok := ctx.Deadline()
		switch {
		case !ok:
			got = append(got, "没有期限")
		case time.Until(dl) > pumpCallDeadline || time.Until(dl) <= 0:
			got = append(got, "期限不在 (0, "+pumpCallDeadline.String()+"] 内: "+time.Until(dl).String())
		default:
			got = append(got, "ok")
		}
		return nil, nil // 空事实 = 不改判那一支，本用例只要问得出 ctx
	}
	pipe.hub.handleDeath("fake", 1, 4242)
	if len(got) != 1 {
		t.Fatalf("handleDeath 该恰好问一次进程事实，实际 %v 次（%v）；泵的观察：%v", len(got), got, tap.lines)
	}
	if got[0] != "ok" {
		t.Errorf("取事实那一笔外呼没带上期限：%v", got)
	}
	// 空事实不能静默：这台没有进程行可归属 ⇒ 一条都不改判，且要喊出来（CO-44② 那格照旧在）。
	if n := tap.count("没有进程行可归属"); n != 1 {
		t.Errorf("空事实必须出声，实际 %d 句：%v", n, tap.lines)
	}
	if n := tap.count("崩溃处理收尾"); n != 1 {
		t.Errorf("崩溃处理该以那一声收尾，实际 %d：%v", n, tap.lines)
	}
	if n := pumpEventsOf(t, d, "execution_unknown"); n != 0 {
		t.Errorf("没有进程行可归属却改判了（execution_unknown %d 行）", n)
	}
}

// TestPumpCallDeadlineMatchesItsFamily 钉的是量级的**出处**：不是我在空里挑一个数，
// 而是与同仓那两个同族常量相等（主控 T-414 ① 给的就是这条理由）。
// 这里断"关系"不断字面值：三枚一起改是另一件事（那要重新裁），单独把泵这枚改成 1 秒当场红。
func TestPumpCallDeadlineMatchesItsFamily(t *testing.T) {
	if pumpCallDeadline != sendAttemptDeadline {
		t.Errorf("泵期限 %s 与 send 那枚 %s 分叉（主控 ① 给的出处就是它俩同值）", pumpCallDeadline, sendAttemptDeadline)
	}
	if pumpCallDeadline != probeAttemptDeadline {
		t.Errorf("泵期限 %s 与 probe 那枚 %s 分叉（同上：三件事该一起到期，不该出现第四档）", pumpCallDeadline, probeAttemptDeadline)
	}
	if pumpExitBound >= pumpCallDeadline {
		t.Errorf("退役等泵的那 %s 不该比一次外呼还长或相等：等不到就喊、别硬等（现值 exit=%s call=%s）",
			pumpExitBound, pumpExitBound, pumpCallDeadline)
	}
}

// TestPumpKeepsDrainingWhileTheWriterIsBusy 是 ② 要的那条：**写面被占住时泵仍在排空入站帧**。
//
// 做法是把那一条串行写连接真的占住（一笔不结束的事务），然后按顺序送两帧都需要写库的：
// `turn/claimed`（写把手列）与解除 hold 之后的 `turn/finished`（写终态）。
// 期望不是"写成功"——占着的时候谁都写不进去——而是**两笔都到期、都出声，中间泵回到过 `select`**：
// 第二声只有第一笔被期限放开、泵又去收帧之后才可能出现。摘掉期限（变异 P-a：`callCtx` 换成不带期限的 ctx）
// ⇒ 第一笔就把泵 park 死、第二声永远不来 ⇒ 本用例红。
//
// 另两条同时断住的：期限**真的起作用**（那两声里的错就是 deadline exceeded，不是别的），
// 以及"明确失败 + 一行都不写"（库里既不落把手也不落终态）。
func TestPumpKeepsDrainingWhileTheWriterIsBusy(t *testing.T) {
	pipe, tap, d, sess := deathFixture(t)
	insertInFlightRow(t, d, "ex-wedge", "task-wedge", sess)
	armHold(t, pipe)

	hold := make(chan struct{})
	released := make(chan struct{})
	occupied := make(chan struct{})
	go func() {
		defer close(released)
		_ = d.Write(context.Background(), func(tx *db.Tx) error {
			// 这一声 close 在 `BEGIN IMMEDIATE` 之后、`<-hold` 之前：等到它才算"那条唯一写连接真被占住了"。
			// （第一版没有这个同步点，探针写可能先抢到的连接 ⇒ 判据自己偶发红 —— 22:06 就是这么一枚红。）
			close(occupied)
			<-hold
			return nil
		})
	}()
	defer func() { close(hold); <-released }()
	select {
	case <-occupied:
	case <-time.After(pumpCallDeadline):
		t.Fatal("占位起不来：那一笔不结束的事务连写连接都没拿到，下面两声到期就不算证据")
	}

	// 先把"写面真被占住"变成一条读数，而不是一个假设：这一发自己也拿不到连接。
	probeCtx, cancelProbe := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelProbe()
	if err := d.Write(probeCtx, func(tx *db.Tx) error { return nil }); err == nil {
		t.Fatal("占位没成立：写面还能收第二笔 ⇒ 下面那两声到期就不算证据")
	}

	start := time.Now()
	sendHeldTurn(t, pipe, "ex-wedge", sess)
	if !tap.wait(t, "claimed 落库：没落成", pumpCallDeadline+20*time.Second) {
		t.Fatalf("泵没能从被占住的写面上放开（第一声都没出来）：%v", tap.lines)
	}
	claimLine := tap.lineWith("claimed 落库：没落成")
	if !strings.Contains(claimLine, "deadline exceeded") {
		t.Errorf("到期那一声里的错应当是期限到了，实际是 %q", claimLine)
	}
	claimWait := time.Since(start)

	// 关键那一格：泵收下第二帧（解除 hold ⇒ 收尾帧到），而写面**仍然被占着**。
	releaseHeldTurn(t, pipe)
	if !tap.wait(t, "收尾没落成", pumpCallDeadline+20*time.Second) {
		t.Fatalf("泵在第一笔到期之后没再收任何帧 ⇒ 它被一次写吃掉了：%v", tap.lines)
	}
	finLine := tap.lineWith("收尾没落成")
	if !strings.Contains(finLine, "deadline exceeded") {
		t.Errorf("收尾那一笔也应当是被期限放开，实际是 %q", finLine)
	}
	total := time.Since(start)

	// 配对：两笔都"明确失败 + 一行都不写"。
	if st := pumpExecStatus(t, d, "ex-wedge"); st != "RUNNING" {
		t.Errorf("写不进去就不许改状态，实际 %s", st)
	}
	if n := pumpEventsOf(t, d, "execution_claimed"); n != 0 {
		t.Errorf("落不成却留下 execution_claimed %d 行", n)
	}
	for _, typ := range []string{"execution_succeeded", "execution_failed", "execution_cancelled"} {
		if n := pumpEventsOf(t, d, typ); n != 0 {
			t.Errorf("被占住的写面上不该有终态事件 %s %d 行", typ, n)
		}
	}
	// 耗时只作"确实等到期限"的粗证（负载会让它往上走，所以不判下界以外的东西）：
	// 两笔各约 pumpCallDeadline，合起来应当明显小于两倍再加富余，也不该比一倍的期限还短。
	if claimWait < pumpCallDeadline-2*time.Second {
		t.Errorf("claimed 那一笔只等了 %s，比期限常量 %s 还短 ⇒ 放开它的不是期限（负载：本用例占住写面后跑）",
			claimWait, pumpCallDeadline)
	}
	if total > 4*pumpCallDeadline {
		t.Errorf("两笔合起来 %s 超过 4 倍期限，这一跑的负载异常，读数以本行为准（deadline=%s）", total, pumpCallDeadline)
	}
	t.Logf("实测（负载=真子进程 + 写面被一笔不结束的事务占住）：claimed 等 %s、两笔合起来 %s、期限=%s",
		claimWait, total, pumpCallDeadline)
}
