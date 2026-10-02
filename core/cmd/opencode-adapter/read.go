package main

// read.go answers §16.7 第 1 条's two read methods out of the provider's own journal,
// and it is where the ruling "delta 是 Adapter 合成的" actually lands.
//
// Three facts from the profile decide the shape, and none of them is ours to change:
//
//   - the persisted journal holds **whole snapshots**, never increments: a `text` part
//     produced at most 2 frames and every `reasoning` part exactly 2 (profile §②, over
//     27,047 parsed rows). So a delta must be derived by diffing neighbours.
//   - the provider's `seq` is monotonic **per aggregate** (one session measured 0..1547),
//     so it is not a legal value for our idempotency key: §16.2 bans reusing it, §6 规则 5
//     is (turn_ref, sequence_no). `seq` appears here only as an ordering input and as the
//     read cursor; it is never published as a sequence_no.
//   - 已知限制 (§16.2 asks for this sentence where the code is, not in a ticket): a
//     reconnect with a cursor backfills the **snapshot sequence**, not the intermediate
//     states that were never written to disk. Nothing downstream may read the gap as
//     "Core lost content".
//
// Numbering rule, stated because the whole criterion depends on it: every segment is
// numbered per message (that is our "turn" for an observed run), starting at 1, and the
// numbers are assigned while replaying the journal from its beginning even when only the
// tail is emitted. So the same segment carries the same number in a full read and in a
// continuation - which is what makes a (turn_ref, sequence_no) dedup meaningful at all.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/SadCat18/lucero/core/internal/adapter"
)

// eventPartUpdated is the only journal type that carries transcript growth. The other
// three names the profile measured (message/session object updates) are listed in
// journalTypes so a row of an unknown type is counted and skipped rather than guessed.
const (
	eventPartUpdated    = "message.part.updated.1"
	eventMessageUpdated = "message.updated.1"
	eventSessionUpdated = "session.updated.1"
	eventSessionCreated = "session.created.1"
)

// maxJournalRows caps one transcript read. The profile's largest measured session is
// 1,547 rows, so hitting this cap means the store is not the shape we recorded, and the
// reply says truncated=true instead of quietly returning a prefix.
const maxJournalRows = 20000

// defaultTranscriptLimit / maxTranscriptLimit bound how many messages one reply carries.
const (
	defaultTranscriptLimit = 50
	maxTranscriptLimit     = 200
	defaultSessionLimit    = 50
	maxSessionLimit        = 200
)

// sessionRow is §4's session/list row. §4 names four fields; `title` has no recorded
// source in the profile (CO-64), so it stays empty and is reported rather than invented
// from the first message's text.
type sessionRow struct {
	RemoteSessionID string `json:"remote_session_id"`
	Title           string `json:"title"`
	ProjectPathHint string `json:"project_path_hint"`
	UpdatedAt       int64  `json:"updated_at"`
	// UpdatedAtUnit says which column this came from and whether it was scaled. An
	// absent source is "" and UpdatedAt 0 together, which is what lets a reader tell
	// "no time column" from "epoch", per §7's discipline generalised to read fields.
	UpdatedAtUnit string `json:"updated_at_unit,omitempty"`
}

