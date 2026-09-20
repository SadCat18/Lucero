// Package test 是跳包黑盒测试。这里的迁移矩阵是**从 specs/events/event-contract-v0.1.md
// §5.1/§5.2 独立再转录的一份**，故意不 import 被测包内部那张表——
// 自抄自测等于没测。矩阵若与实现不一致，本文件就是判据，由实现方或主控修，不由测试改。
//
// 本包两条硬规范（T-29，audit 自翻一条虚用例换来的）：
//  1. **变异验证必须打在"待验那一层"的原始输出上**——被测函数的返回值、或它直接交给
//     下游的那一行数据本身。拿上一层的结果去验下一层，两层防御会互相掩盖（那条 §3.4 用例
//     就是这么两次变异都不红的）。本包每条用例的断言对象都在注释里点名：Check 的 error /
//     Seal 的返回值 / Appender 收到的那一行 / 库里的行数。
//  2. 不许用下游消费者的判断给上游发绿（例：想证明 hash 覆盖了某列，就去改那一列再问 hash，
//     而不是去问"渲染层显示得对不对"）。//  3. 变异验证有硬流程（T-66）：
//     · 备份只准落绝对路径 D:/Project/Lucero/.scratch/<角色>/<具名文件>，不放共享 Temp；
//     · 回滚后必须**证明变异版不在盘上**：贴出变异专用标记串的 0 命中 grep，
//     拿不到这条证据就不许写"变异已还原"；
//     · 变异必须改变"是否走到被验证的那段代码"——藏在早退（skip / flag 关 / 提前 return）
//     后面的变异等于没有变异。
package test

import (
	"testing"

	"github.com/SadCat18/lucero/core/internal/execution"
	"github.com/SadCat18/lucero/core/internal/task"
)

type matrix struct {
	name   string
	states []string
	// normal = §5.x "正常后继" 列；recoveryOnly = "recovery 允许" 列里**不在**正常列的边。
	normal       map[string][]string
	recoveryOnly map[string][]string
	terminals    []string
	check        func(from, to string, recovery bool, evidence string) error
	terminal     func(s string) bool
	unknownState string
}

// ---- 独立转录：Task（§5.1）-------------------------------------------------
func taskMatrix() matrix {
	return matrix{
		name: "Task",
		states: []string{"CREATED", "QUEUED", "PREPARING", "RUNNING", "WAITING_APPROVAL",
			"WAITING_INPUT", "BLOCKED", "PAUSED", "TRANSFERRING", "AGENT_COMPLETED",
			"VERIFYING", "WAITING_ACCEPTANCE", "ACCEPTED", "FAILED", "CANCELLED",
			"INTERRUPTED", "UNKNOWN"},
		normal: map[string][]string{
			"CREATED":            {"QUEUED", "CANCELLED", "BLOCKED"},
			"QUEUED":             {"PREPARING", "PAUSED", "BLOCKED", "CANCELLED", "FAILED"},
			"PREPARING":          {"RUNNING", "WAITING_APPROVAL", "BLOCKED", "PAUSED", "CANCELLED", "FAILED"},
			"RUNNING":            {"WAITING_APPROVAL", "WAITING_INPUT", "BLOCKED", "PAUSED", "TRANSFERRING", "AGENT_COMPLETED", "FAILED", "CANCELLED"},
			"WAITING_APPROVAL":   {"RUNNING", "BLOCKED", "PAUSED", "CANCELLED", "FAILED"},
			"WAITING_INPUT":      {"RUNNING", "BLOCKED", "PAUSED", "CANCELLED", "FAILED"},
			"BLOCKED":            {"QUEUED", "RUNNING", "PAUSED", "CANCELLED", "FAILED"},
			"PAUSED":             {"QUEUED", "RUNNING", "CANCELLED"},
			"TRANSFERRING":       {"RUNNING", "QUEUED", "BLOCKED", "FAILED", "CANCELLED"},
			"AGENT_COMPLETED":    {"VERIFYING", "WAITING_ACCEPTANCE", "RUNNING", "CANCELLED"},
			"VERIFYING":          {"WAITING_ACCEPTANCE", "RUNNING", "FAILED", "CANCELLED"},
			"WAITING_ACCEPTANCE": {"ACCEPTED", "RUNNING", "VERIFYING", "PAUSED", "CANCELLED"},
			"INTERRUPTED":        {"QUEUED", "RUNNING", "TRANSFERRING", "CANCELLED", "FAILED"},
			"FAILED":             {"QUEUED", "CANCELLED"},
			"ACCEPTED":           {},
			"CANCELLED":          {},
			"UNKNOWN":            {},
		},
		recoveryOnly: map[string][]string{
			"QUEUED":             {"CREATED"},
			"PREPARING":          {"QUEUED", "INTERRUPTED"},
			"RUNNING":            {"INTERRUPTED"},
			"WAITING_APPROVAL":   {"INTERRUPTED"},
			"WAITING_INPUT":      {"INTERRUPTED"},
			"BLOCKED":            {"QUEUED", "INTERRUPTED"},
			"PAUSED":             {"QUEUED"},
			"TRANSFERRING":       {"INTERRUPTED"},
			"AGENT_COMPLETED":    {"INTERRUPTED"},
			"VERIFYING":          {"INTERRUPTED"},
			"WAITING_ACCEPTANCE": {},
			"INTERRUPTED":        {},
			"FAILED":             {},
		},
		terminals:    []string{"ACCEPTED", "CANCELLED"},
		unknownState: "UNKNOWN",
		check: func(from, to string, rec bool, ev string) error {
			o := task.Normal
			if rec {
				o = task.Recovery
			}
			return task.Check(task.State(from), task.State(to), o, task.Evidence{Reason: ev})
		},
		terminal: func(s string) bool { return task.Terminal(task.State(s)) },
	}
}

