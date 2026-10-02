package main

// refusals.go is the whole write-chain surface: which names are refused, with what
// §15.1 code, and why no result frame ever leaves for them.
//
// §16.7 第 2 条 states the failure it is guarding against in one line - 返回 202/200 或
// "已受理" 算失败. In this protocol the analogue of a 202 is a **result frame**: JSON-RPC
// distinguishes accepted-from-failed by which member is present (§15.3), so a reply with
// `result` for a write method would read as "the action happened" to Core, to the pump's
// tracker and to the panel. TestWriteMethodsNeverProduceAResultFrame is the criterion, and
// mutation r1 in this round's batch turns these refusals into result frames to prove it
// bites.

import (
	"github.com/SadCat18/lucero/core/internal/adapter"
)

// Adapter-produced §15.1 codes. They are named here rather than in internal/adapter
// because §15.1.1 gives them no Go constant on purpose ("给它们造没人发的常量就是投机
// 代码") - this process is the first one that ever sends them, so the numbers live with
// the sender and stay traceable to the table.
const (
	codeLocalDataUnavailable adapter.ErrorCode = -32005 // §15.1: provider known, local content not readable
	codeSessionNotFound      adapter.ErrorCode = -32006 // §15.1: remote_session_id unknown
)

// refusedMethods is §16.7 第 1 条's "它不实现也不接" list, as data.
//
// Two of these never reach the lookup in normal operation, and that is deliberate rather
// than dead weight: session/send, session/create, session/stop, model/select and
// approval/respond are refused one layer earlier by o.hs.Call, which applies §4.1's own
// write set through the same validator Core uses (§3's write_entry gate at
// internal/adapter/handshake.go). start_now / guide / approval/request are §16.7's words
// for the same closure from the delivery-mode and notification sides, and they arrive
// here only if a caller sends them as bare methods. Keeping the second line is the same
// shape as process.ChildAlreadyGone standing behind Stop's own filter: the first line is
// the rule, this one is the reason the rule cannot be walked past by a caller that
// forgot it.
var refusedMethods = map[string]bool{
	"session/create": true,
	"turn/start":     true,
	// turn/start is §16.7's own spelling of "开始一轮". The wire method that does it is
	// session/send (§4), which is why both names are listed rather than one: the table is
	// the range as written, and TestEveryRefusedNameIsRegistered walks the contract's list
	// against it so a rename in either place shows up as a gap instead of a quiet hole.
	"session/send":     true,
	"start_now":        true,
	"guide":            true,
	"session/stop":     true,
	"approval/request": true,
	"approval/respond": true,
}

// refusedWrite is the exit §16.7 第 2 条 demands: a registered §15.1 name, never a
// result frame.
//
// WRITE_ENTRY_NONE rather than CAPABILITY_UNAVAILABLE because §15.1's own row draws the
// line for us - "整个 Adapter 声明无写入口" is WRITE_ENTRY_NONE, while READ_ONLY (-32016)
// is the object-level "这一轮没有写入口" and CAPABILITY_UNAVAILABLE is "该能力当前无效
// （等级降级/未声明）". This tier closes the whole chain by declaration, so the first is
// the registered answer. backend's T-448 half has to use the same name; CO-65 puts that
// in front of the 主控 instead of letting two roles each pick quietly.
func refusedWrite(method string) error {
	return &adapter.Error{Code: adapter.ErrWriteEntryNone,
		Message: method + ": adapter " + adapterKey + " declares write_entry=false and tier READ_ONLY" +
			" (§3, §16.7 第 2 条): this build observes, it does not act"}
}

// unavailableRefusal is §15.1's answer for a method this build can name but has no
// verified means for (§4's session/locate stub, §16.5's unverified interrupt routes).
// It is a refusal with a reason, not a "not implemented": §4's model/select row says
// explicitly that returning CAPABILITY_UNAVAILABLE is the correct implementation.
func unavailableRefusal(method, why string) error {
	return &adapter.Error{Code: adapter.ErrCapabilityUnavailable, Message: method + ": " + why}
}
