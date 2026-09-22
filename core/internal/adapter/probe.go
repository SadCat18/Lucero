package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// probe.go is the registration half of this package: what contract ③ §7.2 (REQ-AD-1)
// calls the entry point of `POST /api/v1/adapters/{key}/probe`. The package used to be
// protocol-only ("Adapter 不写 DB、不发事件", contract ② §6 规则 4); §7.2 makes it
// protocol + registration domain, and the write stays inside this package through
// platform/db (probe_store_sqlite.go), exactly like internal/approval. Nothing in this
// file touches a database, so the protocol guarantee keeps holding file by file: see
// TestOnlyStdlibImports, which was scoped to that rule rather than deleted.

// AdapterInstanceView is yaml's AdapterInstance, in the field names core-v1.yaml uses.
// §7.2's reason for existing: "handler 里不许补字段". If a required field cannot come out of
// a probe, the probe is what has to change - otherwise the HTTP layer would be inventing
// identity, and a projection that half-fills display_name or health is how an Adapter
// starts looking registered when it never was.
type AdapterInstanceView struct {
	ID                string         `json:"id"`
	AdapterKey        string         `json:"adapter_key"`
	DisplayName       string         `json:"display_name"`
	AdapterVersion    *string        `json:"adapter_version"`
	ProtocolVersion   *string        `json:"protocol_version"`
	SoftwareVersion   *string        `json:"software_version"`
	Enabled           bool           `json:"enabled"`
	Health            string         `json:"health"`
	EffectiveBackend  *string        `json:"effective_backend"`
	LastHeartbeatAt   *int64         `json:"last_heartbeat_at"`
	LastHealthcheckAt *int64         `json:"last_healthcheck_at"`
	Metadata          map[string]any `json:"metadata"`
	Version           int64          `json:"version"`
	CreatedAt         int64          `json:"created_at"`
	UpdatedAt         int64          `json:"updated_at"`
}

// AdapterInstance projects a probe result. VerifiedAt stands in for both created_at and
// updated_at on the rows this call just wrote, and Enabled is true because a registered
// Adapter that probe was called on is enabled by definition - probe is not a way to
// discover disabled rows, it is the thing that makes a row trustworthy.
func (p *ProbeResult) AdapterInstance() AdapterInstanceView {
	return AdapterInstanceView{
		ID: p.AdapterInstanceID, AdapterKey: p.AdapterKey, DisplayName: p.DisplayName,
		AdapterVersion: optString(p.AdapterVersion), ProtocolVersion: optString(p.ProtocolVersion),
		SoftwareVersion: optString(p.SoftwareVersion), Enabled: true,
		Health: p.HealthState, EffectiveBackend: optString(p.EffectiveBackend),
		LastHealthcheckAt: &p.VerifiedAt,
		Metadata:          map[string]any{"schema": 1},
		CreatedAt:         p.VerifiedAt, UpdatedAt: p.VerifiedAt,
	}
}

func optString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Launcher is the one thing Probe needs from the process layer, and it is deliberately
// not `internal/process`: §7.2's REQ-T12 note measured that nothing constructs a
// Supervisor yet, so importing that package would queue probe behind another role's
// work and add a cross-role dependency for no reason. cmd/core hands over something that
// starts the child for this key and gives back one duplex pipe; who owns deadlines,
// restarts and backoff stays on that side, since §7.2 says probe must not invent a second
// health/retry model.
//
// Returning io.ReadWriteCloser rather than two pipes keeps one ownership question: whoever
// produced it closes it.
type Launcher interface {
	Start(ctx context.Context, adapterKey string) (io.ReadWriteCloser, error)
}

// Entry is one registered Adapter: how to start it, and what to call it.
//
// DisplayName is here because adapter_instances.display_name is NOT NULL and yaml's
// AdapterInstance lists it as required. It cannot come from the handshake - §2's
// InitializeResult has no such field - so a Registry that left it out would force the
// HTTP handler to invent one, which §7.2 forbids ("handler 里不许补字段").
type Entry struct {
	AdapterKey  string
	DisplayName string
	Launcher    Launcher
}

// ProbeResult is what a probe learned, in the shape the store persists and the handler
// projects. VerifiedAt and HealthState are always set - including on the failure path,
// where HealthState is the only fact probe is allowed to record (§7.2: 失败 ⇒ 只记
// health_state 变化并回 §15.2 对应码，不伪造 capability 行).
type ProbeResult struct {
	AdapterInstanceID string       `json:"adapter_instance_id"`
	AdapterKey        string       `json:"adapter_key"`
	DisplayName       string       `json:"display_name"`
	SoftwareVersion   string       `json:"software_version"`
	AdapterVersion    string       `json:"adapter_version"`
	ProtocolVersion   string       `json:"protocol_version"`
	Capabilities      []Capability `json:"capabilities"`
	VerifiedAt        int64        `json:"verified_at"`
	HealthState       string       `json:"health_state"`
	EffectiveBackend  string       `json:"effective_backend"`
}

