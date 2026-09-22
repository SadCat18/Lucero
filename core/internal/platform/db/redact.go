package db

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync/atomic"
)

// Redaction is mandatory: there is no skip flag, and a write before
// InitRedaction fails closed rather than storing a possible secret.
type redactor struct {
	denyKeys map[string]bool
	patterns []*regexp.Regexp
	marker   string
}

var red atomic.Pointer[redactor]

// vaultRefPrefix marks the only on-disk form a secret may take (ADR-012), so a
// reference must survive redaction even under a denied key. Mirrors
// audit.VaultRefPrefix; db cannot import audit, so the two are kept identical by
// test rather than by type (契约 §0).
const vaultRefPrefix = "vault://"

// canonicalKey matches audit.CanonicalKey: lowercase, then drop '-', '_' and
// '.'. Without this the rule set's "apikey" would never hit a payload's
// "api_key".
func canonicalKey(k string) string {
	b := strings.Builder{}
	for _, r := range strings.ToLower(k) {
		if r == '-' || r == '_' || r == '.' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// isCleanVaultRef keeps ADR-012's permitted reference form alive under a denied
// key, but only when nothing in it trips a secret pattern. "vault://x sk-<key>"
// is a key wearing a reference: dropping the reference is a discoverable bug,
// leaking the key is the §12 P0, so only an exact-clean ref survives.
func (r *redactor) isCleanVaultRef(s string) bool {
	return strings.HasPrefix(s, vaultRefPrefix) && s == r.scrubString(s, newHits(), "")
}

// InitRedaction compiles the rule set. Rules arrive as plain strings so that
// neither this package nor audit has to import the other (契约 §0); cmd/core
// passes audit.Rules() fields straight through.
func InitRedaction(denyKeys, patterns []string, marker string) error {
	if marker == "" {
		marker = "[REDACTED]"
	}
	r := &redactor{denyKeys: map[string]bool{}, marker: marker}
	for _, k := range denyKeys {
		if k = canonicalKey(k); k != "" {
			r.denyKeys[k] = true
		}
	}
	if len(r.denyKeys) == 0 {
		return fmt.Errorf("redaction deny keys are empty")
	}
	for _, p := range patterns {
		if strings.TrimSpace(p) == "" {
			continue
		}
		re, err := regexp.Compile(p)
		if err != nil {
			return fmt.Errorf("redaction pattern %q: %w", p, err)
		}
		r.patterns = append(r.patterns, re)
	}
	red.Store(r)
	return nil
}

// hits records which column was scrubbed and by which rule. Only names and
// counts, never the original value (ADR-012).
type hits struct {
	m   map[hitsKey]*RedactionHit
	ord []hitsKey
}

type hitsKey struct {
	column string
	kind   HitKind
	match  string
}

// HitKind says which half of the rule set fired (REQ-T3). It travels as its own
// field: when it was encoded as a "key:" prefix inside Match, a forgotten
// prefix silently re-labelled a deny-key hit as a regex hit, and that string is
// covered by audit's entry_hash, so the mis-attribution could not be fixed
// later without breaking the chain.
type HitKind string

const (
	HitKey     HitKind = "key"
	HitPattern HitKind = "pattern"
)

// RedactionHit is one (column, rule) pair scrubbed during a transaction.
// For HitKey, Match is the rule name in audit's canonical form ("apikey"), not
// the payload's spelling, so "API_KEY" and "Authorization" attribute to one rule
// and audit's stored value is unchanged from before this field existed.
type RedactionHit struct {
	Column string  `json:"column"`
	Kind   HitKind `json:"kind"`
	Match  string  `json:"match"`
	Count  int     `json:"count"`
}

func newHits() *hits { return &hits{m: map[hitsKey]*RedactionHit{}} }

func (h *hits) add(column string, kind HitKind, match string) {
	k := hitsKey{column, kind, match}
	if e, ok := h.m[k]; ok {
		e.Count++
		return
	}
	h.m[k] = &RedactionHit{Column: column, Kind: kind, Match: match, Count: 1}
	h.ord = append(h.ord, k)
}

func (h *hits) all() []RedactionHit {
	out := make([]RedactionHit, 0, len(h.ord))
	for _, k := range h.ord {
		out = append(out, *h.m[k])
	}
	return out
}

// scrubString applies the value patterns; ponytail: pattern-only for free text,
// entropy detection if false negatives bite.
func (r *redactor) scrubString(s string, h *hits, column string) string {
	for _, re := range r.patterns {
		m := re.FindAllStringIndex(s, -1)
		if len(m) == 0 {
			continue
		}
		for range m {
			h.add(column, HitPattern, re.String())
		}
		s = re.ReplaceAllString(s, r.marker)
	}
	return s
}

// walk redacts a decoded JSON value in place, replacing any value under a
// denied key wholesale.
func (r *redactor) walk(v any, h *hits, column, path string) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			at := path + "." + k
			if r.denyKeys[canonicalKey(k)] {
				if s, ok := val.(string); ok && r.isCleanVaultRef(s) {
					continue
				}
				t[k] = r.marker
				h.add(column, HitKey, canonicalKey(k))
				continue
			}
			t[k] = r.walk(val, h, column, at)
		}
		return t
	case []any:
		for i, val := range t {
			t[i] = r.walk(val, h, column, path+"["+fmt.Sprint(i)+"]")
		}
		return t
	case string:
		return r.scrubString(t, h, column)
	default:
		return v
	}
}

