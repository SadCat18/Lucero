// Package project 是 Project 聚合的唯一拥有者（契约③ §7 的 REQ-P-1 行，T-130② 派工）。
// 那一行给的边界就是本包的边界：**只做聚合 CRUD 与版本推进** ——
// 不 import adapter/process，不在这发探针，也不把 canonicalize 留给 transport 去做（§7：薄层不得含业务判断）。
package project

import "strings"

// Status 对齐 migrations/000001 的 CHECK 与 §7 词表（四个值，一个不多）。
type Status string

const (
	Active      Status = "ACTIVE"
	Archived    Status = "ARCHIVED"
	Unavailable Status = "UNAVAILABLE"
	Unknown     Status = "UNKNOWN"
)

func (s Status) Valid() bool {
	switch s {
	case Active, Archived, Unavailable, Unknown:
		return true
	}
	return false
}

// Project 是 projects 表那一行的形状。可空列一律用空串/0 表示"库里是 NULL"是**读侧**的折叠，
// 出口要 null 的字段由 transport 负责还原（同 notification 那三个时间戳，见 progress 的已知缺口）。
type Project struct {
	ID            string
	TrustDomainID string
	Name          string
	CanonicalPath string
	Type          string
	Status        Status
	MetadataJSON  string
	CreatedAt     int64
	UpdatedAt     int64
	ArchivedAt    int64
	Version       int64
}

// Draft 是建项目输入。ID 必须由调用方预生成（UUIDv7）：重试要复用同一个，
// 否则同一次点击会留下第二个项目（领域模型「Idempotency」、契约① §3）。
type Draft struct {
	ID           string
	EventID      string
	Name         string
	Path         string // 原样传进来，收不收成键由 Canonicalize 决定
	Type         string
	MetadataJSON string

	PrincipalID   string
	CorrelationID string
	CausationID   string
}

// Canonicalize 把"同一个目录的不同写法"收成一把键。
// 领域模型 §7 给的反例是硬要求：`D:\MZO`、`D:/MZO`、`\\?\D:\MZO` 必须同键 ——
// 因为 `idx_projects_canonical_path` 那条部分唯一索引只在**字符串相等**时判冲突。
//
// 规则（刻意只做文本层，不 stat、不解析符号链接、不读文件系统：那属于 project_paths 的 alias/detected 那条线）：
//  1. 去掉 `\\?\` 与 `\\.\` 命名空间前缀；
//  2. 分隔符统一成反斜杠、折叠重复、丢掉 `.` 段与尾部分隔符；
//  3. 盘符留原样大小写不进键 —— 整条**小写**作键（要区分大小写就得改那条冻结索引的 COLLATE，v0.1 不做）。
//
// 空串 = "没有路径" ⇒ 返回 ""，调用方据此写 NULL。部分唯一索引明确豁免 NULL，
// 所以"没填路径的项目"可以有任意多个，这不是漏洞。
func Canonicalize(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	for _, prefix := range []string{`\\?\`, `\\.\`} {
		p = strings.TrimPrefix(p, prefix)
	}
	lead := ""
	p = strings.ReplaceAll(p, "/", `\`)
	if strings.HasPrefix(p, `\\`) { // UNC：开头那一对要留着，否则 \\srv\share 会被折成 \srv\share
		lead = `\\`
		p = p[2:]
	}
	kept := make([]string, 0, 8)
	for _, seg := range strings.Split(p, `\`) {
		if seg != "" && seg != "." {
			kept = append(kept, seg)
		}
	}
	if len(kept) == 0 {
		return ""
	}
	joined := lead + strings.Join(kept, `\`)
	if lead == "" && len(joined) == 2 && joined[1] == ':' { // "D:" 这种根写法补一个分隔符，避免与 "D:x" 同键
		joined += `\`
	}
	return strings.ToLower(joined)
}
