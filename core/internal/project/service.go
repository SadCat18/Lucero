package project

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/SadCat18/lucero/core/internal/eventjournal"
	"github.com/SadCat18/lucero/core/internal/platform/db"
)

// ErrInvalid 是输入本身不合法（缺 id、名字空白、status 不在词表）；transport 一律映射成 400。
var ErrInvalid = errors.New("project: invalid input")

// ErrPathConflict 是 §3 那句"canonicalize 后的 path 冲突返回 409"。
// 它**不是** UNIQUE 的转译件：先查一次是为了给出人话，UNIQUE 仍在后面兜底（并发/竞态时它才是真相）。
var ErrPathConflict = errors.New("project: canonical_path already taken")

// CreatedPayload 是事件目录 §6 给 project_created 的两个键，一个不多一个不少。
type CreatedPayload struct {
	Name          string `json:"name"`
	CanonicalPath string `json:"canonical_path"`
}

// Service 只写 projects 一张表 + 一条 project_created 事件；没有 adapter/process 依赖（§7 的禁令）。
type Service struct{ now func() int64 }

func NewService(now func() int64) *Service {
	if now == nil {
		now = func() int64 { return time.Now().UnixMilli() }
	}
	return &Service{now: now}
}

// Create 建一个项目：v1、恰一行 project_created。路径为空就写 NULL（不是空串）。
func (s *Service) Create(ctx context.Context, tx *db.Tx, d Draft) (Project, error) {
	if d.ID == "" {
		return Project{}, fmt.Errorf("%w: 缺 id（调用方生成 UUIDv7，重试要复用同一个）", ErrInvalid)
	}
	if d.EventID == "" {
		return Project{}, fmt.Errorf("%w: 缺 event_id", ErrInvalid)
	}
	if strings.TrimSpace(d.Name) == "" {
		return Project{}, fmt.Errorf("%w: name 不能为空", ErrInvalid)
	}
	path := Canonicalize(d.Path)
	metadata := d.MetadataJSON
	if strings.TrimSpace(metadata) == "" {
		metadata = "{}"
	}
	if !json.Valid([]byte(metadata)) {
		return Project{}, fmt.Errorf("%w: metadata 不是合法 JSON", ErrInvalid)
	}
	typeName := d.Type
	if typeName == "" {
		typeName = "UNKNOWN" // 与 000001 的 DEFAULT 同值，别在这里发明新默认
	}
	now := s.now() // 毫秒时钟，与 audit/task 那两处注入的 Now 同形

	if path != "" {
		var taken string
		err := tx.QueryRowContext(ctx,
			`SELECT id FROM projects WHERE canonical_path = ? AND archived_at IS NULL`, path).Scan(&taken)
		if err == nil {
			return Project{}, fmt.Errorf("%w: 路径 %s 已属于项目 %s", ErrPathConflict, path, taken)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return Project{}, fmt.Errorf("project: 查路径占用: %w", err)
		}
	}

	cols := []string{"name", "project_type", "status", "metadata_json", "created_at", "updated_at"}
	vals := []any{d.Name, typeName, string(Active), metadata, now, now}
	if path != "" { // 空路径必须是 NULL：空串是非 NULL，会白占那条部分唯一索引
		cols = append(cols, "canonical_path")
		vals = append(vals, path)
	}
	payload, err := json.Marshal(CreatedPayload{Name: d.Name, CanonicalPath: path})
	if err != nil {
		return Project{}, fmt.Errorf("project: project_created payload: %w", err)
	}
	m := map[string]any{}
	if err := json.Unmarshal(payload, &m); err != nil {
		return Project{}, fmt.Errorf("project: project_created payload: %w", err)
	}
	if err := tx.Create(db.AggProject, d.ID, cols, vals, db.EventDraft{
		EventID: d.EventID, Type: "project_created", Aggregate: db.AggProject, AggregateID: d.ID,
		CorrelationID: d.CorrelationID, CausationID: d.CausationID,
		PrincipalID: d.PrincipalID, Payload: m,
	}); err != nil {
		if isUniqueOnPath(err) {
			return Project{}, fmt.Errorf("%w: 路径 %s 已被占（唯一索引兜底）", ErrPathConflict, path)
		}
		return Project{}, fmt.Errorf("project: create %s: %w", d.ID, err)
	}
	return Project{ID: d.ID, Name: d.Name, CanonicalPath: path, Type: typeName, Status: Active,
		MetadataJSON: metadata, CreatedAt: now, UpdatedAt: now, Version: 1}, nil
}

