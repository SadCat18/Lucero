package adapter

import (
	"crypto/rand"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// RequestID is a UUIDv7 string, the shape §1 requires on every call. v7 rather than
// v4 because the leading 48 bits are a millisecond timestamp, so ids sort by when
// the call was made - which is what makes a replay or a stuck request visible in a
// log instead of something to eyeball by hand.
type RequestID string

func (r RequestID) String() string { return string(r) }

// clock is swappable so a test can pin the timestamp bits; production uses time.Now.
var clock = time.Now

// NewRequestID returns a fresh UUIDv7. crypto/rand failing means a broken platform
// rather than a runtime condition, so it panics instead of handing back an error
// every call site would have to remember to check.
func NewRequestID() RequestID {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("adapter: no randomness for request ids: " + err.Error())
	}
	ms := uint64(clock().UnixMilli())
	b[0], b[1], b[2], b[3], b[4], b[5] = byte(ms>>40), byte(ms>>32), byte(ms>>24),
		byte(ms>>16), byte(ms>>8), byte(ms)
	b[6] = 0x70 | b[6]&0x0f // version 7
	b[8] = 0x80 | b[8]&0x3f // RFC 4122 variant
	return RequestID(fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]))
}

// Timestamp reads back when an id was minted. ok=false for anything that is not a
// well-formed v7, which keeps a forged or truncated id from looking like a time.
func (r RequestID) Timestamp() (time.Time, bool) {
	hex := strings.ReplaceAll(string(r), "-", "")
	if len(hex) != 32 || hex[12] != '7' {
		return time.Time{}, false
	}
	ms, err := strconv.ParseUint(hex[:12], 16, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.UnixMilli(int64(ms)), true
}
