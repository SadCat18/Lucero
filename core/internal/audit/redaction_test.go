package audit

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// testRedactor mirrors what db's InitRedaction must build from Rules().
type testRedactor struct {
	keys map[string]bool
	pats []*regexp.Regexp
}

var tr = func() *testRedactor {
	r := Rules()
	if err := r.Validate(); err != nil {
		panic(err)
	}
	t := &testRedactor{keys: map[string]bool{}}
	for _, k := range r.DenyKeys {
		t.keys[CanonicalKey(k)] = true
	}
	for _, p := range r.Patterns {
		t.pats = append(t.pats, regexp.MustCompile(p))
	}
	return t
}()

// redact is a TEST-ONLY mirror of the enforcement point. audit exports rules,
// never a redactor (internal contract §2), so the behaviour the rule set is
// supposed to produce is specified here. Once db.InitRedaction lands, the test
// role should point these cases at the real entry point and delete this.
func redact(v any) any { return redactTracking("payload_json", v, nil) }

func redactTracking(column string, v any, hits *[]RedactionHit) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			ck := CanonicalKey(k)
			if tr.keys[ck] {
				if s, isStr := val.(string); isStr && IsVaultRef(s) {
					out[k] = val // vault:// is the permitted form, not a leak
					continue
				}
				record(hits, RedactionHit{Column: column, Kind: HitKey, Match: ck, Count: 1})
				out[k] = Rules().Marker
				continue
			}
			out[k] = redactTracking(column, val, hits)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = redactTracking(column, val, hits)
		}
		return out
	case string:
		for _, re := range tr.pats {
			if !re.MatchString(t) {
				continue
			}
			record(hits, RedactionHit{Column: column, Kind: HitPattern, Match: re.String(), Count: 1})
			t = re.ReplaceAllString(t, Rules().Marker)
		}
		return t
	default:
		return v
	}
}

func record(hits *[]RedactionHit, h RedactionHit) {
	if hits != nil {
		*hits = append(*hits, h)
	}
}

func flatten(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func TestDenyKeysAndVaultRefs(t *testing.T) {
	var (
		gh  = "ghp_" + strings.Repeat("A1bC", 9)
		oai = "sk-" + strings.Repeat("xK9_", 8) + "abcdef"
	)
	for _, tc := range []struct {
		name        string
		in          map[string]any
		mustContain string // substring that must survive
		leaks       []string
	}{
		{
			name:  "deny key drops the whole value",
			in:    map[string]any{"token": gh},
			leaks: []string{gh},
		},
		{
			name:  "header-ish spellings hit via canonicalization",
			in:    map[string]any{"X-Api-Key": oai},
			leaks: []string{oai},
		},
		{
			name:  "uppercase deny key",
			in:    map[string]any{"AUTHORIZATION": "Bearer " + strings.Repeat("z", 40)},
			leaks: []string{strings.Repeat("z", 40)},
		},
		{
			name:  "nested maps and arrays",
			in:    map[string]any{"a": map[string]any{"b": []any{map[string]any{"password": "hunter2secretvalue"}}}},
			leaks: []string{"hunter2secretvalue"},
		},
		{
			name:        "vault reference under a deny key survives intact",
			in:          map[string]any{"token": "vault://github_token", "api_key": "vault://openai/prod"},
			mustContain: "vault://github_token",
			leaks:       []string{},
		},
		{
			name:  "vault reference cannot smuggle a secret",
			in:    map[string]any{"token": "vault://github_token sk-" + strings.Repeat("Q7rS", 8)},
			leaks: []string{"sk-" + strings.Repeat("Q7rS", 8)},
		},
		{
			name:        "vault reference inside free text survives",
			in:          map[string]any{"note": "rotate it, ref is vault://db_pass please"},
			mustContain: "vault://db_pass",
		},
		{
			name:  "secret embedded in free text is gone",
			in:    map[string]any{"error_message": "auth failed with key " + oai + " for user"},
			leaks: []string{oai},
		},
		{
			name:        "benign payload untouched",
			in:          map[string]any{"title": "Fix login bug", "count": 3, "ok": true},
			mustContain: "Fix login bug",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := flatten(t, redact(tc.in))
			for _, leak := range tc.leaks {
				if strings.Contains(got, leak) {
					t.Errorf("secret leaked into output: %s\nout = %s", leak, got)
				}
			}
			if tc.mustContain != "" && !strings.Contains(got, tc.mustContain) {
				t.Errorf("expected %q preserved, got %s", tc.mustContain, got)
			}
		})
	}
}