// ErrVersionConflict 对应契约③ §2 的 409 `version_conflict`："我看到的不是你现在的"。
// 与 task 侧同义、同形状（各自包内一份，因为 transport 按包映射错误码）。
var ErrVersionConflict = errors.New("project: version conflict")

// Patch 是 `PATCH /api/v1/projects/{id}` 的落点：**只有 `name` 与 `metadata` 两格**。
// yaml 那句"只允许非状态字段；改状态必须走 `:verb`"在这里是**结构性**成立的 ——
// 状态类字段根本没有对应成员，所以"顺手把 status 改了"是写不出来的代码，不是被某个 if 挡住。
// 两个指针都是 nil = 这一列不动；`WantVersion <= 0` = 请求没带 If-Match（yaml 里它不是必填）。
type Patch struct {
	EventID     string // 与 Draft.EventID 同一规矩：调用方（transport）生成，本包不发明第三份 UUIDv7
	Name        *string
	Metadata    map[string]any
	WantVersion int64
}

// Update 走契约③ §1.1 第 11 条那套四步：库里读当前行（版本只从这儿来，不接受调用方口头给）
// → If-Match 比对 → 要改的列清单 → `Touch`（一个版本、一行 `project_updated`）。任何一步不过，一行都不写。
//
// 空 PATCH（两格都没给）**不写库、不推版本、不发事件**：`project_updated` 说的是"有东西变了"，
// 什么都没变还发一条，就是把事件写成仪式 —— 这与 §9 那条"不许发不登记/不成立的事件"同一条线。
func (s *Service) Update(ctx context.Context, tx *db.Tx, id string, p Patch) (Project, error) {
	if strings.TrimSpace(p.EventID) == "" {
		return Project{}, fmt.Errorf("%w: 缺 event_id（调用方生成，与 Draft 同一规矩）", ErrInvalid)
	}
	cur, err := Get(ctx, tx, id)
	if err != nil {
		return Project{}, err // 库里没有 ⇒ sql.ErrNoRows，交给 transport 映射 404
	}
	if p.WantVersion > 0 && p.WantVersion != cur.Version {
		return cur, fmt.Errorf("%w: 请求带 v%d，库里已是 v%d", ErrVersionConflict, p.WantVersion, cur.Version)
	}
	var (
		set  []string
		args []any
	)
	now := s.now()
	if p.Name != nil {
		if strings.TrimSpace(*p.Name) == "" {
			return cur, fmt.Errorf("%w: name 不能为空", ErrInvalid)
		}
		set, args = append(set, "name = ?"), append(args, *p.Name)
	}
	if p.Metadata != nil {
		raw, err := json.Marshal(p.Metadata)
		if err != nil {
			return cur, fmt.Errorf("%w: metadata 编不成 JSON: %v", ErrInvalid, err)
		}
		set, args = append(set, "metadata_json = ?"), append(args, string(raw))
	}
	if len(set) == 0 {
		return cur, nil
	}
	set, args = append(set, "updated_at = ?"), append(args, now)
	if _, err := tx.Touch(db.AggProject, id, strings.Join(set, ", "), args, db.EventDraft{
		EventID: p.EventID, Type: eventjournal.ProjectUpdated,
		Payload: map[string]any{"name": orDefault(cur.Name, p.Name), "metadata": metadataOf(cur, p)},
	}); err != nil {
		return cur, err
	}
	// C18 裁定 ② 的同一句话：**响应读回库里那一行**，不回显请求体 —— 版本与 updated_at 都以落库为准。
	return Get(ctx, tx, id)
}

func orDefault(cur string, p *string) string {
	if p == nil {
		return cur
	}
	return *p
}

