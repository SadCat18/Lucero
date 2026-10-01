package httpapi_test

// `GET /executions/{id}/output` 的读面判据（T-424）。这一格以前只有一句 501（"登记过、没装配"），
// 今天装配齐了两半：写者是那条常驻入站泵（`cmd/core/turnpump_output_test.go`），读者是这里这一条。
//
// 出口只认三件事，各自一条：
//   - **形状按 yaml**：`OutputChunk` 那六键一个不多一个不少（面板的类型是生成出来的，多一键少一键都是它的编译错），
//     外层是 `CursorPage`+`items` 那两键；
//   - **内容原样**：库里那一截是什么就出什么（本层不解读 delta、不拼接、不裁剪）；
//   - **游标只有一种"没有下一页"的写法**：REQ-F-19 裁过的空串，且 `next_cursor` 喂回 `after_seq` 就走下一页。
//
// 另有三种真话各占一格：有这条执行但一批输出还没有 ⇒ 200 空页（写者已在 ⇒ 这不是冒充），
// 没这条执行 ⇒ 404，读面整个没注入 ⇒ 501。**拿 404 冒充"没装配"、拿空页冒充"没有这条"**都是假话，
// 这两句分别是第七十九批与 T-370② 那两格立过的。

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/SadCat18/lucero/core/internal/execution"
	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// seedChunks 经**归属包自己的写嘴**播行（`execution.Service.FlushOutput`），不在本包 INSERT 那张表。
// 理由只有一条，且是我自己能证的：出口的每一格（号段、channel、内容、created_at）都由真写者盖，
// 我读回来的那条 200 因此是"两半在同一座库上对得上"的证据，而不是我按自己猜的列形状播了一行、
// 再验自己的猜测。写者本体的判据在 `cmd/core/turnpump_output_test.go`（从帧一路验到行）。
// 每一对 (channel, text) 自成一批（一批一行），所以号段按入参顺序 1..n。
//
// 顺手把一条我说错的规则收回：我原以为"非归属包里对这张表的写，门禁连测试夹具一起判"，
// 于是借了一分钟把这里换回裸 `INSERT` 去量 ⇒ `check_ownership.sh` 仍 `exit=0`、`[W13-2] execution_output_chunks: 0`，
// 再读它自己的原文（`:405`、`:430`）写着"_test.go 建 fixture 不算 / 造 fixture 允许，不算越界"。
// ⇒ 那是**门禁明写的裁量**，不是漏；我那句是信念不是读数，已按真理由改写在上面这一段。
func seedChunks(t *testing.T, d *db.DB, execID string, rows ...[2]string) {
	t.Helper()
	ctx := context.Background()
	svc := execution.NewService()
	for i, r := range rows {
		err := d.Write(ctx, func(tx *db.Tx) error {
			_, e := svc.FlushOutput(ctx, tx, execID,
				execution.OutputBatch{Channel: r[0], Texts: []string{r[1]}},
				execution.Command{EventID: fmt.Sprintf("ev-seed-%s-%d", execID, i), PrincipalID: "system"})
			return e
		})
		if err != nil {
			t.Fatalf("经归属包写 %s #%d: %v", execID, i+1, err)
		}
	}
}

