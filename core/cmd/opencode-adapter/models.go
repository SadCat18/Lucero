package main

// models.go answers model/list from the one channel §16.5 leaves open for this product:
// the offline catalogue file (profile §④: D:\opencode\cache\models.json, 4,690,520 B,
// 221 provider keys). The live endpoints that could confirm it - /provider,
// /config/providers - were measured 401, so a successful reply here is a **cache**
// answer and must say so: source=cache_file with confidence MEDIUM, because a cache can
// be stale and pretending otherwise is how "读不到就不显示" gets dressed up as a list.
//
// Nothing in this file opens a network connection, and nothing in the package does:
// §16.7 第 1 条's range is observation of local data only.

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/SadCat18/lucero/core/internal/adapter"
)

type modelRow struct {
	ModelKey string `json:"model_key"`
	Label    string `json:"label"`
	// Parameters stays raw JSON: §4's model/list carries a parameter schema, and
	// interpreting a provider's schema here would put product semantics in a protocol
	// boundary (the same reason fake-adapter keeps it raw).
	Parameters json.RawMessage `json:"parameters"`
	// ProviderRaw carries the catalogue entry plus the two provenance fields §16.5 asks
	// for. It is the only slot §4 leaves for adapter-side facts, so provenance goes in
	// there rather than becoming a new top-level field nobody agreed to.
	ProviderRaw json.RawMessage `json:"provider_raw"`
}

type modelListResult struct {
	Models []modelRow `json:"models"`
}

// modelList reads the cache file. A missing or unrecognised file is an error frame with
// LOCAL_DATA_UNAVAILABLE, not an empty list: §4 has no way to tell "this product offers
// no models" from "we could not read the offer", and an empty answer would be the
// fabricated-normal case §16.5's last sentence forbids.
func (o *opencode) modelList(req *adapter.Frame) error {
	raw, err := os.ReadFile(o.cfg.modelsPath)
	if err != nil {
		return o.reply(errReply(req, &adapter.Error{Code: codeLocalDataUnavailable,
			Message: "model/list: cannot read the catalogue cache " + o.cfg.modelsPath + ": " + err.Error()}))
	}
	catalogue, err := parseCatalogue(raw)
	if err != nil {
		return o.reply(errReply(req, &adapter.Error{Code: codeLocalDataUnavailable,
			Message: "model/list: " + err.Error()}))
	}
	if len(catalogue) == 0 {
		return o.reply(errReply(req, &adapter.Error{Code: codeLocalDataUnavailable,
			Message: fmt.Sprintf("model/list: cache file %s parsed to 0 models; refusing to publish an empty offer as a list",
				o.cfg.modelsPath)}))
	}
	fmt.Fprintf(o.logs, "model/list: %d models from cache %s (%d bytes)\n", len(catalogue), o.cfg.modelsPath, len(raw))
	return o.reply(result(req, modelListResult{Models: catalogue}))
}

// catalogueEntry is the shape the profile recorded: a top-level object keyed by provider
// id, each carrying a models object whose values hold id/reasoning/cost/limit metadata.
// Only the fields §4's row needs are read; the whole entry travels onward in
// provider_raw so a later reader can re-verify without us interpreting it.
type catalogueEntry struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Models map[string]struct {
		ID      string          `json:"id"`
		Name    string          `json:"name"`
		Limit   json.RawMessage `json:"limit"`
		Cost    json.RawMessage `json:"cost"`
		Release json.RawMessage `json:"release_date"`
	} `json:"models"`
}

func parseCatalogue(raw []byte) ([]modelRow, error) {
	var top map[string]catalogueEntry
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, fmt.Errorf("catalogue cache is not the recorded {providerId:{models:{}}} object: %s",
			strings.ReplaceAll(err.Error(), "\n", " "))
	}
	providers := make([]string, 0, len(top))
	for p := range top {
		providers = append(providers, p)
	}
	sort.Strings(providers)
	var out []modelRow
	for _, p := range providers {
		e := top[p]
		keys := make([]string, 0, len(e.Models))
		for k := range e.Models {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			m := e.Models[k]
			provider, err := json.Marshal(map[string]any{
				"source":         "cache_file",
				"confidence":     adapter.ConfidenceMedium,
				"provider_id":    p,
				"native_id":      coalesce(m.ID, k),
				"limit":          rawOr(m.Limit, "null"),
				"cost":           rawOr(m.Cost, "null"),
				"release_date":   rawOr(m.Release, "null"),
				"tier":           "READ_ONLY",
				"live_endpoints": "unverified (401 measured, profile §④)",
			})
			if err != nil {
				return nil, fmt.Errorf("marshal provider_raw for %s/%s: %w", p, k, err)
			}
			out = append(out, modelRow{
				ModelKey:    p + "/" + k,
				Label:       coalesce(m.Name, e.Name+"/"+k),
				Parameters:  parametersFromLimit(m.Limit),
				ProviderRaw: provider,
			})
		}
	}
	return out, nil
}

// parametersFromLimit turns the catalogue's context/reasoning limits into a small JSON
// schema stub rather than a guess at the provider's real parameter surface: §4 asks for
// parameter_schema_json, and inventing one would be a claim about what a caller may send.
// An absent limit yields an empty object, which reads as "unknown" rather than "no
// limits".
func parametersFromLimit(limit json.RawMessage) json.RawMessage {
	if len(limit) == 0 {
		return json.RawMessage(`{"type":"object","properties":{},"x-source":"limit-absent"}`)
	}
	return json.RawMessage(`{"type":"object","properties":{},"x-catalogue-limit":` + string(limit) + `}`)
}

func rawOr(v json.RawMessage, fallback string) json.RawMessage {
	if len(v) == 0 {
		return json.RawMessage(fallback)
	}
	return v
}

func coalesce(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
