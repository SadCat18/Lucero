package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/project"
)

// ProjectService 由 internal/project 的 *Service 满足（依赖方向合法：transport → 业务包）。
// canonicalize、路径冲突判定、事件写入全在业务包里 —— 这里只做解析/状态码（契约③ §7 薄层禁令）。
type ProjectService interface {
	Create(ctx context.Context, tx *db.Tx, d project.Draft) (project.Project, error)
}

type createProjectBody struct {
	Name          string         `json:"name"`
	CanonicalPath *string        `json:"canonical_path"`
	ProjectType   string         `json:"project_type"`
	Metadata      map[string]any `json:"metadata"`
}

// projectView 的必填六键来自 core-v1.yaml 的 Project.required；
// 四条可空项一律显式出 null（不接受"省略 / 空串"当第三种"没有"，同 REQ-F-21/22 那条线）。
type projectView struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	CanonicalPath *string        `json:"canonical_path"`
	ProjectType   string         `json:"project_type"`
	Status        string         `json:"status"`
	TrustDomainID *string        `json:"trust_domain_id"`
	Metadata      map[string]any `json:"metadata"`
	Version       int64          `json:"version"`
	CreatedAt     int64          `json:"created_at"`
	UpdatedAt     int64          `json:"updated_at"`
	ArchivedAt    *int64         `json:"archived_at"`
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nullableMillis(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}

// projectViewOf 把库里那一行投成 yaml 的形状。metadata 是 json_valid 列，解不出来就是真出事，
// 不许悄悄回空对象掩盖（那会把"存储坏了"报成"这个项目没有元数据"）。
func projectViewOf(p project.Project) (projectView, error) {
	metadata := map[string]any{}
	if strings.TrimSpace(p.MetadataJSON) != "" {
		if err := json.Unmarshal([]byte(p.MetadataJSON), &metadata); err != nil {
			return projectView{}, err
		}
	}
	return projectView{ID: p.ID, Name: p.Name, CanonicalPath: nullableString(p.CanonicalPath),
		ProjectType: p.Type, Status: string(p.Status), TrustDomainID: nullableString(p.TrustDomainID),
		Metadata: metadata, Version: p.Version, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
		ArchivedAt: nullableMillis(p.ArchivedAt)}, nil
}

// registerProjectRoutes：POST 与 GET 两条腿按 C28② 那句通用口径分流 ——
// **路由存在、方法合法、能力没接线 ⇒ 501；方法本身不被这条路径接受 ⇒ 405**。
// 所以：注入了读通道之前 GET 回 501（不是 405，"这条路径不接受 GET"是假话），
// 而 PUT/DELETE 这类路径根本不收的方法回 405。
func registerProjectRoutes(mux *http.ServeMux, c Config) {
	if c.Projects == nil || c.Store == nil {
		return
	}
	mux.HandleFunc("/api/v1/projects", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			createProject(w, r, c)
		case http.MethodGet:
			if c.ProjectRead == nil {
				writeError(w, http.StatusNotImplemented, "not_implemented",
					"GET /api/v1/projects 这一腿还没装配读通道；POST 已可用", true)
				return
			}
			listProjects(w, r, c)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed",
				"/api/v1/projects 只接受 GET 与 POST", false)
		}
	})
}

