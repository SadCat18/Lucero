// Package quota 拥有额度账户与快照的领域类型 + 只读查询。
// "额度"不绑定软件名称：同一软件多账号、两家软件共享同一 Provider 额度、
// 一个账号多种窗口额度（领域模型「Quota Account」）。
// 写侧一行 SQL 都没有：落库等 internal/platform/db 可用后的 db.Create / db.Touch。
package quota

// Type 是 quota_snapshots.quota_type 的七个值。
type Type string

const (
	TypePercent    Type = "PERCENT"
	TypeCredits    Type = "CREDITS"
	TypeTokens     Type = "TOKENS"
	TypeRequests   Type = "REQUESTS"
	TypeCurrency   Type = "CURRENCY"
	TypeTimeWindow Type = "TIME_WINDOW"
	TypeCustom     Type = "CUSTOM"
)

func (t Type) Valid() bool {
	switch t {
	case TypePercent, TypeCredits, TypeTokens, TypeRequests, TypeCurrency, TypeTimeWindow, TypeCustom:
		return true
	}
	return false
}

// ObservationKind 必须区分"观测到的"和"估的"：预测不能写进 OBSERVED 快照（§31）。
type ObservationKind string

const (
	Observed  ObservationKind = "OBSERVED"
	Estimated ObservationKind = "ESTIMATED"
)

func (k ObservationKind) Valid() bool {
	return k == Observed || k == Estimated
}

// Confidence 是「状态置信度」四级词表；UNKNOWN 是合法值。
type Confidence string

const (
	ConfidenceHigh    Confidence = "HIGH"
	ConfidenceMedium  Confidence = "MEDIUM"
	ConfidenceLow     Confidence = "LOW"
	ConfidenceUnknown Confidence = "UNKNOWN"
)

func (c Confidence) Valid() bool {
	switch c {
	case ConfidenceHigh, ConfidenceMedium, ConfidenceLow, ConfidenceUnknown:
		return true
	}
	return false
}

type Account struct {
	ID                 string
	AdapterInstanceID  string
	ExternalAccountRef string
	DisplayLabel       string
	QuotaSource        string
	CreatedAt          int64
	UpdatedAt          int64
	Version            int64 // ADR-017
}

// Snapshot 是一条额度读数。可空数值一律用指针：NULL 意为"没读到"，
// 折算成 0 就是 §12 红线「无法确认状态时却显示确定的 0 quota」。
type Snapshot struct {
	ID                  string
	QuotaAccountID      string
	Type                Type
	Key                 string
	RawValueJSON        string
	UsedValue           *float64
	RemainingValue      *float64
	PercentageUsed      *float64
	PercentageRemaining *float64
	WindowStart         *int64
	WindowEnd           *int64
	ResetAt             *int64
	ObservationKind     ObservationKind
	Source              string
	Confidence          Confidence
	ObservedAt          int64
}
