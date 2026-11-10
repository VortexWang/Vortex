package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http"
	"time"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
)

func eventScope(p auth.Principal, tenant string) CursorScope {
	return CursorScope{PrincipalID: p.ID(), TenantID: tenant, Collection: "events", Direction: "forward"}
}
func (a *API) eventID(p auth.Principal, tenant string, sequence int64) (string, error) {
	return a.config.Cursors.EncodeEventCursor(eventScope(p, tenant), sequence)
}
func (a *API) streamEvents(c *gin.Context) {
	scope, ok := scope(c, policy.Read)
	if !ok {
		return
	}
	if c.Request.URL.RawQuery != "" {
		fail(c, 400, "invalid_request", "Unexpected query parameters", nil)
		return
	}
	p := c.MustGet("principal").(auth.Principal)
	after := int64(0)
	headers := c.Request.Header.Values("Last-Event-ID")
	if len(headers) > 1 {
		fail(c, 400, "invalid_cursor", "Invalid event cursor", nil)
		return
	}
	if len(headers) == 1 {
		var e error
		after, e = a.config.Cursors.DecodeEventCursor(headers[0], eventScope(p, scope.TenantID()))
		if e != nil {
			fail(c, 400, "invalid_cursor", "Invalid event cursor", nil)
			return
		}
	}
	select {
	case a.streams <- struct{}{}:
		defer func() { <-a.streams }()
	default:
		c.Header("Retry-After", "5")
		fail(c, 429, "too_many_streams", "Too many event streams", nil)
		return
	}
	events := repository.DomainEvents{Pool: a.config.Pool}
	page, e := events.Replay(c.Request.Context(), scope, after, 100, requestID(c))
	if e != nil {
		readError(c, e)
		return
	}
	if after > page.LastSequence {
		fail(c, 400, "invalid_cursor", "Invalid event cursor", nil)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Minute)
	defer cancel()
	controller := http.NewResponseController(c.Writer)
	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("X-Accel-Buffering", "no")
	write := func(raw []byte) error {
		if e := controller.SetWriteDeadline(time.Now().Add(5 * time.Second)); e != nil && !errors.Is(e, http.ErrNotSupported) {
			return e
		}
		if _, e := c.Writer.Write(raw); e != nil {
			return e
		}
		return controller.Flush()
	}
	send := func(kind string, sequence int64, data any) error {
		id, e := a.eventID(p, scope.TenantID(), sequence)
		if e != nil {
			return e
		}
		raw, e := json.Marshal(data)
		if e != nil {
			return e
		}
		return write([]byte(fmt.Sprintf("id: %s\nevent: %s\ndata: %s\n\n", id, kind, raw)))
	}
	if e = write([]byte(": connected\n\n")); e != nil {
		return
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		if after < page.RetainedAfter {
			_ = send("resync", page.LastSequence, struct {
				Reason string `json:"reason"`
			}{"retention_gap"})
			return
		}
		for _, event := range page.Events {
			if event.Sequence != after+1 {
				_ = send("resync", page.LastSequence, struct {
					Reason string `json:"reason"`
				}{"event_gap"})
				return
			}
			if e = send(event.Type, event.Sequence, struct {
				ResourceID string          `json:"resource_id"`
				Revision   int64           `json:"revision"`
				Summary    json.RawMessage `json:"summary"`
			}{event.ResourceID, event.Revision, event.Summary}); e != nil {
				return
			}
			after = event.Sequence
		}
		// Always revalidate before the next page/heartbeat; long streams cannot keep
		// an expired, revoked, suspended or retargeted principal alive.
		sendHeartbeat := false
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			sendHeartbeat = true
		case <-ticker.C:
		}
		current, e := a.config.Tokens.Authenticate(ctx, c.GetString("access_token"))
		if e != nil {
			return
		}
		next, e := repository.NewTenantScope(current, scope.TenantID(), policy.Read)
		if e != nil {
			return
		}
		scope = next
		p = current
		if sendHeartbeat {
			if e = write([]byte(": heartbeat\n\n")); e != nil {
				return
			}
		}
		page, e = events.Replay(ctx, scope, after, 100, requestID(c))
		if e != nil {
			return
		}
	}
}
