package project

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

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
}

const projectCols = `id, trust_domain_id, name, canonical_path, project_type, status,
	metadata_json, created_at, updated_at, archived_at, version`

// Get 读回真那一行。C18 裁定 ② 要求"首次响应也回读库"，所以 transport 建完必须走它。
func Get(ctx context.Context, q Queryer, id string) (Project, error) {
	var (
		p                         Project
		trust, path               sql.NullString
		archived                  sql.NullInt64
		status                    string
		metadata, typeName        string
		created, updated, version int64
	)
	err := q.QueryRowContext(ctx, `SELECT `+projectCols+` FROM projects WHERE id = ?`, id).
		Scan(&p.ID, &trust, &p.Name, &path, &typeName, &status, &metadata, &created, &updated, &archived, &version)
	if err != nil {
		return Project{}, fmt.Errorf("project: get %s: %w", id, err)
	}
	if !Status(status).Valid() {
		return Project{}, fmt.Errorf("project: get %s: status %q 不在 §7 词表里", id, status)
	}
	p.TrustDomainID, p.CanonicalPath, p.Type, p.Status = trust.String, path.String, typeName, Status(status)
	p.MetadataJSON, p.CreatedAt, p.UpdatedAt, p.ArchivedAt, p.Version = metadata, created, updated, archived.Int64, version
	return p, nil
}