// redactValue scrubs and serializes a value destined for a TEXT column. Anything
// that is not text-like (int, time, []byte) passes through untouched.
// Scrub is the exported entry to the one and only redaction implementation
// (契约 §2.2.1 item 1). audit needs both the final bytes and the attribution
// before it seals entry_hash, and cmd/core injects this function value into
// Chain.Scrub, so the bytes that go into the hash are the bytes that go into
// the row.
//
// Rules still belong to audit (§2.1); execution is still only this code. Do not
// write a second redactor.
//
// Scrubbing is byte-stable on its own output (see TestScrubIsByteStable), so
// Tx.Append re-scrubbing the same values later changes no byte — that re-scrub
// stays as defence in depth, not as a correction pass.
func Scrub(table string, cols []string, args []any) ([]any, []RedactionHit, error) {
	h := newHits()
	out, err := scrubArgsInto(h, table, cols, args)
	if err != nil {
		return nil, nil, err
	}
	return out, h.all(), nil
}

func scrubArgsInto(h *hits, table string, cols []string, args []any) ([]any, error) {
	if len(cols) != len(args) {
		return nil, fmt.Errorf("scrub %s: %d columns but %d args", table, len(cols), len(args))
	}
	out := make([]any, len(args))
	for i, v := range args {
		s, err := redactValue(table+"."+cols[i], v, h)
		if err != nil {
			return nil, err
		}
		out[i] = s
	}
	return out, nil
}

func redactValue(column string, v any, h *hits) (any, error) {
	r := red.Load()
	if r == nil {
		return nil, fmt.Errorf("column %s: redaction is not initialized (call db.InitRedaction before any write)", column)
	}
	switch t := v.(type) {
	case map[string]any, []any, json.RawMessage:
		b, err := json.Marshal(t)
		if err != nil {
			return nil, fmt.Errorf("column %s: %w", column, err)
		}
		return r.encode(b, column, h)
	case string:
		s := strings.TrimLeft(t, " \t\r\n")
		if !strings.HasPrefix(s, "{") && !strings.HasPrefix(s, "[") {
			return r.scrubString(t, h, column), nil
		}
		return r.encode([]byte(t), column, h)
	default:
		return v, nil
	}
}

func (r *redactor) encode(b []byte, column string, h *hits) (string, error) {
	var decoded any
	if err := json.Unmarshal(b, &decoded); err != nil {
		return "", fmt.Errorf("column %s is not valid JSON: %w", column, err)
	}
	scrubbed := r.walk(decoded, h, column, "$")
	out, err := json.Marshal(scrubbed)
	if err != nil {
		return "", fmt.Errorf("column %s: re-encode after redaction: %w", column, err)
	}
	return string(out), nil
}
