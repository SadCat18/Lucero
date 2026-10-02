package main

// mapping.go is REQ-OC-1's per-provider table: OpenCode's own tool names -> the action
// strings contract ① §3.1's built-in classifier already knows. It lives here, in the
// adapter, because §16's ruling says one table per provider lives on the adapter side and
// Core's vocabulary does not grow for anybody.
//
// Two prohibitions decide what may be in this map, and both are why the table is short:
//
//   - the value domain is §3.1's registered strings only (`filesystem.write`,
//     `command.exec`, `db.write`, `deploy`, `remote.*`, `secret.read`, `adapter.debug.*`).
//     Adding a name to `permission`'s lists instead would hollow out §3.1 第 2 行's
//     default - unrecognised actions are supposed to fail closed into approval;
//   - a tool name that is NOT here must come back as "unregistered", never as "low risk".
//     read / grep / glob / todowrite / task / question are all measured on this machine
//     (profile §③: read 1202, bash 1157, grep 577, edit 481, glob 92, write 39,
//     todowrite 19, task 17, question 4), and none of them maps onto a §3.1 action that
//     this adapter is allowed to assert. Judging them low is Core's call to make, by
//     failing closed, not ours to pre-empt.
//
// Who consumes this today: nobody, and that is §16.7 第 5 条 rather than an oversight -
// the provider's permission table measured 0 rows, approvals never reach its journal, and
// this tier's write chain is closed, so approval/request has no input source here. The
// table ships now because §16's five REQ-OC rulings land regardless of which service
// option the user picked; its first real caller is the write path that option (a)/(b)
// would open.

// toolActions maps an observed OpenCode tool name onto a §3.1 action string.
var toolActions = map[string]string{
	"bash":  "command.exec",
	"edit":  "filesystem.write",
	"write": "filesystem.write",
}

// classifyTool answers the only two things an adapter may say about a tool name: the
// §3.1 action it corresponds to, or "unregistered". A false second result is not a risk
// level; it is "this provider did not teach me this name", which §3.1 routes into
// REQUIRE_APPROVAL with DEFAULT_UNKNOWN_RISK.
func classifyTool(name string) (action string, registered bool) {
	a, ok := toolActions[name]
	return a, ok
}

// registeredActions is what a criterion walks, so the table cannot grow a value that
// permission has never heard of (an unregistered *value* would fail closed for a reason
// nobody chose, which is the same accident T-441 pinned for the fake adapter).
func registeredActions() map[string]string {
	out := make(map[string]string, len(toolActions))
	for tool, action := range toolActions {
		out[tool] = action
	}
	return out
}