type sessionListParams struct {
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type sessionListResult struct {
	Sessions   []sessionRow `json:"sessions"`
	NextCursor string       `json:"next_cursor"`
}

// segment is one synthesized piece of a part's growth. Synthesized is always true and
// never defaulted, because a reader must be able to tell "this came from the provider"
// from "this is our diff" after the JSON is on the wire (§6 规则 6①'s misreading risk,
// which §16.3 says is *worse* on this product).
type segment struct {
	SequenceNo  int64  `json:"sequence_no"`
	Text        string `json:"text"`
	Synthesized bool   `json:"synthesized"`
	// Replaced marks a snapshot that is not an extension of its neighbour. Diffing that
	// can honestly only be reported as "the text changed wholesale"; pretending it was an
	// appended delta would invent an ordering the provider never had.
	Replaced bool `json:"replaced,omitempty"`
}

// partView is one part: its latest snapshot plus the growth reconstructed around it.
type partView struct {
	PartID    string    `json:"part_id"`
	Type      string    `json:"type"`
	Text      string    `json:"text"`
	State     string    `json:"state,omitempty"`
	Segments  []segment `json:"segments,omitempty"`
	Snapshots int       `json:"snapshots"`
}

// messageView groups parts under the provider's messageID. Role has no recorded source
// (CO-64) and is published empty; position is not evidence of role, and guessing it from
// "every other message" would be exactly the fabrication §16.7's range forbids.
type messageView struct {
	RemoteMessageID string     `json:"remote_message_id"`
	Role            string     `json:"role"`
	Text            string     `json:"text"`
	Parts           []partView `json:"parts,omitempty"`
}

type sessionReadParams struct {
	RemoteSessionID string `json:"remote_session_id"`
	SinceCursor     string `json:"since_cursor,omitempty"`
	Limit           int    `json:"limit,omitempty"`
}

type sessionReadResult struct {
	Messages       []messageView `json:"messages"`
	NextCursor     string        `json:"next_cursor"`
	RemoteRevision string        `json:"remote_revision"`
	Truncated      bool          `json:"truncated,omitempty"`
}

func (o *opencode) sessionList(ctx context.Context, req *adapter.Frame) error {
	var p sessionListParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return o.reply(errReply(req, badParams(req.Method, err)))
	}
	s, err := o.ensureStore()
	if err != nil {
		return o.reply(errReply(req, &adapter.Error{Code: codeLocalDataUnavailable, Message: err.Error()}))
	}
	limit := clampLimit(p.Limit, defaultSessionLimit, maxSessionLimit)
	offset, err := decodeOffsetCursor(p.Cursor)
	if err != nil {
		return o.reply(errReply(req, err))
	}
	sessions, more, err := s.listSessions(ctx, offset, limit)
	if err != nil {
		return o.reply(errReply(req, storeUnavailable(err)))
	}
	next := ""
	if more {
		next = encodeOffset(offset + int64(len(sessions)))
	}
	fmt.Fprintf(o.logs, "session/list: %d rows from offset %d, more=%v\n", len(sessions), offset, more)
	return o.reply(result(req, sessionListResult{Sessions: sessions, NextCursor: next}))
}

func (o *opencode) sessionRead(ctx context.Context, req *adapter.Frame) error {
	var p sessionReadParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return o.reply(errReply(req, badParams(req.Method, err)))
	}
	if p.RemoteSessionID == "" {
		return o.reply(errReply(req, &adapter.Error{Code: adapter.ErrInvalidParams,
			Message: "session/read has no remote_session_id (§4)"}))
	}
	s, err := o.ensureStore()
	if err != nil {
		return o.reply(errReply(req, storeUnavailable(err)))
	}
	limit := clampLimit(p.Limit, defaultTranscriptLimit, maxTranscriptLimit)
	after, err := decodeSeqCursor(p.SinceCursor)
	if err != nil {
		return o.reply(errReply(req, err))
	}
	res, err := s.transcript(ctx, p.RemoteSessionID, after, limit)
	var nf *notFound
	if errors.As(err, &nf) {
		// §15.1's SESSION_NOT_FOUND has no Go constant in internal/adapter on purpose
		// (Adapter-produced, §15.1.1); this is the call site that names the number.
		return o.reply(errReply(req, &adapter.Error{Code: codeSessionNotFound,
			Message: "session/read: no aggregate " + nf.id + " in this store (§4)"}))
	}
	if err != nil {
		return o.reply(errReply(req, storeUnavailable(err)))
	}
	fmt.Fprintf(o.logs, "session/read %s: %d messages, watermark seq %d, truncated=%v\n",
		p.RemoteSessionID, len(res.Messages), res.remoteWatermark(), res.Truncated)
	return o.reply(result(req, res))
}

// notFound means the aggregate has no journal rows at all. It is a type, not an empty
// result, because "no such session" and "a session with nothing in it" are two answers a
// panel must not merge.
type notFound struct{ id string }

func (e *notFound) Error() string { return "no journal rows for session " + e.id }

// partSnapshot is one payload of message.part.updated.1. Recorded shape: top level
// [part, sessionID, time], part level [id, messageID, sessionID, text, type], and tool
// parts additionally a state object with a status among {completed, error, running}
// (profile §①/§②). Unknown extra keys stay unread rather than mapped onto our fields.
type partSnapshot struct {
	Part struct {
		ID        string `json:"id"`
		MessageID string `json:"messageID"`
		Type      string `json:"type"`
		Text      string `json:"text"`
		State     *struct {
			Status string `json:"status"`
		} `json:"state,omitempty"`
	} `json:"part"`
}

