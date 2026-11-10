package httpapi

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"net/url"
	"strconv"
	"time"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
)

func pageSize(c *gin.Context) (int, url.Values, bool) {
	query, e := url.ParseQuery(c.Request.URL.RawQuery)
	if e != nil {
		fail(c, 400, "invalid_request", "Invalid query parameters", nil)
		return 0, nil, false
	}
	for k, values := range query {
		if (k != "limit" && k != "cursor") || len(values) != 1 {
			fail(c, 400, "invalid_request", "Invalid query parameters", nil)
			return 0, nil, false
		}
	}
	limit := 50
	if raw, exists := query["limit"]; exists {
		v, e := strconv.Atoi(raw[0])
		if e != nil || v < 1 || v > 100 || strconv.Itoa(v) != raw[0] {
			fail(c, 400, "invalid_request", "Invalid page size", map[string]string{"limit": "Must be between 1 and 100"})
			return 0, nil, false
		}
		limit = v
	}
	return limit, query, true
}
func (a *API) resourceList(collection string) gin.HandlerFunc {
	return func(c *gin.Context) {
		p := c.MustGet("principal").(auth.Principal)
		var s repository.TenantScope
		var ok bool
		if collection == "tenants" {
			tenant := p.TenantID()
			if p.Platform() {
				tenant = p.ID()
			}
			var e error
			s, e = repository.NewTenantScope(p, tenant, policy.Read)
			if e != nil {
				fail(c, 403, "forbidden", "Access denied", nil)
				return
			}
		} else {
			s, ok = scope(c, policy.Read)
			if !ok {
				return
			}
		}
		limit, query, ok := pageSize(c)
		if !ok {
			return
		}
		cs := CursorScope{PrincipalID: p.ID(), TenantID: s.TenantID(), Collection: collection, Direction: "forward"}
		var position CursorPosition
		if raw, exists := query["cursor"]; exists {
			v, e := a.config.Cursors.Decode(raw[0], cs, time.Now())
			if e != nil {
				fail(c, 400, "invalid_cursor", "Invalid or expired cursor", nil)
				return
			}
			position = v
		}
		rows, e := (repository.PublicResources{Pool: a.config.Pool}).List(c.Request.Context(), s, collection, position.CreatedAt, position.ID, limit+1, requestID(c))
		if e != nil {
			readError(c, e)
			return
		}
		next := ""
		if len(rows) > limit {
			rows = rows[:limit]
			last := rows[len(rows)-1]
			next, e = a.config.Cursors.Encode(cs, CursorPosition{CreatedAt: last.CreatedAt, ID: last.ID}, time.Now())
			if e != nil {
				fail(c, 500, "internal_error", "Request could not be completed", nil)
				return
			}
		}
		items := make([]json.RawMessage, 0, len(rows))
		for _, row := range rows {
			items = append(items, row.Body)
		}
		c.JSON(200, struct {
			Items []json.RawMessage `json:"items"`
			Next  string            `json:"next_cursor,omitempty"`
		}{items, next})
	}
}
func (a *API) registerResourceLists(secured *gin.RouterGroup) {
	secured.GET("/me", func(c *gin.Context) {
		if c.Request.URL.RawQuery != "" {
			fail(c, 400, "invalid_request", "Unexpected query parameters", nil)
			return
		}
		p := c.MustGet("principal").(auth.Principal)
		c.JSON(200, struct {
			ID       string    `json:"id"`
			TenantID string    `json:"tenant_id,omitempty"`
			Role     auth.Role `json:"role"`
			Platform bool      `json:"platform"`
		}{p.ID(), p.TenantID(), p.Role(), p.Platform()})
	})
	secured.GET("/tenants", a.resourceList("tenants"))
	for _, collection := range []string{"nodes", "node-groups", "rules", "packages", "subscriptions", "port-allocations", "sources", "restrictions"} {
		secured.GET("/tenants/:tenant_id/"+collection, a.resourceList(collection))
	}
}
