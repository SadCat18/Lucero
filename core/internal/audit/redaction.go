// Package audit owns the sensitive-data rule set and the tamper-evident audit chain.
//
// It exports rules only. The enforcement point (recursive payload traversal and
// replacement) is the db platform layer, per internal contract §2. This package
// must never import internal/platform/db, and db must never import this package;
// cmd/core wires them together.
package audit

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// DefaultMarker replaces every redacted value.
const DefaultMarker = "[REDACTED]"

// VaultRefPrefix marks the only on-disk form a secret may take.
const VaultRefPrefix = "vault://"

// RedactionRules is the whole audit->db contract surface for sensitive data.
type RedactionRules struct {
	// DenyKeys are matched against JSON key names after canonicalization:
	// lowercase, then drop '-', '_' and '.'. So "Authorization", "x-api-key"
	// and "apiKey" all hit "apikey". A hit replaces the entire value.
	DenyKeys []string
	// Patterns are RE2 source strings applied to string values, which catches
	// secrets inside free text that no key name can describe.
	Patterns []string
	Marker   string
}

// Rules is the single exported rule set. Patterns are constants, so a compile
// failure here is a programming error caught by TestRulesValidate, not runtime.
func Rules() RedactionRules {
	return RedactionRules{
		// Key names are canonical form (see DenyKeys).
		DenyKeys: []string{
			"token", "accesstoken", "refreshtoken", "idtoken", "bearertoken",
			"apikey", "xapikey", "authorization", "auth", "cookie", "setcookie",
			"password", "passwd", "secret", "secretkey", "clientsecret",
			"sessionkey", "sessiontoken", "privatekey", "accesskey", "secretaccesskey",
			"credential", "credentials", "connectionstring", "dsn",
		},
		Patterns: []string{
			`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`,
			`gh[pousr]_[A-Za-z0-9]{20,}`,
			`github_pat_[A-Za-z0-9_]{20,}`,
			`sk-[A-Za-z0-9_-]{16,}`,
			`xox[baprs]-[A-Za-z0-9-]{10,}`,
			`AKIA[0-9A-Z]{16}`,
			`eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`,
			`(?i)bearer\s+[A-Za-z0-9._~+/=-]{16,}`,
		},
		Marker: DefaultMarker,
	}
}

// IsVaultRef reports whether a value may survive redaction as a reference: it
// must be prefixed with vault:// AND carry nothing a secret pattern would catch.
//
// The second half is the whole point. "是 vault 引用" and "值里没藏密钥" are not
// the same statement: vault://sk-<20 chars> is a key wearing a reference form,
// and honouring the prefix alone was a documented cause of the Secret-leak P0
// (Q6). Losing a reference is a discoverable bug; leaking a key is not, so this
// errs toward redacting, and an uncompilable pattern exempts nothing.
//
// The enforcement point in internal/platform/db implements the same decision on
// its own copy of the rules until REQ-A1 puts a type on that; keep them moving
// together.
func IsVaultRef(v string) bool {
	if !strings.HasPrefix(v, VaultRefPrefix) {
		return false
	}
	for _, re := range patterns() {
		if re.MatchString(v) {
			return false
		}
	}
	return true
}

// patterns compiles Rules().Patterns once. nil means a rule is broken, which
// IsVaultRef reads as "exempt nothing".
var patterns = sync.OnceValue(func() []*regexp.Regexp {
	var out []*regexp.Regexp
	for _, p := range Rules().Patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil
		}
		out = append(out, re)
	}
	return out
})

// CanonicalKey normalizes a JSON key name for DenyKeys lookup. Exported so the
// enforcement point matches the rule set's documented semantics instead of
// inventing its own.
func CanonicalKey(k string) string {
	b := strings.Builder{}
	for _, r := range strings.ToLower(k) {
		if r == '-' || r == '_' || r == '.' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// HitKind says which half of the rule set fired. REQ-T3: it travels as a field,
// never as a prefix on the matched name, because this string is covered by
// entry_hash — a lost prefix would silently re-attribute a deny-key hit as a
// regex hit, and correcting it later would break the chain.
type HitKind string

const (
	HitKey     HitKind = "key"
	HitPattern HitKind = "pattern"
)

// RedactionHit is what Audit records about a redaction: where it happened,
// which rule fired, how many times. It carries no original value and no length,
// because "this field held a 40-char GitHub token" is itself a leak.
type RedactionHit struct {
	Column string  `json:"column"`
	Kind   HitKind `json:"kind"`
	Match  string  `json:"match"`
	Count  int     `json:"count"`
}

// RedactionDetails renders audit_entries.details_json for redaction hits. The
// db enforcement point calls this after applying Rules(); audit owns the shape
// so the "counts and names only" rule is not reinvented per writer.
func RedactionDetails(hits []RedactionHit) (string, error) {
	type payload struct {
		Schema     int            `json:"schema"`
		Redactions []RedactionHit `json:"redactions"`
	}
	b, err := json.Marshal(payload{Schema: 1, Redactions: hits})
	if err != nil {
		return "", fmt.Errorf("audit: encode redaction details: %w", err)
	}
	return string(b), nil
}

// Validate compiles every pattern and reports the first failure. db's
// InitRedaction calls it before Core reaches Ready: an uncompilable pattern
// means the rule silently never fires, so it is fail-closed, not "skip it".
func (r RedactionRules) Validate() error {
	if r.Marker == "" {
		return fmt.Errorf("audit: redaction Marker is empty")
	}
	if len(r.DenyKeys) == 0 {
		return fmt.Errorf("audit: redaction DenyKeys is empty")
	}
	for i, p := range r.Patterns {
		if _, err := regexp.Compile(p); err != nil {
			return fmt.Errorf("audit: redaction pattern[%d] %q: %w", i, p, err)
		}
	}
	return nil
}
