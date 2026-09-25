package httpapi_test

// `POST /api/v1/notifications/{id}:handle` 的验收半边，按契约① §1.1 **第 21 条**（行 457~483，
// 判据形状按 §1.1.16 三条腿）钉六格：
// UNREAD→ / READ→ / PENDING→ / HANDLED 重放（**版本不动 + journal 不增长两样都断**）/
// IGNORED→409 / EXPIRED→409（409 那两条还要断**一行都没写**）。
// 外加两格末格：`UNREAD→HANDLED` 不补 `read_at`、**不要求 `Idempotency-Key`**。

import (
	"net/http"
	"testing"
	"time"

	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// handleBack 读回那一行的四个观察点：状态、两把时刻、版本。`read_at` 与 `handled_at` 分开断，
// 因为第 21 条末格说的正是"处置不许折成读过"。
func handleBack(t *testing.T, d *db.DB, id string) (state string, readAt, handledAt, version int64) {
	t.Helper()
	if err := d.Read.QueryRow(`SELECT state, COALESCE(read_at,0), COALESCE(handled_at,0), version
		FROM notifications WHERE id = ?`, id).Scan(&state, &readAt, &handledAt, &version); err != nil {
		t.Fatal(err)
	}
	return
}

// forceState 只给"库里已经有这么一行"用（PENDING/IGNORED/EXPIRED 今天都没有生产者，
// 造它们是夹具的活 —— `_test.go` 里的跨包写按 §1.3 合法，不冒充生产路径）。
func forceState(t *testing.T, d *db.DB, id, state string) {
	t.Helper()
	if _, err := d.Read.Exec(`UPDATE notifications SET state = ? WHERE id = ?`, state, id); err != nil {
		t.Fatal(err)
	}
}

// TestHandleFromUnreadWritesOnlyHandledAt 第 21 条末格：`UNREAD → HANDLED` **只写 handled_at，不补 read_at**。
func TestHandleFromUnreadWritesOnlyHandledAt(t *testing.T) {
	srv, d := newNotifHarness(t, false, true)
	seedOne(t, d, "n-h-unread")

	code, body := call(t, srv, "POST", "/api/v1/notifications/n-h-unread:handle", "local-user", "", "")
	if code != http.StatusOK {
		t.Fatalf("该 200，实际 %d %s", code, body)
	}
	state, readAt, handledAt, ver := handleBack(t, d, "n-h-unread")
	if state != "HANDLED" || ver != 2 {
		t.Errorf("该推进成 HANDLED v2，实际 %s v%d", state, ver)
	}
	if readAt != 0 {
		t.Errorf("第 21 条末格：UNREAD→HANDLED 不许补 read_at（那是替面板抹掉「未读但已处理」），实际 read_at=%d", readAt)
	}
	if handledAt <= 0 {
		t.Errorf("handled_at 必须落下（反例「摘掉 handled_at 写入」就红在这一格），实际 %d", handledAt)
	}
	if n := eventCount(t, d, "notification_handled"); n != 1 {
		t.Errorf("一行状态配恰一条 notification_handled，实际 %d", n)
	}
	if n := eventCount(t, d, "notification_created"); n != 1 {
		t.Errorf("建行的那条事件不该被动，实际 %d", n)
	}
}

// TestHandleFromReadAndPendingAreBothLegal 钉"READ 必须在允许集里"那一格（464~471 行的理由）。
// 只放 READ→HANDLED 会逼面板先补发一次 :read；反过来彻底不放 READ，就等于把"看过"当成"处置过"。
func TestHandleFromReadAndPendingAreBothLegal(t *testing.T) {
	t.Run("READ→HANDLED 且保留 read_at", func(t *testing.T) {
		srv, d := newNotifHarness(t, true, true)
		seedOne(t, d, "n-h-read")
		if code, body := call(t, srv, "POST", "/api/v1/notifications/n-h-read:read", "local-user", "", ""); code != http.StatusOK {
			t.Fatalf("先标读过该 200，实际 %d %s", code, body)
		}
		_, readBefore, _, _ := handleBack(t, d, "n-h-read")
		if readBefore <= 0 {
			t.Fatal("前置没成立：:read 之后 read_at 还是空")
		}
		if code, body := call(t, srv, "POST", "/api/v1/notifications/n-h-read:handle", "local-user", "", ""); code != http.StatusOK {
			t.Fatalf("READ→HANDLED 是合法边，该 200，实际 %d %s", code, body)
		}
		state, readAt, handledAt, ver := handleBack(t, d, "n-h-read")
		if state != "HANDLED" || ver != 3 || readAt != readBefore || handledAt <= 0 {
			t.Errorf("该 HANDLED v3、read_at 原样保留、handled_at 新落下，实际 %s v%d read=%d handled=%d",
				state, ver, readAt, handledAt)
		}
	})

	t.Run("PENDING→HANDLED", func(t *testing.T) {
		srv, d := newNotifHarness(t, false, true)
		seedOne(t, d, "n-h-pending")
		forceState(t, d, "n-h-pending", "PENDING")
		if code, body := call(t, srv, "POST", "/api/v1/notifications/n-h-pending:handle", "local-user", "", ""); code != http.StatusOK {
			t.Fatalf("PENDING→HANDLED 该 200，实际 %d %s", code, body)
		}
		if state, _, _, ver := handleBack(t, d, "n-h-pending"); state != "HANDLED" || ver != 2 {
			t.Errorf("该 HANDLED v2，实际 %s v%d", state, ver)
		}
	})
}

// TestHandleReplayIsNoOp 是重放那一格，**两样都要断**：版本不动 + journal 不增长。
// 只断 200 的话，"每次重放再发一条 `notification_handled`"那种写法也能过（§6 事件写成仪式就是这一族）。
func TestHandleReplayIsNoOp(t *testing.T) {
	srv, d := newNotifHarness(t, false, true)
	seedOne(t, d, "n-h-replay")

	code1, body1 := call(t, srv, "POST", "/api/v1/notifications/n-h-replay:handle", "local-user", "", "")
	if code1 != http.StatusOK {
		t.Fatalf("首攻该 200，实际 %d %s", code1, body1)
	}
	_, _, handledFirst, verFirst := handleBack(t, d, "n-h-replay")

	time.Sleep(3 * time.Millisecond) // 让 now() 一定往前走，否则"没覆写"可能只是毫秒撞车的假绿
	code2, body2 := call(t, srv, "POST", "/api/v1/notifications/n-h-replay:handle", "local-user", "", "")
	if code2 != http.StatusOK {
		t.Fatalf("重放该 200 no-op，实际 %d %s", code2, body2)
	}
	state, _, handledNow, verNow := handleBack(t, d, "n-h-replay")
	if state != "HANDLED" || verNow != verFirst {
		t.Errorf("重放不该推版本，实际 %s v%d（首攻 v%d）", state, verNow, verFirst)
	}
	if handledNow != handledFirst {
		t.Errorf("重放不许覆写 handled_at（%d→%d）", handledFirst, handledNow)
	}
	if n := eventCount(t, d, "notification_handled"); n != 1 {
		t.Errorf("重放不许再发事件，`notification_handled` 实际 %d 条", n)
	}
}

// TestHandleRefusesTerminalStatesWith409 钉 `IGNORED`/`EXPIRED`：409 `illegal_transition` **且一行都不写**。
// 这两条就是反例"把 409 那道闸改成任意前态都盖"要红的地方。
func TestHandleRefusesTerminalStatesWith409(t *testing.T) {
	for _, st := range []string{"IGNORED", "EXPIRED"} {
		t.Run(st+"→HANDLED 该 409", func(t *testing.T) {
			srv, d := newNotifHarness(t, false, true)
			id := "n-h-" + st
			seedOne(t, d, id)
			forceState(t, d, id, st)
			stateBefore := st
			_, readB, handledB, verB := handleBack(t, d, id)
			journalBefore := eventCount(t, d, "notification_handled")

			code, body := call(t, srv, "POST", "/api/v1/notifications/"+id+":handle", "local-user", "", "")
			if code != http.StatusConflict {
				t.Fatalf("%s 已是处置/失效痕迹，再标一次该 409，实际 %d %s", st, code, body)
			}
			if c := codeOf(t, body); c != "illegal_transition" {
				t.Errorf("错误码该是 illegal_transition（§15.2：这码来自各 Service 而非 Adapter），实际 %q", c)
			}
			state, readAt, handledAt, ver := handleBack(t, d, id)
			if state != stateBefore || ver != verB || readAt != readB || handledAt != handledB {
				t.Errorf("被拒的这一发不该动那一行：%s v%d read=%d handled=%d（前置 %s v%d read=%d handled=%d）",
					state, ver, readAt, handledAt, stateBefore, verB, readB, handledB)
			}
			if n := eventCount(t, d, "notification_handled"); n != journalBefore {
				t.Errorf("被拒的一发多写了 %d 条事件（409 那两条要断「一行都没写」这一格）", n-journalBefore)
			}
		})
	}
}

// TestHandleUnknownIs404 与 :read 同一格口径：查不到就是 404 `not_found`，不许退成 500 或 409。
func TestHandleUnknownIs404(t *testing.T) {
	srv, _ := newNotifHarness(t, false, true)
	code, body := call(t, srv, "POST", "/api/v1/notifications/n-h-none:handle", "local-user", "", "")
	if code != http.StatusNotFound {
		t.Fatalf("该 404，实际 %d %s", code, body)
	}
	if c := codeOf(t, body); c != "not_found" {
		t.Errorf("错误码该是 not_found，实际 %q", c)
	}
}

// TestHandleNeedsNoIdempotencyKey 钉末格"不要求 Idempotency-Key"：上面每一格都是不带 key 打的，
// 这条明写出来，是为了下一个想"顺手加个 403"的人（`:read` 同口径，C36③ 已认）。
func TestHandleNeedsNoIdempotencyKey(t *testing.T) {
	srv, d := newNotifHarness(t, false, true)
	seedOne(t, d, "n-h-nokey")
	code, body := call(t, srv, "POST", "/api/v1/notifications/n-h-nokey:handle", "local-user", "", "")
	if code != http.StatusOK {
		t.Fatalf("不带 Idempotency-Key 也该 200（这口不要求它），实际 %d %s", code, body)
	}
	if state, _, _, _ := handleBack(t, d, "n-h-nokey"); state != "HANDLED" {
		t.Errorf("库里该真推进了，实际 %s", state)
	}
}