// ProbeStore is the persistence this package's registration needs. probe_store_sqlite.go
// implements it over platform/db; keeping it an interface is what lets Probe be tested
// against a real handshake without anyone handing this package a DB handle.
type ProbeStore interface {
	// RecordProbe writes the instance row and its capability rows, and reports the
	// instance id it wrote them under. It is one transaction: a probe that half-landed
	// would leave an Adapter "registered" with someone else's capabilities.
	RecordProbe(ctx context.Context, res ProbeResult) (string, error)
	// MarkUnverified is the failure path: health only, no capability rows, and the
	// instance row is created if this key was never seen.
	MarkUnverified(ctx context.Context, key, displayName, reason string) (string, error)
}

// readResult carries one frame plus its read error across the goroutine that blocks on
// the pipe, so a slow or silent Adapter cannot pin the probe's stack.
type readResult struct {
	f   *Frame
	err error
}

// versionString is §2.2's "M.m" spelling of a Version.
func versionString(v Version) string { return fmt.Sprintf("%d.%d", v.Major, v.Minor) }

// Registry is the Core-side registration entry point. One attempt per call, no retry.
type Registry struct {
	protocol Version
	entries  map[string]Entry
	store    ProbeStore
	now      func() int64
	// timeout bounds one handshake attempt. It is a field, not a constant call, so a
	// test can prove "one attempt, no retry" without waiting on a real deadline.
	timeout func() (seconds int)
}

// NewRegistry builds a Registry over a set of entries. coreVersion is what this Core
// speaks; §2.2's rejection of a different major happens inside NewHandshake, not here.
func NewRegistry(coreVersion Version, entries []Entry, store ProbeStore, now func() int64) (*Registry, error) {
	if store == nil {
		return nil, &Error{Code: ErrInvalidParams, Message: "registry needs a store: probe that answers but records nothing is the failure §12 warns about"}
	}
	if now == nil {
		return nil, &Error{Code: ErrInvalidParams, Message: "registry requires a clock"}
	}
	r := &Registry{protocol: coreVersion, entries: map[string]Entry{}, store: store, now: now}
	for _, e := range entries {
		if e.AdapterKey == "" || e.Launcher == nil {
			return nil, &Error{Code: ErrInvalidParams,
				Message: fmt.Sprintf("registry entry %q is missing adapter_key or a launcher", e.AdapterKey)}
		}
		if _, dup := r.entries[e.AdapterKey]; dup {
			return nil, &Error{Code: ErrInvalidParams,
				Message: fmt.Sprintf("registry has two entries for %q; one key is one Adapter", e.AdapterKey)}
		}
		r.entries[e.AdapterKey] = e
	}
	return r, nil
}

// Probe starts the Adapter for this key once, drives one §2 handshake over the pipes it
// gets back, and records what it saw. It never retries: §7.2 pins v0.1 at one attempt so
// a probe cannot quietly turn into an auto-recovery loop with its own backoff story.
func (r *Registry) Probe(ctx context.Context, adapterKey string) (*ProbeResult, error) {
	entry, ok := r.entries[adapterKey]
	if !ok {
		// A key nobody registered is a bad request, not an Adapter that is down: the
		// difference is what §15.2 maps to 400 versus 503/502.
		return nil, &Error{Code: ErrInvalidParams,
			Message: fmt.Sprintf("probe: %q is not a registered adapter_key (unknown key ≠ unreachable one)", adapterKey)}
	}
	pipe, err := entry.Launcher.Start(ctx, adapterKey)
	if err != nil {
		return r.failed(ctx, entry, "launch: "+err.Error())
	}
	defer pipe.Close()

	res, err := r.exchange(ctx, pipe, adapterKey)
	if err != nil {
		// One row either way: failed() records the health change, so a mismatch must not
		// write first here and again there.
		var mismatch *Error
		if errors.As(err, &mismatch) && mismatch.Code == ErrProtocolVersionMismatch {
			// §2.2 refuses a different major without guessing, and §15.2 maps that to 426,
			// not to the 503 an unreachable Adapter gets.
			return r.failedWith(ctx, entry, ErrProtocolVersionMismatch, err.Error())
		}
		return r.failed(ctx, entry, err.Error())
	}
	hs, err := NewHandshake(r.protocol, *res)
	if err != nil {
		return r.failed(ctx, entry, "handshake: "+err.Error())
	}
	caps := hs.EffectiveCapabilities()
	backend := ""
	for _, c := range caps {
		if c.EffectiveBackend != "" {
			backend = c.EffectiveBackend
			break
		}
	}
	out := ProbeResult{
		AdapterKey: adapterKey, DisplayName: entry.DisplayName,
		SoftwareVersion: softwareVersionOf(res.SoftwareVersions, adapterKey),
		AdapterVersion:  res.AdapterVersion,
		ProtocolVersion: fmt.Sprintf("%d.%d", hs.Version().Major, hs.Version().Minor),
		Capabilities:    caps, VerifiedAt: r.now(), HealthState: res.Health,
		EffectiveBackend: backend,
	}
	if out.HealthState == "" {
		// §3's health is a required field of the initialize result; an empty one is not
		// UNKNOWN-by-evidence, it is a broken reply, and recording it as HEALTHY would be
		// the §12 mistake in the other direction.
		return r.failed(ctx, entry, "initialize result carried no health")
	}
	id, err := r.store.RecordProbe(ctx, out)
	if err != nil {
		return nil, fmt.Errorf("probe %s: recorded but the write failed: %w", adapterKey, err)
	}
	out.AdapterInstanceID = id
	return &out, nil
}

