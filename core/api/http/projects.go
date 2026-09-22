package httpapi

import (
	"context"
	"database/sql"
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
	Update(ctx context.Context, tx *db.Tx, id string, p project.Patch) (project.Project, error)
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

// registerProjectItemRoute 是 `/api/v1/projects/{id}` 这一条**子树**路径（与上面那条精确路径是两个 pattern，
// Go 的 mux 允许并存）。分流仍按 C28②：PATCH 已接；GET 单条是 yaml 里 declared 但还没接线的能力 ⇒ 501
// （不是 405 —— "这条路径不接受 GET"是假话）；PUT/DELETE 这类根本不被接受 ⇒ 405。
func registerProjectItemRoute(mux *http.ServeMux, c Config) {
	if c.Projects == nil || c.Store == nil {
		return
	}
	mux.HandleFunc("/api/v1/projects/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/projects/")
		if id == "" || strings.Contains(id, "/") || strings.Contains(id, ":") {
			// 空 id 或多一段：形状不认识，不拿它去查库（那会回一个假装合法的 404）。
			// 带冒号的那种（`:archive` 之类）今天根本没登记 ⇒ 501，但这里连形状都不认，别猜。
			writeError(w, http.StatusNotImplemented, "not_implemented",
				"projects 的单条路径只认 {id}；带 custom verb 的那类还没接线", true)
			return
		}
		switch r.Method {
		case http.MethodPatch:
			updateProject(w, r, c, id)
		case http.MethodGet:
			if c.ProjectRead == nil {
				writeError(w, http.StatusNotImplemented, "not_implemented",
					"GET /api/v1/projects/{id} 还没装配读通道", true)
				return
			}
			getProject(w, r, c, id)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed",
				"/api/v1/projects/{id} 只接受 GET 与 PATCH", false)
		}
	})
}

// getProject 是单条读：`project.Get` 读库里那一行，投影复用 `projectViewOf`。
// 归档的项目**照样读得到** —— 这条路契约里没给任何过滤位，我不替它藏（C26 同一课）。
func getProject(w http.ResponseWriter, r *http.Request, c Config, id string) {
	p, err := project.Get(r.Context(), c.ProjectRead, id)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows), errors.Is(err, db.ErrAggregateNotFound):
			writeError(w, http.StatusNotFound, "not_found", "没有这个项目", false)
		default:
			serverError(w, r, err)
		}
		return
	}
	view, err := projectViewOf(p)
	if err != nil {
		serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// updateProject 只做解析/校验/状态码（契约③ §7）：版本比对与"改哪几列"全在 project.Service.Update 里。
func updateProject(w http.ResponseWriter, r *http.Request, c Config, id string) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "读请求体失败", true)
		return
	}
	// DisallowUnknownFields 不是洁癖：`status` 在词表里是**存在**的列，静默丢掉它等于让客户端以为改了。
	// yaml 把这条口的语义写死成"只允许非状态字段；改状态必须走 :verb" ⇒ 不认识的键就 400，不猜。
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	var body updateProjectBody
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "请求体不合法: "+err.Error(), false)
		return
	}
	var want int64
	if v := r.Header.Get("If-Match"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "validation_failed", "If-Match 必须是正整数版本", false)
			return
		}
		want = n
	}
	p := project.Patch{EventID: newUUIDv7(), WantVersion: want, Metadata: body.Metadata}
	if body.Name != nil {
		name := *body.Name
		p.Name = &name
	}
	var out project.Project
	err = c.Store.Write(r.Context(), func(tx *db.Tx) error {
		var err error
		out, err = c.Projects.Update(r.Context(), tx, id, p)
		return err
	})
	view, viewErr := projectViewOf(out)
	switch {
	case err == nil && viewErr == nil:
		writeJSON(w, http.StatusOK, view)
	case err == nil:
		serverError(w, r, viewErr)
	case errors.Is(err, project.ErrVersionConflict):
		writeError(w, http.StatusConflict, "version_conflict", err.Error(), true)
	case errors.Is(err, project.ErrInvalid):
		writeError(w, http.StatusBadRequest, "validation_failed", err.Error(), false)
	case errors.Is(err, sql.ErrNoRows) || errors.Is(err, db.ErrAggregateNotFound):
		writeError(w, http.StatusNotFound, "not_found", "没有这个项目", false)
	default:
		serverError(w, r, err)
	}
}

// updateProjectBody 两个键都是可选的，但**都不给**是合法的 no-op（见 Service.Update 那一段）。
// 没有 status / archived_at 的位置：那是结构性约束，不是运行时的 if。
type updateProjectBody struct {
	Name     *string        `json:"name"`
	Metadata map[string]any `json:"metadata"`
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