// listProjects 只做解析/校验/投影：翻页与排序规则住在 project.ListPage（契约③ §7）。
// 游标沿用**分页**那一套（`next_cursor: ""` 是"没有下一页"的唯一写法，REQ-F-19），
// 与 tasks 的编解码同形，所以直接复用 encodeCursor/decodeCursor —— 第三份编解码只能给同步游标。
func listProjects(w http.ResponseWriter, r *http.Request, c Config) {
	q := r.URL.Query()
	limit := 50
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > 200 {
			writeError(w, http.StatusBadRequest, "validation_failed", "limit 必须是 1~200", false)
			return
		}
		limit = n
	}
	f := project.ListFilter{Limit: limit}
	if cur := q.Get("cursor"); cur != "" {
		at, id, err := decodeCursor(cur)
		if err != nil {
			writeError(w, http.StatusBadRequest, "validation_failed", "cursor 不合法", false)
			return
		}
		f.AfterCreatedAt, f.AfterID = at, id
	}
	items, err := project.ListPage(r.Context(), c.ProjectRead, f)
	if err != nil {
		serverError(w, r, err)
		return
	}
	views := make([]projectView, 0, len(items))
	for _, p := range items {
		v, err := projectViewOf(p)
		if err != nil {
			serverError(w, r, err)
			return
		}
		views = append(views, v)
	}
	next := ""
	if len(items) == limit && len(items) > 0 {
		last := items[len(items)-1]
		next = encodeCursor(last.CreatedAt, last.ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": views, "next_cursor": next})
}

func createProject(w http.ResponseWriter, r *http.Request, c Config) {
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		writeError(w, http.StatusForbidden, "idempotency_key_required",
			"POST /api/v1/projects 是有副作用的写，必须带 Idempotency-Key", false)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "读请求体失败", true)
		return
	}
	var body createProjectBody
	if err := json.Unmarshal(raw, &body); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "请求体不是合法 JSON: "+err.Error(), false)
		return
	}
	principal := r.Header.Get("X-Lucero-Principal")
	correlation := newUUIDv7()
	id := newUUIDv7() // 重试复用同一个 id：所以它必须由 handler 生成、而不是让库里生成

	path := ""
	if body.CanonicalPath != nil {
		path = *body.CanonicalPath
	}
	metadata := []byte("{}")
	if body.Metadata != nil {
		if metadata, err = json.Marshal(body.Metadata); err != nil {
			writeError(w, http.StatusBadRequest, "validation_failed", "metadata 编不成 JSON: "+err.Error(), false)
			return
		}
	}

	var sealed string
	err = c.Store.Write(r.Context(), func(tx *db.Tx) error {
		s, err := claimIdempotency(r.Context(), tx, key, principal, "project_created", hashOf(raw))
		if err != nil {
			return err
		}
		if s != "" {
			sealed = s // 已封存 → 原样回首次响应，一个副作用都不再发生
			return nil
		}
		if _, err := c.Projects.Create(r.Context(), tx, project.Draft{
			ID: id, EventID: newUUIDv7(), Name: body.Name, Path: path, Type: body.ProjectType,
			MetadataJSON: string(metadata), PrincipalID: principal,
			CorrelationID: correlation, CausationID: correlation,
		}); err != nil {
			return err
		}
		// C18 裁定 ②：首次响应也回读库，别让"对外说的"和"库里存的"各说一套（脱敏在库那边跑）。
		created, err := project.Get(r.Context(), tx, id)
		if err != nil {
			return err
		}
		view, err := projectViewOf(created)
		if err != nil {
			return err
		}
		if sealed, err = sealIdempotency(r.Context(), tx, key, view); err != nil {
			return err
		}
		return nil
	})

	switch {
	case err == nil:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(sealed))
	case errors.Is(err, errReplay):
		writeError(w, http.StatusConflict, "idempotency_replay",
			"同一 Idempotency-Key 的首次请求还没完成，暂时没有可回放的响应", true)
	case errors.Is(err, errKeyMismatch):
		writeError(w, http.StatusConflict, "idempotency_mismatch",
			"同一 Idempotency-Key 带了不同的请求体（键含 request_hash）", false)
	case errors.Is(err, project.ErrPathConflict):
		writeError(w, http.StatusConflict, "duplicate_project_path",
			"canonicalize 后的路径已属于另一个未归档项目", false)
	case errors.Is(err, project.ErrInvalid):
		writeError(w, http.StatusBadRequest, "validation_failed", err.Error(), false)
	default:
		serverError(w, r, err)
	}
}