// failed is the only failure shape: one health row, no capability rows, and a code from
// §15.1's existing vocabulary rather than an invented one.
func (r *Registry) failed(ctx context.Context, entry Entry, reason string) (*ProbeResult, error) {
	return r.failedWith(ctx, entry, ErrAdapterUnresponsive, reason)
}

// failedWith is the only failure shape: one health row, no capability rows, and a code
// from §15.1's existing vocabulary rather than an invented one - which code it is matters,
// because §15.2 turns it into the HTTP status the caller sees.
func (r *Registry) failedWith(ctx context.Context, entry Entry, code ErrorCode, reason string) (*ProbeResult, error) {
	id, err := r.store.MarkUnverified(ctx, entry.AdapterKey, entry.DisplayName, reason)
	if err != nil {
		return nil, fmt.Errorf("probe %s: %s; and the health row failed too: %w", entry.AdapterKey, reason, err)
	}
	return nil, &Error{Code: code,
		Message: fmt.Sprintf("probe %s failed once and was recorded as UNAVAILABLE (instance %s): %s",
			entry.AdapterKey, id, reason)}
}

// exchange drives §2's adapter/initialize over a live pipe pair: one request, one reply,
// nothing else. It returns the decoded InitializeResult so the negotiation and the
// capability resolution run through the same NewHandshake the rest of Core uses.
func (r *Registry) exchange(ctx context.Context, pipe io.ReadWriteCloser, adapterKey string) (*InitializeResult, error) {
	in, out := NewReader(pipe), NewWriter(pipe)
	id := NewRequestID().String()
	params, err := json.Marshal(InitializeParams{
		Protocol: r.protocol, CoreVersion: versionString(r.protocol),
	})
	if err != nil {
		return nil, fmt.Errorf("probe %s: initialize params: %w", adapterKey, err)
	}
	req := &Frame{JSONRPC: JSONRPCVersion, ID: id, Method: MethodInitialize, Params: params, RequestID: id}
	if err := out.Write(req); err != nil {
		return nil, fmt.Errorf("probe %s: write initialize: %w", adapterKey, err)
	}
	done := make(chan readResult, 1)
	go func() {
		f, err := in.Read()
		done <- readResult{f, err}
	}()
	select {
	case <-ctx.Done():
		// Closing the pipe is how the read goroutine is released; leaving it blocked
		// would leak one goroutine per timed-out probe. probe owns no deadline of its own
		// because §7.2 pins the timeout口径 to whoever started the process.
		_ = pipe.Close()
		return nil, &Error{Code: ErrAdapterUnresponsive,
			Message: fmt.Sprintf("probe %s: no initialize reply before the caller's deadline: %v",
				adapterKey, ctx.Err())}
	case got := <-done:
		if got.err != nil {
			return nil, fmt.Errorf("probe %s: read initialize reply: %w", adapterKey, got.err)
		}
		f := got.f
		if f.Kind() != KindResponse || f.ID != id {
			return nil, &Error{Code: ErrAdapterUnresponsive,
				Message: fmt.Sprintf("probe %s: got a %s frame instead of the initialize reply", adapterKey, f.Kind())}
		}
		if f.Error != nil {
			return nil, &Error{Code: f.Error.Code,
				Message: fmt.Sprintf("probe %s: Adapter refused initialize: %s", adapterKey, strings.TrimSpace(f.Error.Message))}
		}
		var res InitializeResult
		if err := json.Unmarshal(f.Result, &res); err != nil {
			return nil, &Error{Code: ErrAdapterUnresponsive,
				Message: fmt.Sprintf("probe %s: initialize reply is not decodable: %v", adapterKey, err)}
		}
		// No Validate call here on purpose: NewHandshake is the one place §2's result is
		// checked, and a second check in probe would be a second opinion that can
		// disagree with the first.
		return &res, nil
	}
}

// softwareVersionOf picks this Adapter's own line out of §2.1's software_versions map.
// The map is keyed by component, so the fallback chain is deliberate: the key itself,
// then "core" - and never the first entry it happens to iterate, because map order in Go
// is random and a probe that reports a different product every run is worse than one that
// reports nothing.
func softwareVersionOf(versions map[string]string, adapterKey string) string {
	if v, ok := versions[adapterKey]; ok {
		return v
	}
	return versions["core"]
}