// listSessions pages the provider's session table by row offset, newest first by
// whichever time column exists (falling back to rowid so the order is still stable).
func (s *sqlStore) listSessions(ctx context.Context, offset int64, limit int) ([]sessionRow, bool, error) {
	timeCol, err := s.firstColumn(ctx, "session", "time_updated", "updated_at", "time")
	if err != nil {
		return nil, false, err
	}
	titleCol, err := s.firstColumn(ctx, "session", "title", "name")
	if err != nil {
		return nil, false, err
	}
	pathCol, err := s.firstColumn(ctx, "session", "directory", "project_path", "project_id")
	if err != nil {
		return nil, false, err
	}
	orderBy := "rowid"
	if timeCol != "" {
		orderBy = quoteIdent(timeCol) + " DESC, rowid DESC"
	}
	sel := []string{"id"}
	for _, pair := range [][2]string{{titleCol, "title"}, {pathCol, "project"}, {timeCol, "updated"}} {
		col, name := pair[0], pair[1]
		if col != "" {
			sel = append(sel, quoteIdent(col)+" AS "+name)
		}
	}
	q := fmt.Sprintf("SELECT %s FROM session ORDER BY %s LIMIT ? OFFSET ?", strings.Join(sel, ", "), orderBy)
	rows, err := s.query(ctx, q, limit+1, offset)
	if err != nil {
		return nil, false, err
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	out := make([]sessionRow, 0, len(rows))
	for _, r := range rows {
		row := sessionRow{
			RemoteSessionID: fieldText(r, "id"),
			Title:           fieldText(r, "title"),
			ProjectPathHint: fieldText(r, "project"),
		}
		if c, ok := r["updated"]; ok {
			if n, numOK := c.number(); numOK {
				row.UpdatedAt, row.UpdatedAtUnit = millis(n)
			}
		}
		if row.RemoteSessionID == "" {
			return nil, false, fmt.Errorf("session table has an id column that reads empty; not this product's schema")
		}
		out = append(out, row)
	}
	return out, more, nil
}

// accPart and accMessage are transcript()'s accumulators: the last snapshot seen for a
// part (the diff base) and the per-turn segment counter, which keeps counting through
// the rows a cursor skips so the numbers stay stable across reads.
type accPart struct {
	view partView
	last string
}

type accMessage struct {
	id       string
	parts    map[string]*accPart
	order    []string
	numbered int64
	maxSeq   int64
}

// transcript rebuilds one session's journal into messages and synthesizes each part's
// growth from its neighbouring snapshots.
func (s *sqlStore) transcript(ctx context.Context, sessionID string, after int64, limit int) (sessionReadResult, error) {
	rows, err := s.query(ctx,
		"SELECT seq, type, data FROM event WHERE aggregate_id = ? ORDER BY seq ASC LIMIT ?",
		sessionID, maxJournalRows)
	if err != nil {
		return sessionReadResult{}, err
	}
	if len(rows) == 0 {
		return sessionReadResult{}, &notFound{id: sessionID}
	}

	// accPart/accMessage are the accumulator pair. They are package-level because they
	// reference each other, and they exist because numbering has to survive the cursor
	// skip: a segment's number is assigned while replaying from the beginning, so the
	// counters count numbered segments, not emitted ones.
	var (
		messages   = map[string]*accMessage{}
		msgOrder   []string
		unknown    int
		globalMax  int64
		partSeen   = 0
		snapshotNo = 0
	)
	for _, r := range rows {
		seqCell, ok := r["seq"]
		if !ok {
			return sessionReadResult{}, fmt.Errorf("journal row has no seq column")
		}
		n, _ := seqCell.number()
		watermark := int64(n)
		if watermark > globalMax {
			globalMax = watermark
		}
		typ := r["type"].text()
		if typ != eventPartUpdated {
			if !knownEventType(typ) {
				unknown++
			}
			continue
		}
		var snap partSnapshot
		if err := json.Unmarshal(r["data"].bytes(), &snap); err != nil {
			return sessionReadResult{}, fmt.Errorf("journal payload at seq %d: %w", watermark, err)
		}
		p := snap.Part
		if p.ID == "" {
			continue
		}
		m := messages[p.MessageID]
		if m == nil {
			m = &accMessage{id: p.MessageID, parts: map[string]*accPart{}}
			messages[p.MessageID] = m
			msgOrder = append(msgOrder, p.MessageID)
		}
		if watermark > m.maxSeq {
			m.maxSeq = watermark
		}
		pa := m.parts[p.ID]
		if pa == nil {
			pa = &accPart{}
			m.parts[p.ID] = pa
			m.order = append(m.order, p.ID)
			partSeen++
		}
		// PartID is set on every row, not only the first: it is the identity a reader
		// joins segments by, and a reply whose parts all carry an empty id is the kind of
		// shape defect only a wire-level test catches (this one did).
		pa.view.PartID, pa.view.Type, pa.view.Text = p.ID, p.Type, p.Text
		pa.view.Snapshots++
		if p.State != nil {
			pa.view.State = p.State.Status
		}
		snapshotNo++

		// One numbered segment per snapshot. The first is the whole text (there is no
		// earlier neighbour to diff against); later ones are the appended tail, or a
		// flagged wholesale replacement.
		seg := segment{SequenceNo: m.numbered + 1, Text: p.Text, Synthesized: true}
		if pa.view.Snapshots > 1 {
			if strings.HasPrefix(p.Text, pa.last) {
				seg.Text = p.Text[len(pa.last):]
			} else {
				seg.Replaced = true
			}
		}
		m.numbered++
		pa.last = p.Text
		if watermark > after {
			pa.view.Segments = append(pa.view.Segments, seg)
		}
	}
	if len(rows) >= maxJournalRows {
		fmt.Fprintf(s.log, "transcript %s: journal capped at %d rows; reply says truncated=true\n", sessionID, maxJournalRows)
	}
	if unknown > 0 {
		fmt.Fprintf(s.log, "transcript %s: %d rows of a type outside the 4 measured ones, skipped not guessed\n", sessionID, unknown)
	}

	out := sessionReadResult{RemoteRevision: "seq-" + strconv.FormatInt(globalMax, 10),
		NextCursor: encodeSeq(globalMax), Truncated: len(rows) >= maxJournalRows}
	for _, id := range msgOrder {
		if len(out.Messages) >= limit {
			out.Truncated = true
			break
		}
		m := messages[id]
		mv := messageView{RemoteMessageID: m.id}
		var text strings.Builder
		for _, pid := range m.order {
			pv := m.parts[pid].view
			mv.Parts = append(mv.Parts, pv)
			if pv.Type == "text" {
				text.WriteString(pv.Text)
			}
		}
		mv.Text = text.String()
		out.Messages = append(out.Messages, mv)
	}
	fmt.Fprintf(s.log, "transcript %s: %d journal rows, %d messages, %d parts, %d snapshots\n",
		sessionID, len(rows), len(msgOrder), partSeen, snapshotNo)
	return out, nil
}

func knownEventType(t string) bool {
	switch t {
	case eventPartUpdated, eventMessageUpdated, eventSessionUpdated, eventSessionCreated:
		return true
	}
	return false
}

func clampLimit(v, def, max int) int {
	if v <= 0 {
		return def
	}
	if v > max {
		return max
	}
	return v
}

// millis decides the unit by magnitude rather than trusting a column name: §4 wants Unix
// milliseconds, and a seconds-granularity column would otherwise read as 1970. The unit
// travels with the value so a reader can undo the choice.
func millis(v float64) (int64, string) {
	if v < 1e11 {
		return int64(v * 1000), "second"
	}
	return int64(v), "millisecond"
}

func fieldText(r map[string]cell, key string) string {
	c, ok := r[key]
	if !ok {
		return ""
	}
	return c.text()
}

// Two cursors, on purpose: session/list pages by row offset, session/read resumes by
// journal watermark. Merging them into one opaque string would make a read of one method's
// cursor into the other silently wrong, and §4 gives each its own field.
func encodeOffset(offset int64) string { return "o" + strconv.FormatInt(offset, 10) }
func encodeSeq(seq int64) string       { return "e" + strconv.FormatInt(seq, 10) }

func decodeOffsetCursor(c string) (int64, error) {
	if c == "" {
		return 0, nil
	}
	if !strings.HasPrefix(c, "o") {
		return 0, &adapter.Error{Code: adapter.ErrInvalidParams,
			Message: "session/list cursor " + c + " is not an offset cursor of this build (expected \"o<n>\")"}
	}
	n, err := strconv.ParseInt(c[1:], 10, 64)
	if err != nil || n < 0 {
		return 0, &adapter.Error{Code: adapter.ErrInvalidParams,
			Message: "session/list cursor " + c + " does not decode to a non-negative offset"}
	}
	return n, nil
}

func decodeSeqCursor(c string) (int64, error) {
	if c == "" {
		return 0, nil
	}
	if !strings.HasPrefix(c, "e") {
		return 0, &adapter.Error{Code: adapter.ErrInvalidParams,
			Message: "session/read since_cursor " + c + " is not a journal watermark of this build (expected \"e<n>\")"}
	}
	n, err := strconv.ParseInt(c[1:], 10, 64)
	if err != nil || n < 0 {
		return 0, &adapter.Error{Code: adapter.ErrInvalidParams,
			Message: "session/read since_cursor " + c + " does not decode to a non-negative seq"}
	}
	return n, nil
}

// storeUnavailable maps a store failure onto §15.1: the registered code for "we know the
// provider, the local content is not readable" is LOCAL_DATA_UNAVAILABLE, and it stays
// retryable rather than becoming INTERNAL, which would read as our own bug.
func storeUnavailable(err error) error {
	return &adapter.Error{Code: codeLocalDataUnavailable, Message: err.Error()}
}

// remoteWatermark is a logging aid: the cursor this reply ended at.
func (r sessionReadResult) remoteWatermark() int64 {
	n, _ := decodeSeqCursor(r.NextCursor)
	return n
}
