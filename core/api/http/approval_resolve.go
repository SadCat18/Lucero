package httpapi

// `POST /api/v1/approvals/{id}:resolve` 的 handler 接线（T-426 ①，10-01 14:00 主控派发）。
//
// 它单独成文件只为一条：**`approvals.go` 那个文件里不许出现任何表入口符号**（它自己那条按字面扫的
// 判据 `TestApprovalsFileNeverTouchesTheTable` 就是钉这一格的），而幂等那一腿天生要用
// `c.Store.Write` 去认领/封存 `idempotency_keys` 的行 —— 那是**可变表里我的那一家**，
// 与 `approvals` 这张绝对禁区毫无关系。把两件事塞进一个文件，要么逼红那条判据、要么逼我放弃幂等，
// 两条都是拿形状换形状。所以这里明写：本文件出现 `idempotency_keys` 是允许的，出现 `approvals` 的
// SQL 不是（一条都不在，决议整条腿只调 `internal/approval` 的方法）。
//
// 三条边界（T-426 ① 与 yaml 逐字对）：
//   1. **本层一行都不判"该不该批"**：yaml `:603` 明写"面板只发决议，裁决权在 Core 的 Permission/
//      Approval Engine"。谁能批、缺什么就拒、`resolved_by` 从哪来 —— 三件全在 `internal/approval`
//      那侧（audit 10-01 14:1x 用三行交出它实际校验了什么；契约 §3/§4 由主控据那三行落）。
//      我这一层只把请求形状认下来、转发、再把它给出的答案翻译成 HTTP 的码。
//   2. **`approvals` 表零接触**：所有读写都经注入的那一个方法集（`ApprovalDecider`）。
//   3. **不在写事务里调会自己开事务的 Service**（契约① 第 23 条 + CO-45 那条"一次事务里调第二个
//      开事务的 Service ⇒ 先挂死"）⇒ 决议这一腿是**三段短事务**：认领 / （Service 自己那一笔）/
//      封存，中间那段**不在**任何我持有的 tx 里。这与 send 那条"两笔短事务"是同一个理。

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/SadCat18/lucero/core/internal/approval"
	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// resolveApprovalBody 逐字是 yaml `ResolveApprovalRequest`（`:1228~1233`）那两个键：
// `state` 必填、枚举只有 APPROVED/REJECTED；`note` 可空。
// **这里不加第三个键**：temporary grant 在这张请求体里没有位置（`Resolution.Temporary` 由
// 有授权面的那一腿自己去填，transport 拿不到、也不该替面板发明一个入口）。
type resolveApprovalBody struct {
	State string  `json:"state"`
	Note  *string `json:"note"`
}

// notWiredDecision 是两条口共用的那句真话：注入的是"回读 + 裁决"这一对，缺一半就不算接上。
const notWiredDecision = "本进程没把 approval 的回读/裁决这一对接进来 —— 那一对在 `internal/approval`" +
	"（T-372 那一家：`Service.GetByID` + `Service.Resolve`）"