// metadataOf 与 orDefault 同一条判据：没带 metadata 就把库里那份原样进 payload，别写个空对象冒充"改成了空"。
func metadataOf(cur Project, p Patch) map[string]any {
	if p.Metadata != nil {
		return p.Metadata
	}
	m := map[string]any{}
	if strings.TrimSpace(cur.MetadataJSON) != "" {
		_ = json.Unmarshal([]byte(cur.MetadataJSON), &m) // 库里过不了 json_valid 的早就被 CHECK 拦了
	}
	return m
}

func isUniqueOnPath(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") && strings.Contains(msg, "canonical_path")
}

// Queryer 是本包读侧用到的最小接口（同 internal/session 的理由：只声明自己要的那两条）。
type Queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

const projectCols = `id, trust_domain_id, name, canonical_path, project_type, status,
	metadata_json, created_at, updated_at, archived_at, version`

// Get 读回真那一行。C18 裁定 ② 要求"首次响应也回读库"，所以 transport 建完必须走它。
func Get(ctx context.Context, q Queryer, id string) (Project, error) {
	p, err := scanProject(q.QueryRowContext(ctx,
		`SELECT `+projectCols+` FROM projects WHERE id = ?`, id))
	if err != nil {
		return Project{}, fmt.Errorf("project: get %s: %w", id, err)
	}
	return p, nil
}

// ListFilter 是 GET /api/v1/projects 的条件。契约（yaml 那条 get 的 parameters）只给了 cursor + limit，
// 所以这里**没有** status 过滤位，归档的也在列表里 —— 要过滤得等契约开口，不在我这边自挑（C26 同一课）。
type ListFilter struct {
	AfterCreatedAt int64
	AfterID        string
	Limit          int
}

// ListPage 按 (created_at, id) 升序出一页。这个序与 internal/task 那条 list 相同，
// 游标也是同一个二元形状，所以 transport 复用 encodeCursor/decodeCursor，不再造第三份编解码
// （同步游标那套口径相反，见 api/http/tasks.go 末尾那一句）。
// 翻页是 keyset 谓词而不是 OFFSET（契约③ §2 禁 offset）；返回值的最后一行就是下一页游标的原料。
func ListPage(ctx context.Context, q Queryer, f ListFilter) ([]Project, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	// "1=1" 打底是 internal/task 那条 list 的写法：条件全空时也不会拼出 `WHERE  ORDER BY`
	// （notification 那口今天刚为此红过一次，见第 139 条）。
	where := []string{"1=1"}
	args := []any{}
	if f.AfterID != "" {
		where = append(where, "(created_at > ? OR (created_at = ? AND id > ?))")
		args = append(args, f.AfterCreatedAt, f.AfterCreatedAt, f.AfterID)
	}
	args = append(args, limit)
	rows, err := q.QueryContext(ctx, `SELECT `+projectCols+` FROM projects WHERE `+
		strings.Join(where, " AND ")+` ORDER BY created_at, id LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("project: list: %w", err)
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, fmt.Errorf("project: list scan: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

type rowScanner interface{ Scan(dest ...any) error }

// scanProject 是 Get 与 ListPage 共用的那份读法（可空列一律走 Null*，不在这儿折成空串——
// 折不折得开是 transport 那一层显式出 null 的事，见 api/http/projects.go 的 nullableString）。
func scanProject(row rowScanner) (Project, error) {
	var (
		p                         Project
		trust, path               sql.NullString
		archived                  sql.NullInt64
		status                    string
		metadata, typeName        string
		created, updated, version int64
	)
	if err := row.Scan(&p.ID, &trust, &p.Name, &path, &typeName, &status, &metadata,
		&created, &updated, &archived, &version); err != nil {
		return Project{}, err
	}
	if !Status(status).Valid() {
		return Project{}, fmt.Errorf("project: %s: status %q 不在 §7 词表里", p.ID, status)
	}
	p.TrustDomainID, p.CanonicalPath, p.Type, p.Status = trust.String, path.String, typeName, Status(status)
	p.MetadataJSON, p.CreatedAt, p.UpdatedAt, p.ArchivedAt, p.Version = metadata, created, updated, archived.Int64, version
	return p, nil
}
