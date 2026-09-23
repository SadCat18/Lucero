package session

// `POST /api/v1/adapters/{id}/sessions:discover` 的域半边（T-198 把它提到链首：
// 面板链第 2/3/4 格共用这一把锁，Wave-2 的"真子进程 Task→ACCEPTED"也要先留下 session 行才谈得上后续）。
//
// 分工照契约③ §7.2 那句"写就在归属包里"：连接与协议在 `internal/adapter` / `internal/process`
// （它们**不写库**，包注释明说过），**建行与事件在这里**。

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// DiscoveredRemote 是 §4 `session/list` 的一行（协议 §4 那行明写"四个字段，no more"）。
type DiscoveredRemote struct {
	RemoteSessionID string
	Title           string
	ProjectPathHint string
	UpdatedAt       int64
}

// Discovered 是一次发现之后本地那一行的身份：`DiscoveredSession`（yaml:1017）要的就是这两格。
type Discovered struct {
	SessionID       string `json:"session_id"`
	RemoteSessionID string `json:"remote_session_id"`
}

type DiscoverResult struct {
	Items []Discovered
	// Created 是这一批里真建了几行（幂等重放时它是 0，但响应形状与首次一致）。
	Created int
}

type DiscoverCommand struct {
	// AdapterInstanceID 是 `adapter_instances.id`（路径参数 `{id}` 指的是实例 id，不是 adapter_key ——
	// 与 probe 那条不同，那一条 yaml 明写"路径参数 `key` 是 `adapter_key`"）。
	AdapterInstanceID string
	Rows              []DiscoveredRemote
	// NewID 由装配方给（Core 全仓只有一处 UUIDv7 生成器，业务包不各造一份随机源）。
	NewID func() string
}

// UpsertDiscovered 按 `(adapter_instance_id, remote_session_id)` 去重 upsert（T-204①：库里的唯一键就是这两列，
// `idx_sessions_remote_unique` 还带 `WHERE remote_session_id IS NOT NULL` —— 只说"按 remote_session_id"是不严的写法，
// 跨实例同 id 是两行、不是同一行）：**已存在的行一行都不改、一条事件都不发**，
// 只把它的 id 回进列表（yaml 那条 description 就是"重复 discover 不产生新行，只返回同一批 session id"）。
// 新行落 `Tx.Create(db.AggSession, …)` + 恰一行 `session_discovered`（同一事务）。
//
// 同页两条同样的 `remote_session_id` ⇒ 整批拒 `ErrInvalidCommand`（T-204③ 收这项，并把 transport 那一格定在
// **400 `validation_failed`** —— 见 `api/http/adapters.go` 的 discover 分流）。
//
// `project_id` 留空是**契约原意**：§6:176 那一格 `session_discovered` 的 payload 只有
// `remote_session_id` `adapter_instance_id` 两键，没有归属；绑项目是 `:manage` 的事（C41/T-196 已裁）。
// 所以这里既不查项目、也不接 `project_id` —— 顺手绑上就是拿一次"拉清单"去做归属改动。
func (s *Service) UpsertDiscovered(ctx context.Context, tx Writer, cmd DiscoverCommand) (DiscoverResult, error) {
	if cmd.AdapterInstanceID == "" {
		return DiscoverResult{}, fmt.Errorf("%w: 缺 adapter_instance_id（sessions 这一列是 NOT NULL 外键）", ErrInvalidCommand)
	}
	if cmd.NewID == nil {
		return DiscoverResult{}, fmt.Errorf("%w: 没给 NewID（本地行 id 必须由调用方预生成，重试复用同一批）", ErrInvalidCommand)
	}
	seen := make(map[string]struct{}, len(cmd.Rows))
	for i, r := range cmd.Rows {
		if r.RemoteSessionID == "" {
			return DiscoverResult{}, fmt.Errorf("%w: 第 %d 行没有 remote_session_id，去重没有依据", ErrInvalidCommand, i)
		}
		if _, dup := seen[r.RemoteSessionID]; dup {
			return DiscoverResult{}, fmt.Errorf("%w: 同一页里 %q 出现了两次，宁可整批拒（拿后一条覆盖前一条就是静默丢事实）",
				ErrInvalidCommand, r.RemoteSessionID)
		}
		seen[r.RemoteSessionID] = struct{}{}
	}

	existing, err := s.repo.RemoteIndex(ctx, cmd.AdapterInstanceID)
	if err != nil {
		return DiscoverResult{}, err
	}
	out := DiscoverResult{Items: make([]Discovered, 0, len(cmd.Rows))}
	for _, r := range cmd.Rows {
		if id, ok := existing[r.RemoteSessionID]; ok {
			out.Items = append(out.Items, Discovered{SessionID: id, RemoteSessionID: r.RemoteSessionID})
			continue
		}
		id := cmd.NewID()
		cols := []string{"adapter_instance_id", "remote_session_id", "managed", "created_at", "updated_at", "metadata_json"}
		vals := []any{cmd.AdapterInstanceID, r.RemoteSessionID, 0, s.now(), s.now(), metadataOf(r)}
		if r.Title != "" {
			cols = append(cols, "title")
			vals = append(vals, r.Title)
		}
		payload, err := payloadOf(DiscoveredPayload{RemoteSessionID: r.RemoteSessionID, AdapterInstanceID: cmd.AdapterInstanceID})
		if err != nil {
			return DiscoverResult{}, fmt.Errorf("session: discovered payload: %w", err)
		}
		if err := tx.Create(db.AggSession, id, cols, vals, db.EventDraft{
			EventID: cmd.NewID(), Type: EventDiscovered, Aggregate: db.AggSession, AggregateID: id,
			Payload: payload,
		}); err != nil {
			return DiscoverResult{}, fmt.Errorf("session: create %s: %w", id, err)
		}
		existing[r.RemoteSessionID] = id
		out.Items = append(out.Items, Discovered{SessionID: id, RemoteSessionID: r.RemoteSessionID})
		out.Created++
	}
	return out, nil
}

// metadataOf 把 §4 给的两个"本地没有列"的字段收进 metadata_json：`project_path_hint` 与远端的 `updated_at`。
// 为什么不新开列：迁移是 `db` 角色的地盘，我不越界；为什么不丢：§4 那行给了它，
// 丢了就再也问不回来（下一次 discover 想按"远端比本地新"排序就没依据了）。
func metadataOf(r DiscoveredRemote) string {
	meta := map[string]any{
		"schema": 1, "remote_updated_at": r.UpdatedAt,
	}
	if r.ProjectPathHint != "" {
		meta["project_path_hint"] = r.ProjectPathHint
	}
	b, err := json.Marshal(meta)
	if err != nil {
		// map[string]any 里只有字符串与整数，编码不可能失败；真失败了就是上面加了不该加的东西。
		return `{}`
	}
	return string(b)
}