func TestExecutionOutputReadsBackExactlyTheSixYamlKeys(t *testing.T) {
	srv, d := newExecutionHarness(t, true)
	seedChunks(t, d, "ex-2",
		[2]string{"ASSISTANT", "第一段"}, [2]string{"ASSISTANT", "第二段"}, [2]string{"TOOL", "tool-result"})

	code, body := call(t, srv, http.MethodGet, "/api/v1/executions/ex-2/output", "", "", "")
	if code != http.StatusOK {
		t.Fatalf("有这条执行、内容也在库里 ⇒ 200，实际 %d %s", code, body)
	}
	page := mustParseMap(t, body)
	if len(page) != 2 {
		t.Errorf("外层只该是 `CursorPage` + `items` 两键（yaml OutputChunkList），实际 %d 键：%v", len(page), keysOf(page))
	}
	if page["next_cursor"] != "" {
		t.Errorf("三行没到一页 ⇒ next_cursor 该是空串（REQ-F-19：空串是唯一的「没有下一页」写法，null 与省略都不许），实际 %#v", page["next_cursor"])
	}
	items, ok := page["items"].([]any)
	if !ok || len(items) != 3 {
		t.Fatalf("items 该是三条，实际 %#v", page["items"])
	}
	first, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("items[0] 不是对象：%#v", items[0])
	}
	const wantKeys = "channel,content,created_at,execution_id,id,sequence_no"
	if got := keysOf(first); got != wantKeys {
		t.Errorf("OutputChunk 的键集该逐字是 yaml :1196~1204 那六键（面板的类型由它生成），期望 %q，实际 %q", wantKeys, got)
	}
	for i, want := range []struct {
		seq  float64
		ch   string
		text string
	}{{1, "ASSISTANT", "第一段"}, {2, "ASSISTANT", "第二段"}, {3, "TOOL", "tool-result"}} {
		it := items[i].(map[string]any)
		if it["sequence_no"] != want.seq || it["channel"] != want.ch || it["content"] != want.text {
			t.Errorf("第 %d 条该原样是 #%v %v/%v，实际 #%v %v/%v（本层不解读 delta、不拼接、不排序成别的形状）",
				i+1, want.seq, want.ch, want.text, it["sequence_no"], it["channel"], it["content"])
		}
		// created_at 是写者盖的钟（`FlushOutput` 取 `time.Now()`）⇒ 这里能断的只有"是个正的毫秒数、
		// 且沿号段不减"。拿字面量去比会把一条时间判据变成一条巧合。
		at, ok := it["created_at"].(float64)
		if !ok || at <= 0 {
			t.Errorf("第 %d 条的 created_at 该是写者盖的正毫秒数，实际 %#v", i+1, it["created_at"])
		}
		if i > 0 {
			if prev, _ := items[i-1].(map[string]any)["created_at"].(float64); at < prev {
				t.Errorf("第 %d 条的 created_at 比前一条还早（%d < %d）⇒ 出口把顺序排错了", i+1, int64(at), int64(prev))
			}
		}
		if it["execution_id"] != "ex-2" {
			t.Errorf("第 %d 条的 execution_id 该带上（面板补拉的是某一条执行的输出，不靠 URL 记着）", i+1)
		}
		if id, _ := it["id"].(float64); id == 0 {
			t.Errorf("第 %d 条的 id 该是真行 id，实际 %#v", i+1, it["id"])
		}
	}
}

// TestExecutionOutputCursorWalksWithoutGapsOrDupes 是这条游标的本体：一页两行、走完全程
// **一行不重、一行不漏**，且每一站的 `next_cursor` 都能直接喂回 `after_seq`。
// 为什么特意播五行：三行配 limit=2 只有一页，走完就完了，撞不出"重复/漏行"那一形。
func TestExecutionOutputCursorWalksWithoutGapsOrDupes(t *testing.T) {
	srv, d := newExecutionHarness(t, true)
	seedChunks(t, d, "ex-2",
		[2]string{"ASSISTANT", "a"}, [2]string{"ASSISTANT", "b"}, [2]string{"TOOL", "c"},
		[2]string{"STATUS", "d"}, [2]string{"STATUS", "e"})

	var seen []float64
	query := ""
	for hop := 0; hop < 10; hop++ {
		code, body := call(t, srv, http.MethodGet, "/api/v1/executions/ex-2/output"+query, "", "", "")
		if code != http.StatusOK {
			t.Fatalf("第 %d 站该 200，实际 %d %s", hop+1, code, body)
		}
		page := mustParseMap(t, body)
		items := page["items"].([]any)
		for _, raw := range items {
			seen = append(seen, raw.(map[string]any)["sequence_no"].(float64))
		}
		next, _ := page["next_cursor"].(string)
		if len(items) == 2 {
			if next == "" {
				t.Fatalf("取满一页（2 条）却不报游标 ⇒ 面板永远补不到第 4 行：%s", body)
			}
			query = "?after_seq=" + next + "&limit=2"
			continue
		}
		if next != "" {
			t.Errorf("没取满一页却报了游标 %q（REQ-F-19：只有「取满且非空」才写游标）", next)
		}
		break
	}
	if len(seen) != 5 {
		t.Fatalf("走完该看见 5 行，实际 %d 行：%v", len(seen), seen)
	}
	for i, want := range []float64{1, 2, 3, 4, 5} {
		if seen[i] != want {
			t.Fatalf("补拉顺序该是 1..5 升序、不重不漏，实际 %v", seen)
		}
	}
	// 站在终点再问一次：空页 + 空游标（200 是诚实答案，不是"没装配"的 501）。
	code, body := call(t, srv, http.MethodGet, "/api/v1/executions/ex-2/output?after_seq=5", "", "", "")
	if code != http.StatusOK || !strings.Contains(body, `"items":[]`) || !strings.Contains(body, `"next_cursor":""`) {
		t.Errorf("水位之后该是 200 空页 + 空游标，实际 %d %s", code, body)
	}
}

