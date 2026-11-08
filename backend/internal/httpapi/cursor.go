package httpapi

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrCursor = errors.New("invalid or expired cursor")

type CursorScope struct {
	PrincipalID string `json:"principal"`
	TenantID    string `json:"tenant"`
	Collection  string `json:"collection"`
	Direction   string `json:"direction"`
	Filter      string `json:"filter"`
}
type CursorPosition struct {
	CreatedAt time.Time
	ID        string
}
type CursorCodec struct {
	key []byte
	ttl time.Duration
}
type cursorPayload struct {
	Version   int         `json:"v"`
	Scope     CursorScope `json:"scope"`
	CreatedAt string      `json:"created_at"`
	ID        string      `json:"id"`
	IssuedAt  int64       `json:"iat"`
	ExpiresAt int64       `json:"exp"`
}

func NewCursorCodec(key []byte, ttl time.Duration) (*CursorCodec, error) {
	if len(key) < 32 || len(key) > 128 || ttl < time.Second || ttl > 24*time.Hour || ttl%time.Second != 0 {
		return nil, ErrCursor
	}
	return &CursorCodec{key: append([]byte(nil), key...), ttl: ttl}, nil
}
func cursorScopeValid(s CursorScope) bool {
	for _, v := range []string{s.PrincipalID, s.TenantID, s.Collection} {
		if len(v) == 0 || len(v) > 128 || !utf8.ValidString(v) {
			return false
		}
	}
	return (s.Direction == "forward" || s.Direction == "backward") && len(s.Filter) <= 256 && utf8.ValidString(s.Filter)
}
func cursorPositionValid(p CursorPosition) bool {
	return !p.CreatedAt.IsZero() && p.CreatedAt.Equal(p.CreatedAt.Truncate(time.Microsecond)) && len(p.ID) > 0 && len(p.ID) <= 128 && utf8.ValidString(p.ID)
}
func (c *CursorCodec) mac(raw []byte) []byte {
	m := hmac.New(sha256.New, c.key)
	m.Write([]byte("vortex/api/cursor/v1\x00"))
	m.Write(raw)
	return m.Sum(nil)
}
func (c *CursorCodec) Encode(scope CursorScope, p CursorPosition, now time.Time) (string, error) {
	if c == nil || !cursorScopeValid(scope) || !cursorPositionValid(p) || now.IsZero() {
		return "", ErrCursor
	}
	payload := cursorPayload{Version: 1, Scope: scope, CreatedAt: p.CreatedAt.UTC().Format(time.RFC3339Nano), ID: p.ID, IssuedAt: now.Unix(), ExpiresAt: now.Add(c.ttl).Unix()}
	raw, e := json.Marshal(payload)
	if e != nil {
		return "", ErrCursor
	}
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(c.mac(raw)), nil
}
func (c *CursorCodec) Decode(token string, scope CursorScope, now time.Time) (CursorPosition, error) {
	var zero CursorPosition
	if c == nil || len(token) == 0 || len(token) > 4096 || !cursorScopeValid(scope) || now.IsZero() {
		return zero, ErrCursor
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return zero, ErrCursor
	}
	raw, e := base64.RawURLEncoding.DecodeString(parts[0])
	if e != nil || base64.RawURLEncoding.EncodeToString(raw) != parts[0] {
		return zero, ErrCursor
	}
	signature, e := base64.RawURLEncoding.DecodeString(parts[1])
	if e != nil || base64.RawURLEncoding.EncodeToString(signature) != parts[1] || !hmac.Equal(signature, c.mac(raw)) {
		return zero, ErrCursor
	}
	var p cursorPayload
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&p) != nil {
		return zero, ErrCursor
	}
	if decoder.Decode(new(any)) != io.EOF {
		return zero, ErrCursor
	}
	canonical, e := json.Marshal(p)
	if e != nil || !bytes.Equal(canonical, raw) || p.Version != 1 || p.Scope != scope || p.IssuedAt > now.Unix() || p.ExpiresAt <= now.Unix() || p.ExpiresAt <= p.IssuedAt || p.ExpiresAt-p.IssuedAt > int64(c.ttl/time.Second) {
		return zero, ErrCursor
	}
	at, e := time.Parse(time.RFC3339Nano, p.CreatedAt)
	if e != nil || at.UTC().Format(time.RFC3339Nano) != p.CreatedAt {
		return zero, ErrCursor
	}
	position := CursorPosition{CreatedAt: at, ID: p.ID}
	if !cursorPositionValid(position) {
		return zero, ErrCursor
	}
	return position, nil
}

// Event cursors have stable IDs. Retention is checked against the durable PG
// floor rather than a newly minted wall-clock expiry on each delivery.
type eventCursorPayload struct {
	Version  int         `json:"v"`
	Scope    CursorScope `json:"scope"`
	Sequence string      `json:"sequence"`
}

func (c *CursorCodec) eventMAC(raw []byte) []byte {
	m := hmac.New(sha256.New, c.key)
	m.Write([]byte("vortex/api/event-cursor/v1\x00"))
	m.Write(raw)
	return m.Sum(nil)
}
func eventCursorScopeValid(scope CursorScope) bool {
	return cursorScopeValid(scope) && scope.Collection == "events" && scope.Direction == "forward" && scope.Filter == ""
}
func (c *CursorCodec) EncodeEventCursor(scope CursorScope, sequence int64) (string, error) {
	if c == nil || !eventCursorScopeValid(scope) || sequence < 0 {
		return "", ErrCursor
	}
	raw, e := json.Marshal(eventCursorPayload{Version: 1, Scope: scope, Sequence: strconv.FormatInt(sequence, 10)})
	if e != nil {
		return "", ErrCursor
	}
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(c.eventMAC(raw)), nil
}
func (c *CursorCodec) DecodeEventCursor(token string, scope CursorScope) (int64, error) {
	if c == nil || !eventCursorScopeValid(scope) || len(token) == 0 || len(token) > 2048 {
		return 0, ErrCursor
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return 0, ErrCursor
	}
	raw, e := base64.RawURLEncoding.DecodeString(parts[0])
	if e != nil || base64.RawURLEncoding.EncodeToString(raw) != parts[0] {
		return 0, ErrCursor
	}
	signature, e := base64.RawURLEncoding.DecodeString(parts[1])
	if e != nil || base64.RawURLEncoding.EncodeToString(signature) != parts[1] || !hmac.Equal(signature, c.eventMAC(raw)) {
		return 0, ErrCursor
	}
	var value eventCursorPayload
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&value) != nil || d.Decode(new(any)) != io.EOF {
		return 0, ErrCursor
	}
	canonical, e := json.Marshal(value)
	if e != nil || !bytes.Equal(canonical, raw) || value.Version != 1 || value.Scope != scope {
		return 0, ErrCursor
	}
	sequence, e := strconv.ParseInt(value.Sequence, 10, 64)
	if e != nil || sequence < 0 || strconv.FormatInt(sequence, 10) != value.Sequence {
		return 0, ErrCursor
	}
	return sequence, nil
}