func TestPatternsCatchCommonSecretFormats(t *testing.T) {
	long := strings.Repeat("aB3d", 12)
	for _, tc := range []struct{ name, sample string }{
		{"github ptx token", "ghp_" + long},
		{"github user-to-server", "ghu_" + long},
		{"github refresh token", "ghr_" + long},
		{"github fine pat", "github_pat_" + long + "_" + long},
		{"openai style", "sk-" + long},
		{"openai project key", "sk-proj-" + long},
		{"anthropic style", "sk-ant-" + long},
		{"slack token", "xoxb-" + strings.Repeat("1a2b", 6)},
		{"aws access key id", "AKIA" + strings.Repeat("ABCD4EFG", 2)},
		{"jwt", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJsb2NhbC11c2VyIn0.eyS" + long},
		{"pem private key", "-----BEGIN RSA PRIVATE KEY-----\n" + long},
		{"bearer header", "Authorization: Bearer " + long},
		{"case-insensitive bearer", "bearer " + long},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := flatten(t, redact(map[string]any{"text": tc.sample}))
			if strings.Contains(out, tc.sample) {
				t.Errorf("pattern missed %s: %s", tc.name, out)
			}
		})
	}
}

func TestPatternsDoNotOverMatch(t *testing.T) {
	for _, benign := range []string{
		"task 42 finished with result_code ok",
		"risk_level", "filesystem.write", "PENDING",
		"the model was ski-lls and the sk- prefix never appeared", // "sk-" alone is prose, not a key
		"https://example.com/a?b=c",
		"/d/Work资源/Lucero_Project_Starter/file.md",
	} {
		if out := flatten(t, redact(map[string]any{"text": benign})); !strings.Contains(out, benign) {
			t.Errorf("false positive on %q -> %s", benign, out)
		}
	}
}

func TestRulesValidateIsFailClosed(t *testing.T) {
	if err := Rules().Validate(); err != nil {
		t.Fatalf("shipped rule set must validate: %v", err)
	}

	bad := Rules()
	bad.Patterns = append([]string{`([unclosed`}, bad.Patterns...)
	err := bad.Validate()
	if err == nil {
		t.Fatal("Validate accepted an uncompilable pattern; InitRedaction could not fail closed")
	}
	if !strings.Contains(err.Error(), "unclosed") {
		t.Errorf("error must name the offending pattern, got %v", err)
	}

	for name, mutate := range map[string]func(*RedactionRules){
		"empty marker":   func(r *RedactionRules) { r.Marker = "" },
		"empty denykeys": func(r *RedactionRules) { r.DenyKeys = nil },
	} {
		r := Rules()
		mutate(&r)
		if err := r.Validate(); err == nil {
			t.Errorf("%s: Validate accepted it, so a broken rule set could reach Ready", name)
		}
	}
}

func TestEveryDenyKeyIsLive(t *testing.T) {
	// Deliberately pattern-invisible: only a DenyKeys hit can redact this value.
	const value = "hunter2worth40chars0000000000000000000000"
	for _, key := range Rules().DenyKeys {
		for _, spelling := range []string{key, strings.ToUpper(key)} {
			out := flatten(t, redact(map[string]any{spelling: value}))
			if strings.Contains(out, value) {
				t.Errorf("deny key %q survived as %q: %s", key, spelling, out)
			}
			if !strings.Contains(out, DefaultMarker) {
				t.Errorf("deny key %q (%q) replaced with something other than the marker: %s", key, spelling, out)
			}
		}
	}
}

func TestSecretsNeverReachAuditDetails(t *testing.T) {
	long := strings.Repeat("aB3d", 12)
	secrets := []string{"sk-" + strings.Repeat("Zx9Q", 8), "ghp_" + long}
	payload := map[string]any{
		"schema": 1,
		"request": map[string]any{
			"api_key": secrets[0],
			"detail":  "the adapter used " + secrets[1] + " to reply",
		},
		"secret_ref": "vault://openai/prod",
	}
	var hits []RedactionHit
	out := flatten(t, redactTracking("payload_json", payload, &hits))
	for _, s := range secrets {
		if strings.Contains(out, s) {
			t.Errorf("stored payload still carries %s: %s", s, out)
		}
	}
	if !strings.Contains(out, "vault://openai/prod") {
		t.Errorf("vault reference was destroyed instead of preserved: %s", out)
	}

	details, err := RedactionDetails(hits)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range secrets {
		if strings.Contains(details, s) {
			t.Errorf("audit details leaked the value %s: %s", s, details)
		}
	}
	if !strings.Contains(details, `"kind":"key"`) || !strings.Contains(details, `"match":"apikey"`) ||
		!strings.Contains(details, `"count":1`) {
		t.Errorf("audit details must name the kind, the rule and the count, got %s", details)
	}
	if !strings.Contains(details, `"kind":"pattern"`) {
		t.Errorf("the pattern hit lost its kind tag, which is exactly what REQ-T3 exists to prevent: %s", details)
	}
	if len(hits) != 2 {
		t.Errorf("expected exactly 2 hits (one deny key, one pattern), got %d in %s", len(hits), details)
	}
}