// ---- 独立转录：Execution（§5.2）--------------------------------------------
func executionMatrix() matrix {
	return matrix{
		name: "Execution",
		states: []string{"CREATED", "STARTING", "RUNNING", "WAITING_APPROVAL", "WAITING_INPUT",
			"PAUSED", "TRANSFERRING", "INTERRUPTED", "SUCCEEDED", "FAILED", "CANCELLED", "UNKNOWN"},
		normal: map[string][]string{
			"CREATED":          {"STARTING", "CANCELLED", "FAILED"},
			"STARTING":         {"RUNNING", "WAITING_APPROVAL", "FAILED", "CANCELLED"},
			"RUNNING":          {"WAITING_APPROVAL", "WAITING_INPUT", "PAUSED", "TRANSFERRING", "SUCCEEDED", "FAILED", "CANCELLED"},
			"WAITING_APPROVAL": {"RUNNING", "PAUSED", "FAILED", "CANCELLED"},
			"WAITING_INPUT":    {"RUNNING", "PAUSED", "FAILED", "CANCELLED"},
			"PAUSED":           {"RUNNING", "CANCELLED"},
			"TRANSFERRING":     {"FAILED", "CANCELLED"},
			"INTERRUPTED":      {"RUNNING", "FAILED", "CANCELLED"},
			"SUCCEEDED":        {},
			"FAILED":           {},
			"CANCELLED":        {},
			"UNKNOWN":          {},
		},
		recoveryOnly: map[string][]string{
			"CREATED":          {},
			"STARTING":         {"INTERRUPTED"},
			"RUNNING":          {"INTERRUPTED"},
			"WAITING_APPROVAL": {"INTERRUPTED"},
			"WAITING_INPUT":    {"INTERRUPTED"},
			"PAUSED":           {"INTERRUPTED"}, // RUNNING 已在正常列，recovery 列只多这一条
			"TRANSFERRING":     {"INTERRUPTED"},
			"INTERRUPTED":      {},
		},
		terminals:    []string{"SUCCEEDED", "FAILED", "CANCELLED"},
		unknownState: "UNKNOWN",
		check: func(from, to string, rec bool, ev string) error {
			o := execution.Normal
			if rec {
				o = execution.Recovery
			}
			return execution.Check(execution.State(from), execution.State(to), o, execution.Evidence{Reason: ev})
		},
		terminal: func(s string) bool { return execution.Terminal(execution.State(s)) },
	}
}

func matrices() []matrix { return []matrix{taskMatrix(), executionMatrix()} }