func resolveApproval(w http.ResponseWriter, r *http.Request, c Config, id string) {
	if c.ApprovalDecision == nil {
		writeError(w, http.StatusNotImplemented, "not_implemented",
			"POST /api/v1/approvals/"+id+":resolve 在契约③ §3 登记过（resolveApproval），但"+notWiredDecision, true)
		return
	}
	// 主体从头部来，不从 body 自报（契约③ §1.4 与 `:accept` 同一条：决议人就是那个写主体）。
	// **这里不再判一次"主体在不在"**：`handler.go:235` 已经把"写操作必须带 X-Lucero-Principal"挡在
	// 所有 POST 前面（403），在这一层再写一支就是一个谁也观察不到的分支（第 306 条那条"无人能观察的
	// 状态不留"）。真到了空串那一格，`internal/approval` 的 `validateResolution` 自己会拒 ——
	// 那是它的名分，我这一层不替它先答。
	by := r.Header.Get("X-Lucero-Principal")
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "读请求体失败", true)
		return
	}
	var body resolveApprovalBody
	if err := json.Unmarshal(raw, &body); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed",
			`:resolve 需要一个 JSON 体 {"state":"APPROVED"|"REJECTED","note":可选}`, false)
		return
	}
	// 枚举在这里认，**权限不在这里认**：形状是 transport 的事（yaml 把 enum 写死在 schema 里），
	// "这个主体能不能批这一单"是 Permission/Approval Engine 的事。
	if body.State != approval.APPROVED && body.State != approval.REJECTED {
		writeError(w, http.StatusBadRequest, "validation_failed",
			"state 只能是 APPROVED 或 REJECTED（EXPIRED/CANCELLED 由那一侧自己判，面板不替它填）", false)
		return
	}
	note := ""
	if body.Note != nil {
		note = *body.Note
	}
	correlation := newUUIDv7()
	key := r.Header.Get("Idempotency-Key")
	// 幂等待遇与 `:accept` 同一条既有裁量：**带了就走 claim/seal、没带按现状放行**
	// （yaml 把 `Idempotency-Key` 列进参数、`required` 那一句在 v0.1 五个 task 动词上也没强制；
	// 要不要 403 强制归主控，不在这一层擅自改口径）。request_hash 取 body：这一口有 body，
	// 它就是"同一把键换了一份决议"的判据（与 createTask/send 同一个道理）。
	var idem *idemSpec
	if key != "" {
		idem = &idemSpec{key: key, operation: "approval_resolved", hash: hashOf(raw)}
	}

	// 第一段短事务：认领这把键。重放直接把首次响应原样发出去，一次副作用都不许多做。
	if idem != nil {
		state, payload, cerr := claimDecision(r.Context(), c.Store, idem, by)
		if cerr != nil {
			writeIdemError(w, r, cerr)
			return
		}
		switch state {
		case claimReplay, claimResume:
			if state == claimReplay {
				writeRawJSON(w, http.StatusOK, payload)
				return
			}
			// IN_PROGRESS 且带着凭证 ⇒ 第一笔的意图已提交。`Resolve` 自己按状态幂等
			// （库里已 resolved ⇒ 直接返回 nil，不多写一行、不多发一条事件），所以这里**续跑**：
			// 重跑一次决议、然后把回读到的那份封成首次响应。第 25 条②那句"答案来自第一次完成的尝试"
			// 在这一腿的落法就是这个 —— 不是再判一次，是把那一份读出来。
		case claimInFlight:
			writeError(w, http.StatusConflict, "idempotency_replay",
				"这把 Idempotency-Key 的第一次请求还在进行且没有可续跑的凭证 ⇒ 现在没有答案可给", true)
			return
		}
	}

	// 中间这一段**不在任何我持有的事务里**：`approval.Service` 的那笔写自带 tx 与自带审计链
	//（`store_sqlite.go` 的 `write(tx, chain)`），把它套进我的 tx 里就是 CO-45 那格实测过的挂死。
	derr := c.ApprovalDecision.Resolve(r.Context(), id, approval.Resolution{
		State: body.State, By: by, Note: note,
		CorrelationID: correlation, CausationID: correlation,
	})
	if derr != nil {
		writeResolveError(w, r, derr)
		return
	}
	got, gerr := c.ApprovalDecision.GetByID(r.Context(), id)
	if gerr != nil {
		// 决议已经落了，这里读不回来是**另一件事**：不许回 404 说"没有这条审批"（那句会对不上库），
		// 也不许回一个空 200。500 + retryable 是真话：副作用已发生，重试这把键会走续跑/重放那条腿。
		writeError(w, http.StatusInternalServerError, "internal_error",
			"决议已提交，但回读那份 Approval 没成功 ⇒ 带同一个 Idempotency-Key 重试，或按 {id} 再读一次", true)
		return
	}
	v, verr := approvalViewOf(got)
	if verr != nil {
		serverError(w, r, verr)
		return
	}
	sealed := ""
	if idem != nil {
		// `sealIdempotency` 回读的是库里那一份 ⇒ 首次与重放共用同一串字节（C18 裁定 ② 那条"逐字节等于首击"）。
		s, serr := sealDecision(r.Context(), c.Store, idem.key, v)
		if serr != nil {
			// 决议已落、封存输了：这一把键的"首次响应"没能登记 ⇒ 明说 409，不静默 200。
			writeError(w, http.StatusConflict, "idempotency_replay",
				"决议已落库，但这把 Idempotency-Key 的封存没赢（并发同键）⇒ 用 {id} 读那一份是真的", true)
			return
		}
		sealed = s
	}
	if sealed != "" {
		writeRawJSON(w, http.StatusOK, sealed)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// claimDecision / sealDecision 是这一腿对 `idempotency_keys` 的两笔短事务包装。
// 单独抽出来只为一件事：`claimIdempotency` 那个旧签名把四态折成两态（它服务的五口从不写凭证），
// 而这里要区分"重放"与"可续跑"⇒ 用 `claimIdempotencyRow` 那份四态核心，不改别人的签名。
func claimDecision(ctx context.Context, store TaskStore, spec *idemSpec, principal string) (int, string, error) {
	var state int
	var payload string
	err := store.Write(ctx, func(tx *db.Tx) error {
		var err error
		state, payload, err = claimIdempotencyRow(ctx, tx, spec.key, principal, spec.operation, spec.hash)
		return err
	})
	if err != nil {
		return 0, "", err
	}
	return state, payload, nil
}

func sealDecision(ctx context.Context, store TaskStore, key string, view any) (string, error) {
	var sealed string
	err := store.Write(ctx, func(tx *db.Tx) error {
		s, err := sealIdempotency(ctx, tx, key, view)
		sealed = s
		return err
	})
	if err != nil {
		return "", err
	}
	return sealed, nil
}

// writeResolveError 只翻译**有名字的**答案：`ErrNotFound` 是 404，
// `ErrConflictingResolution` 是 409（契约① §3 规则 8 那半：同一个 id 第二次给**不同的**决议必须 409，
// 不许静默成功 —— 这个哨兵是 audit 在 T-438 交的名字，不是我照文案猜的；它没落地之前这一支一直落 500）。
// 其余一律 500：**没名分的错误不猜码**（今天 `internal/approval` 还有几条 `fmt.Errorf` 的拒绝没有哨兵，
// 按文案折码就是拿字符串当合同，改一个字换一种码而没人看得见）。
//
// 为什么 409 用 `illegal_transition` 这个码名：yaml `ErrorCode` 是**闭集 29 条**（`:797~803`），
// 里面没有泛用的 `conflict`，而 `illegal_transition`（409，§15.2 已登记）说的正是"这个聚合不接受这一次转换" ——
// 一张已决的审批再要一个不同的决议，就是那条转换。另注册在同一码上的 `version_conflict` 是 If-Match 的事，不混用。
func writeResolveError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, approval.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "没有这条 Approval", false)
	case errors.Is(err, approval.ErrConflictingResolution):
		writeError(w, http.StatusConflict, "illegal_transition",
			"这条审批已经有了一份不同的决议 ⇒ 不覆盖（要改判走新的审批单）", false)
	default:
		serverError(w, r, err)
	}
}

// writeIdemError 是认领那一步的两种失败：键被别的请求体占用 / 重复键。
// 码与 task/project 那两口逐字相同（同一个哨兵同一个码，别在第五个出口上漂移）。
func writeIdemError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, errKeyMismatch):
		writeError(w, http.StatusConflict, "idempotency_mismatch",
			"同一把 Idempotency-Key 带了不同的请求身份（键含 request_hash）⇒ 这一次不执行", false)
	case errors.Is(err, errReplay):
		writeError(w, http.StatusConflict, "idempotency_replay",
			"同一 Idempotency-Key 的首次请求还没完成，暂时没有可回放的响应", true)
	default:
		serverError(w, r, err)
	}
}

// writeRawJSON 把**已经封存的字节**原样发出去：重放的那一份响应不许再 marshal 一次
// （C18 那条"同键重放逐字节等于首击"，浮点/字段序都能让二次编码漂移）。
func writeRawJSON(w http.ResponseWriter, code int, payload string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = io.WriteString(w, payload)
}
