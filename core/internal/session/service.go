package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// 这里是 `POST /api/v1/sessions/{id}:sync` 的判定半边（C25 裁的那句："先看有没有只读链可走"）。
// transport 那半边只许按下面三条哨兵错误映射状态码 —— 契约③ §7 不许业务判断住在 handler 里。

// ErrSessionNotFound 是"这条 Session 根本不在本地库里"。它与 ErrNoLocalData 是两件事：
// 前者是对象不存在（§15.2 → 404 `session_not_found`），后者是对象在、但没有能给你的东西（→ 503）。
var ErrSessionNotFound = errors.New("session: 没有这条 Session")

// ErrNoLocalData 是 C25 里"完全无 Provider"那一支今天在本地可观察到的形态：
// 既没有一条消息可回放，也没有一个"上次同步到哪"的位置可回显 —— 于是也没有可用 Provider 可问。
var ErrNoLocalData = errors.New("session: 本地没有可回放的内容，也没有可用 Provider")

// SyncOutcome 是一次降级同步要回的那几格。没有 AddedMessages：这条口今天**不知道**新增了几条，
// 而 §12 禁的正是拿 `0` 冒充"不知道"（C25 回执里主控也点了这一句）。
type SyncOutcome struct {
	State      SyncState
	Cursor     *string
	Confidence Confidence
}

// Service 的只读那一格（`:sync` 走降级链）今天仍然一行都不写；写侧从第四十五批起多了一条 `Manage`
// （见 manage.go —— 它是纯聚合的一次写，不依赖 adapter 的连接层）。
// 两个方法要的能力不同，所以 `Manage` 自己吃一个 `TouchWriter`，这里不把事务通道存进 Service：
// 存进来就等于允许"拿构造时的库、在别的请求里写"。
type Service struct {
	repo *Repository
	// now 只给写侧用（`updated_at`）。缺省取墙钟，装配方（cmd/core）会把它那份共享时钟传进来。
	now func() int64
}

func NewService(repo *Repository, clock ...func() int64) *Service {
	now := nowMillisNow
	if len(clock) > 0 && clock[0] != nil {
		now = clock[0]
	}
	return &Service{repo: repo, now: now}
}

// SyncFromLocal 走只读链：库里这条 Session 有内容就把它回给调用方，并且**明写这是降级**
// （`sync_state=DEGRADED` + `state_confidence=LOW`），不拿"看起来成功"冒充真同步。
//
// 这一条今天一行都不写：没有推进聚合就不许发 `session_synced`（事件契约 §9 禁止事项 1 的同一条线），
// 所以也不占 If-Match、不留幂等记录。
//
// 置信度固定 LOW 而不是照抄库里那一格：库里存的可能是昨天从官方实时接口拿到的 HIGH，
// 而这次回答只读了本地字节 —— §49 的原话是"如果只是根据推断：不能标 HIGH"，
// 把旧的 HIGH 端出来就是拿昨天的置信度冒充今天的。
func (s *Service) SyncFromLocal(ctx context.Context, id string) (SyncOutcome, error) {
	sess, err := s.repo.Get(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return SyncOutcome{}, ErrSessionNotFound
	}
	if err != nil {
		return SyncOutcome{}, err
	}
	// limit=1 是存在性探针，不是取一页消息：这条口今天不把消息体外发给调用方。
	msgs, err := s.repo.ListMessages(ctx, id, 1)
	if err != nil {
		return SyncOutcome{}, fmt.Errorf("session: sync %s: %w", id, err)
	}
	if len(msgs) == 0 && sess.SyncCursor == nil {
		return SyncOutcome{}, ErrNoLocalData
	}
	return SyncOutcome{State: SyncDegraded, Cursor: sess.SyncCursor, Confidence: ConfidenceLow}, nil
}