func has(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

const ev = "recovery: lease 过期 + provider_execution_id 仍在跑"

// legal 报告 from→to 在某个场合下按矩阵是否合法（UNKNOWN 另行处理）。
func legal(m matrix, from, to string, recovery bool) bool {
	if has(m.normal[from], to) {
		return true
	}
	return recovery && has(m.recoveryOnly[from], to)
}

// TestLegalEdgesReachable 正向：矩阵每一条边都可达，且正常边在两种场合下都可用。
func TestLegalEdgesReachable(t *testing.T) {
	for _, m := range matrices() {
		for from, tos := range m.normal {
			for _, to := range tos {
				if err := m.check(from, to, false, ""); err != nil {
					t.Errorf("%s: 正常边 %s→%s 被拒: %v", m.name, from, to, err)
				}
				if err := m.check(from, to, true, ev); err != nil {
					t.Errorf("%s: 正常边 %s→%s 在 Recovery 场合被拒: %v（Recovery 应是超集）", m.name, from, to, err)
				}
			}
		}
		for from, tos := range m.recoveryOnly {
			for _, to := range tos {
				if legal(m, from, to, false) {
					continue // 同一目标已在正常列里，不算 recovery-only
				}
				if err := m.check(from, to, true, ev); err != nil {
					t.Errorf("%s: recovery 边 %s→%s 被拒: %v", m.name, from, to, err)
				}
				if err := m.check(from, to, false, ""); err == nil {
					t.Errorf("%s: %s→%s 只应对 Recovery 开放，Normal 场合却放行了", m.name, from, to)
				}
			}
		}
	}
}

// TestIllegalTransitionsRejected 反向：全笛卡尔积里矩阵没写的组合一律报错。
// 这条与正向测试互为对账——实现多一条边会被抓，少一条边也会被抓。
func TestIllegalTransitionsRejected(t *testing.T) {
	for _, m := range matrices() {
		for _, from := range m.states {
			if from == m.unknownState {
				continue // §5.5 的 UNKNOWN 有独立规则，单独测
			}
			for _, to := range m.states {
				if to == m.unknownState {
					continue // 进入 UNKNOWN 同样单独测
				}
				for _, rec := range []bool{false, true} {
					want := legal(m, from, to, rec)
					err := m.check(from, to, rec, ev)
					if want && err != nil {
						t.Errorf("%s: 矩阵里的边 %s→%s（recovery=%v）被判非法: %v", m.name, from, to, rec, err)
					}
					if !want && err == nil {
						t.Errorf("%s: 矩阵外的边 %s→%s（recovery=%v）被放行", m.name, from, to, rec)
					}
				}
			}
		}
	}
}

// TestTerminalsCannotExit 终态不得外跳（含不得跳到 UNKNOWN）。
func TestTerminalsCannotExit(t *testing.T) {
	for _, m := range matrices() {
		got := map[string]bool{}
		n := 0
		for _, s := range m.states {
			if m.terminal(s) {
				got[s] = true
				n++
			}
		}
		if n != len(m.terminals) {
			t.Errorf("%s: Terminal() 为真的状态 %d 个，矩阵声明 %d 个（%v）: %v",
				m.name, n, len(m.terminals), m.terminals, got)
		}
		for _, term := range m.terminals {
			if !got[term] {
				t.Errorf("%s: %s 是终态但 Terminal() 返回 false", m.name, term)
			}
			for _, to := range append(append([]string{}, m.states...), "BOGUS_STATE") {
				for _, rec := range []bool{false, true} {
					if err := m.check(term, to, rec, ev); err == nil {
						t.Errorf("%s: 终态 %s 外跳到 %s 被放行（recovery=%v）", m.name, term, to, rec)
					}
				}
			}
		}
	}
}

// TestUnknownRules §5.5：UNKNOWN 只能由 Recovery + 证据进入/离开；终态不得被改判为 UNKNOWN。
func TestUnknownRules(t *testing.T) {
	for _, m := range matrices() {
		u := m.unknownState
		for _, s := range m.states {
			if s == u {
				continue
			}
			terminal := has(m.terminals, s)

			// 进入 UNKNOWN
			if err := m.check(s, u, true, ev); terminal {
				if err == nil {
					t.Errorf("%s: 终态 %s 被改判成 UNKNOWN（§5.5 明令禁止，属丢历史）", m.name, s)
				}
			} else if err != nil {
				t.Errorf("%s: 非终态 %s 在 Recovery + 证据下应可进入 UNKNOWN: %v", m.name, s, err)
			}
			if err := m.check(s, u, false, ""); err == nil {
				t.Errorf("%s: Normal 场合下 %s→UNKNOWN 被放行（不得凭空改判）", m.name, s)
			}
			if err := m.check(s, u, true, ""); err == nil {
				t.Errorf("%s: Recovery 但无证据时 %s→UNKNOWN 被放行（UNKNOWN 必须有出处）", m.name, s)
			}

			// 离开 UNKNOWN：只能 Recovery + 证据 → 确定态
			if err := m.check(u, s, true, ev); err != nil {
				t.Errorf("%s: UNKNOWN→%s 在 Recovery + 证据下被拒: %v", m.name, s, err)
			}
			if err := m.check(u, s, false, ""); err == nil {
				t.Errorf("%s: Normal 场合下 UNKNOWN→%s 被放行", m.name, s)
			}
			if err := m.check(u, s, true, ""); err == nil {
				t.Errorf("%s: UNKNOWN→%s 无证据被放行", m.name, s)
			}
		}
		if err := m.check(u, u, true, ev); err == nil {
			t.Errorf("%s: UNKNOWN→UNKNOWN 被放行（等于原地不动却占一个版本）", m.name)
		}
	}
}

// TestUnknownStatesRejected 实现必须拒绝它不认识的状态名，而不是当成"没有出边"放过去。
func TestUnknownStatesRejected(t *testing.T) {
	for _, m := range matrices() {
		for _, c := range [][2]string{{"BOGUS", "RUNNING"}, {"RUNNING", "BOGUS"}, {"", ""}, {"running", "queued"}} {
			if err := m.check(c[0], c[1], true, ev); err == nil {
				t.Errorf("%s: 未知状态对 %q→%q 被放行", m.name, c[0], c[1])
			}
		}
		if m.terminal("BOGUS") {
			t.Errorf("%s: Terminal(BOGUS) 返回 true", m.name)
		}
	}
}
