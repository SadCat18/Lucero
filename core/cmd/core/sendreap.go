package main

// sendreap 是甲（T-370① 裁的那一支）的落点 (b)：send 这一腿在连接关闭**之前**，
// 把这条连接的入站通道扫一遍，收属于这一次执行的 `turn/finished`。
//
// 它单独成一个文件、并且刻意做成"只吃一个 channel + 一个执行号"的纯函数，有两个原因：
//   - 边界三：甲的语义就是"这条腿自带收割"，散在 handler 里将来没人找得到它 ⇒ 整块可搬走；
//   - 判据要能喂得进帧：`*process.Connection` 我造不出来（那是 adapter 的包），
//     但 `<-chan process.Inbound` 我造得出来 ⇒ 有牙的那一层在这里，接线那一层由 sendpipe 的用例钉。
//
// 为什么只"非阻塞扫一遍"（边界二）：`internal/process` 的泵写 `inbound` 是**满就顶住读泵**
// （`connection.go:290~292` 明写"back-pressure at least shows up as a stalled Adapter"，缓冲 64 在 `:112`），
// 这里若阻塞等一个可能永远不来的帧，就是把"等"塞回 handler 线程；用 sleep/轮询"等够"更是禁的。
// 甲能这么做的依据是"会关这条 turn 的那两条腿都在回执之前发帧"：`sessionStop` 的注释与代码都是
// `stream/state → turn/finished(CANCELLED) → reply`（`cmd/fake-adapter/service.go:435` 那个 `sessionStop`，收尾在 `:458`），
// 队列排干那一路是 `evidence → turn/finished(resultSucceeded)`（`main.go:466` 的 `drainQueue`，收尾在 `:490`；
// 那个常量在 `:117` = `"SUCCEEDED"`，T-382② 之前它发的是 `"OK"`）。
//
// **但 `start_now` 那一路今天还是不发 `turn/finished`**（① 未交，已开成 CO-54）：`accept`（`main.go:346`）只开 turn + 交 CO-8 那一对
// 证据帧就把回执发回来，`finish` 是队列唯一的排干点（CO-25 的裁语，`service.go:454` 那句 "No drain call here on purpose" 明写不在别处补一次）。
// ⇒ 所以走真 producer 的 start_now 会落到边界二那一格：**等不到帧 ⇒ 不写终态**，execution 留 RUNNING。
// 这一条我原样报主控（它决定"闸门的下一次观察"要不要 producer 配合），不在这里加 sleep 去"等够"，
// 也不在 Core 侧凭回执编一个终态出来（那正是 T-361②/T-377④ 禁的伪造）。
//
// 只认帧内容（边界一）：`Err != nil` 是"进程死了/证据破了"那一声（`connection.go:657` 那一支），不是收尾；
// 不属于这次执行的帧跳过（一条连接可能同时跑几个执行，见 §6 规则 5 的 turn_ref 归因）；
// 只有 `Frame.IsFinished()` 且执行号对得上才收，收的是**帧里那个字符串本身**，翻译发生在 (a)。

import (
	"github.com/SadCat18/lucero/core/internal/execution"
	"github.com/SadCat18/lucero/core/internal/process"
)

// reapTurnFinished 扫空当前缓冲，返回收到的那份收尾（没收到就是零值）。
// 缓冲里同一次执行出现多条收尾时取**最后一条**并原样带上 —— 重复与矛盾是 CO-8 的 tracker 判的
// （`stream.go` 的 `Finish` 对"已 CLOSED 又换一个结果"直接报错），我不在这里再判一遍。
func reapTurnFinished(in <-chan process.Inbound, executionID string) execution.TurnFinish {
	var found execution.TurnFinish
	for {
		select {
		case msg, ok := <-in:
			if !ok {
				return found
			}
			if msg.Err != nil || msg.Frame == nil || !msg.Frame.IsFinished() {
				continue
			}
			// 归因只认帧上那个执行号，**空号不算命中**：拿一条归不到执行的收尾去推进这次执行，
			// 就是 §6 规则 5 与 CO-8 禁的那种"替别人猜一个身份"（pump 在 connection.go:552/:557 会填这个号）。
			if msg.ExecutionID != executionID {
				continue
			}
			found = execution.TurnFinish{Seen: true, ResultCode: msg.Frame.ResultCode}
		default:
			return found
		}
	}
}

// noteTurnFinish 把"这次收到了什么收尾"落成日志上的一声，三种状态分开（契约② §6 第 6 条推论①，
// T-381③ⅱ 明写别把"没收到帧"当成"对端楔住"来验证）：
//   - **没收到帧** ⇒ 不喊。今天真 producer 的 `start_now` 正是这一形（只交 CO-8 那一对证据帧就回回执），
//     它与"对端楔住"（连回执都没有 ⇒ 503 + 不密封）是两种结局，混了就会把恢复那一格测成假象；
//   - **收到表外的码**（如 `"OK"`）⇒ 喊一句，不写终态（边界一：收到帧不等于成功）；
//   - **收到表内的码** ⇒ 不喊，由 handler 在第二笔短事务里交给 `FinishFromTurn` 落终态。
//
// 从 `sendpipe.go` 里抽出来只有一个理由：这三行是甲唯一"只能从日志看见"的分支，不抽就没有自己的用例。
func noteTurnFinish(logf func(string, ...any), executionID string, fin execution.TurnFinish) {
	if !fin.Seen || execution.MappedTurnResultCode(fin.ResultCode) {
		return
	}
	if logf != nil {
		logf("send %s: 收到未裁的 result_code=%q ⇒ 不写终态（契约② §6 第 6 条：表里只有 SUCCEEDED/FAILED/CANCELLED）",
			executionID, fin.ResultCode)
	}
}