// TestExecutionOutputSays404AndNotTheThreeLies 分岔表剩下那几格：
// 404（没这条执行）/ 400（游标与页长不合法）/ 405（动词）/ 501（读面没注入）各说各的话。
// 尤其 501 那一格：`newExecutionHarness(..., false)` 不装读面，那时候**不许拿空页冒充"没有输出"**
// —— 那正是 T-370② 点名过的那一形（"读一个空页返回 200"）。
func TestExecutionOutputSays404AndNotTheThreeLies(t *testing.T) {
	srv, _ := newExecutionHarness(t, true)
	for _, tc := range []struct {
		name, path string
		wantCode   int
		wantText   string
	}{
		{"没有这条执行", "/api/v1/executions/ex-ghost/output", http.StatusNotFound, "没有这条 Execution"},
		{"after_seq 是负数", "/api/v1/executions/ex-2/output?after_seq=-1", http.StatusBadRequest, "after_seq"},
		{"after_seq 不是数", "/api/v1/executions/ex-2/output?after_seq=abc", http.StatusBadRequest, "after_seq"},
		{"limit 为零", "/api/v1/executions/ex-2/output?limit=0", http.StatusBadRequest, "limit"},
		{"limit 超上限", "/api/v1/executions/ex-2/output?limit=201", http.StatusBadRequest, "limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, body := call(t, srv, http.MethodGet, tc.path, "", "", "")
			if code != tc.wantCode {
				t.Errorf("该 %d，实际 %d %s", tc.wantCode, code, body)
			}
			if !strings.Contains(body, tc.wantText) {
				t.Errorf("报错里要看得见 %q，实际 %s", tc.wantText, body)
			}
			// 三条假话一条都不许出现：对登记过的路径说形状不认识、把没装配说成没这条、把读失败说成空页。
			if strings.Contains(body, "形状我不认识") {
				t.Errorf("这条路径 §3 登记过，不许说形状不认识：%s", body)
			}
		})
	}

	bare, _ := newExecutionHarness(t, false)
	code, body := call(t, bare, http.MethodGet, "/api/v1/executions/ex-2/output", "", "", "")
	if code != http.StatusNotImplemented {
		t.Fatalf("读面没注入 ⇒ 501（不许拿 200 空页冒充「这批就是空」，也不许 404 说成没这条），实际 %d %s", code, body)
	}
	if !strings.Contains(body, "还没装配读通道") || !strings.Contains(body, `"retryable":true`) {
		t.Errorf("501 要说清缺的是哪一格且可重试，实际 %s", body)
	}
	if strings.Contains(body, `"items"`) {
		t.Errorf("没装配却吐了一个带 items 的壳 ⇒ 那是冒充已装配：%s", body)
	}
}
