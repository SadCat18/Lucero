package main

// discoverassembly 是 `POST /api/v1/adapters/{id}/sessions:discover` 的装配半边（T-198 把它提到链首）。
//
// 三件事决定了它长这样：
//   - **协议与连接不在我手里**：`internal/process.Connection` 已经有 `Connect`/`Initialize`/`Call`，
//     那两个包按设计不写库（包注释自说），所以"拉清单"用它们、"建行与发事件"回 `internal/session`。
//   - **路径参数是实例 id，不是 adapter_key**（yaml `:207` 那条用的是 `parameters/Id`；probe 那条明写 `key`）。
//     所以这里要先 `adapter_instances` 反查出 `adapter_key`，再去查"这个 key 本机有没有起法"。
//     反查不到 = 404（对象不存在）；有实例但本机没配起法 = 501（这台机器上从来没有过这条能力）——
//     两者与 503（那个 Adapter 病了）是三件事，混成一个就是把处置信息丢给调用方。
//   - **v0.1 不做连接池**：一次 discover 现起一条连接、拉完就关。盘上没有任何"活连接登记表"，
//     自己造一个就是替所有人决定"连接归谁持有、什么时候算死"，那是 REQ-AD-1 之外的新语义 ⇒ 记 **C42**。

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	httpapi "github.com/SadCat18/lucero/core/api/http"
	"github.com/SadCat18/lucero/core/internal/adapter"
	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/session"
)

// discoverPageLimit 与 discoverMaxPages 是 §4 分页的两道闸：单页 200 条、最多 20 页。
// 触顶而 `next_cursor` 还在 ⇒ **报错**，不返回"看起来成功的前 4000 条"（那正是 §12 那一族：
// 拿部分结果冒充完整清单，面板会以为远端就只有这些）。
const (
	discoverPageLimit = 200
	discoverMaxPages  = 20
)

type discoverService struct {
	dbh    *db.DB
	read   *sql.DB
	launch *launcher
	byKey  map[string]adapterSpec
	svc    *session.Service
	logf   func(format string, args ...any)
}

// coreProtocolVersion 是 §2.2 要的 "M.m" 写法（`adapter.Version` 那份是给 Registry 的，这里是给连接的）。
func coreProtocolVersion() string {
	return fmt.Sprintf("%d.%d", adapter.ProtocolMajor, adapter.ProtocolMinor)
}

// sessionListRow / sessionListPage 是 §4 `session/list` 回执的本地读法（写在这里而不是复用 fake 的私有类型：
// Core 与 Adapter 各自持有一份"对方说了什么"的形状，谁改了另一边的解码就该红，而不是共享一个类型把漂移藏起来）。
type sessionListRow struct {
	RemoteSessionID string `json:"remote_session_id"`
	Title           string `json:"title"`
	ProjectPathHint string `json:"project_path_hint"`
	UpdatedAt       int64  `json:"updated_at"`
}

type sessionListPage struct {
	Sessions   []sessionListRow `json:"sessions"`
	NextCursor string           `json:"next_cursor"`
}

func (d *discoverService) DiscoverSessions(ctx context.Context, adapterInstanceID string) ([]session.Discovered, int, error) {
	var adapterKey string
	if err := d.read.QueryRowContext(ctx,
		`SELECT adapter_key FROM adapter_instances WHERE id = ?`, adapterInstanceID).Scan(&adapterKey); err != nil {
		// sql.ErrNoRows 原样往上抛：transport 认它 ⇒ 404（"先 probe 一次让它登记"）。
		return nil, 0, err
	}
	if _, ok := d.byKey[adapterKey]; !ok {
		return nil, 0, fmt.Errorf("%w: %s（要 -adapter %s=<可执行路径>）",
			httpapi.ErrAdapterNotLaunchable, adapterKey, adapterKey)
	}

	// 连接这条路必须有期限：`process.Connection` 的读泵挂在管道上，没期限就是一个挂住的 Adapter
	// 拖住一条 handler 线程 —— 与 T-132 那两条死锁同一个形状，所以在这里补默认值。
	cctx := ctx
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		cctx, cancel = context.WithTimeout(ctx, probeAttemptDeadline)
		defer cancel()
	}

	conn, err := d.launch.supervisorFor(adapterKey).Connect(cctx, coreProtocolVersion())
	if err != nil {
		return nil, 0, fmt.Errorf("discover %s: 起连接: %w", adapterKey, err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			// 连接关不掉不能改成"整个请求失败"：行已经该落的就落了，喊出来才是诚实的处置。
			d.logf("discover %s: 关闭连接失败: %v", adapterKey, err)
		}
	}()
	if _, err := conn.Initialize(cctx); err != nil {
		return nil, 0, fmt.Errorf("discover %s: 握手: %w", adapterKey, err)
	}

	rows, err := listAllSessions(cctx, conn, adapterKey)
	if err != nil {
		return nil, 0, err
	}

	var out session.DiscoverResult
	if err := d.dbh.Write(cctx, func(tx *db.Tx) error {
		var err error
		out, err = d.svc.UpsertDiscovered(cctx, tx, session.DiscoverCommand{
			AdapterInstanceID: adapterInstanceID, Rows: rows,
			// 复用全仓那一个 UUIDv7 生成器（adapter.NewRequestID），不在这里再造第四份随机源。
			NewID: func() string { return adapter.NewRequestID().String() },
		})
		return err
	}); err != nil {
		return nil, 0, err
	}
	return out.Items, out.Created, nil
}

// listAllSessions 按 §4 的 `next_cursor` 把清单拉完（有界：discoverMaxPages 页）。
func listAllSessions(ctx context.Context, conn sessionCaller, adapterKey string) ([]session.DiscoveredRemote, error) {
	var (
		all    []session.DiscoveredRemote
		cursor string
	)
	for page := 1; page <= discoverMaxPages; page++ {
		params := map[string]any{"limit": discoverPageLimit}
		if cursor != "" {
			params["cursor"] = cursor
		}
		f, err := conn.Call(ctx, "session/list", params)
		if err != nil {
			return nil, fmt.Errorf("discover %s: session/list 第 %d 页: %w", adapterKey, page, err)
		}
		var res sessionListPage
		if err := json.Unmarshal(f.Result, &res); err != nil {
			return nil, fmt.Errorf("discover %s: session/list 的回执不是 §4 的形状: %w", adapterKey, err)
		}
		for _, s := range res.Sessions {
			all = append(all, session.DiscoveredRemote{
				RemoteSessionID: s.RemoteSessionID, Title: s.Title,
				ProjectPathHint: s.ProjectPathHint, UpdatedAt: s.UpdatedAt,
			})
		}
		if res.NextCursor == "" {
			return all, nil
		}
		cursor = res.NextCursor
	}
	return nil, fmt.Errorf("discover %s: 远端清单在 %d 页之后还没完（单页 %d），宁可报错也不交一份看起来完整的部分清单",
		adapterKey, discoverMaxPages, discoverPageLimit)
}

// sessionCaller 是 `listAllSessions` 真正用到的那一格能力（`*process.Connection` 满足它）。
// 抽出来的理由和别处一样：让这段能拿一个替身测"分页拉到底"与"触顶报错"，不用起真子进程。
type sessionCaller interface {
	Call(ctx context.Context, method string, params any) (*adapter.Frame, error)
}