// The exemption is prefix AND pattern-clean. Q6: "looks like a reference" and
// "hides no key" are different claims, and the compact vault://<key> spellings
// below can only be told apart by running the patterns. This is the same
// decision the enforcement point makes; keep the two in lockstep until REQ-A1
// puts a type on it.
func TestIsVaultRef(t *testing.T) {
	for v, want := range map[string]bool{
		"vault://github_token":             true,
		"vault://openai/prod":              true,
		"vault://db pass":                  true,  // no secret in it: junk, not a leak
		"vault://":                         true,  // likewise: an empty ref hides nothing
		"vault://sk-AAAAAAAAAAAAAAAA":      false, // a key wearing a reference
		"vault://ghp_A1bCA1bCA1bCA1bCA1bC": false,
		"vault://AKIAABCDEFGHIJKLMNOP":     false,
		"vault://x sk-AAAAAAAAAAAAAAAA":    false,
		"http://vault/x":                   false,
		"sk-vault://x":                     false,
	} {
		if got := IsVaultRef(v); got != want {
			t.Errorf("IsVaultRef(%q) = %v, want %v", v, got, want)
		}
	}
}

// A denied key holding a key-shaped "reference" must be redacted, not exempted.
func TestVaultRefDoesNotExcuseEmbeddedSecrets(t *testing.T) {
	for _, v := range []string{
		"vault://sk-Zx9QZx9QZx9QZx9QZx9Q",
		"vault://ghp_A1bCA1bCA1bCA1bCA1bCA1bCA1bCA1bC",
		"vault://AKIAABCDEFGHIJKLMNOP",
	} {
		out := flatten(t, redact(map[string]any{"token": v}))
		if strings.Contains(out, v) {
			t.Errorf("key-shaped reference survived redaction: %s", out)
		}
		if !strings.Contains(out, DefaultMarker) {
			t.Errorf("no marker written for %s: %s", v, out)
		}
	}
}

// grant_revoked's payload carries a free-text reason (T-13 ruling). Nothing about
// the field name "reason" says secret, so its protection can only come from the
// value patterns — this pins that, and pins that a clean reason is left alone.
func TestFreeTextReasonIsScrubbedByPatterns(t *testing.T) {
	for _, tc := range []struct{ name, reason, secret string }{
		{"github token", "revoked after " + "ghp_A1bCA1bCA1bCA1bCA1bCA1bCA1bCA1bC" + " was pasted", "ghp_A1bCA1bCA1bCA1bCA1bCA1bCA1bCA1bC"},
		{"provider key", "reason: " + "sk-Zx9QZx9QZx9QZx9QZx9QZx9Q", "sk-Zx9QZx9QZx9QZx9QZx9QZx9Q"},
		{"bearer", "the grant carried Bearer " + "abcdef0123456789abcdef0123456789" + " inline", "abcdef0123456789abcdef0123456789"},
	} {
		out := flatten(t, redact(map[string]any{"reason": tc.reason}))
		if strings.Contains(out, tc.secret) {
			t.Errorf("%s: secret survived in reason: %s", tc.name, out)
		}
		if !strings.Contains(out, DefaultMarker) {
			t.Errorf("%s: no marker written: %s", tc.name, out)
		}
		if !strings.Contains(out, `"reason"`) {
			t.Errorf("%s: the field name was rewritten away: %s", tc.name, out)
		}
	}
	for _, clean := range []string{
		"task ended", "user revoked from the panel", "expired by sweep",
	} {
		if out := flatten(t, redact(map[string]any{"reason": clean})); !strings.Contains(out, clean) {
			t.Errorf("clean reason rewritten: %s -> %s", clean, out)
		}
	}
}

func TestCanonicalKey(t *testing.T) {
	for in, want := range map[string]string{
		"Authorization": "authorization",
		"x-api-key":     "xapikey",
		"apiKey":        "apikey",
		"session_key":   "sessionkey",
		"Set-Cookie":    "setcookie",
		"risk_level":    "risklevel",
	} {
		if got := CanonicalKey(in); got != want {
			t.Errorf("CanonicalKey(%q) = %q, want %q", in, got, want)
		}
	}
}
