package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/compiler"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	"vortex/backend/internal/service"
)

type Config struct {
	Pool    *pgxpool.Pool
	Tokens  *auth.Tokens
	Cursors *CursorCodec
}
type API struct {
	config  Config
	streams chan struct{}
}
type apiError struct {
	Code        string            `json:"code"`
	Message     string            `json:"message"`
	RequestID   string            `json:"request_id"`
	FieldErrors map[string]string `json:"field_errors,omitempty"`
}

func NewRouter(config Config) (http.Handler, error) {
	if config.Pool == nil || config.Tokens == nil || config.Cursors == nil {
		return nil, errors.New("incomplete API dependencies")
	}
	a := &API{config: config, streams: make(chan struct{}, 64)}
	r := gin.New()
	if e := r.SetTrustedProxies(nil); e != nil {
		return nil, e
	}
	r.Use(func(c *gin.Context) {
		c.Set("request_id", uuid.NewString())
		c.Header("X-Request-ID", requestID(c))
		c.Header("Cache-Control", "no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		defer func() {
			if recover() != nil {
				if !c.Writer.Written() {
					fail(c, 500, "internal_error", "Request could not be completed", nil)
				}
				c.Abort()
			}
		}()
		c.Next()
	})
	r.NoRoute(func(c *gin.Context) { fail(c, 404, "not_found", "Resource not found", nil) })
	r.POST("/api/v1/auth/refresh", a.refresh)
	secured := r.Group("/api/v1", a.authenticate)
	secured.POST("/auth/logout", a.logout)
	a.registerResourceReads(secured)
	a.registerResourceLists(secured)
	secured.GET("/tenants/:tenant_id/events", a.streamEvents)
	secured.PUT("/tenants/:tenant_id/rules/:rule_id", a.reviseRule)
	secured.PATCH("/tenants/:tenant_id/nodes/:node_id", a.transitionNode)
	secured.PUT("/tenants/:tenant_id/packages/:package_id", a.putPackageCatalog)
	secured.GET("/tenants/:tenant_id/operations", a.listOperations)
	secured.GET("/tenants/:tenant_id/operations/:operation_id", a.operation)
	return r, nil
}
func requestID(c *gin.Context) string { return c.GetString("request_id") }
func fail(c *gin.Context, status int, code, message string, fields map[string]string) {
	c.AbortWithStatusJSON(status, apiError{Code: code, Message: message, RequestID: requestID(c), FieldErrors: fields})
}
func decodeBody(c *gin.Context, v any) bool {
	if c.ContentType() != "application/json" {
		fail(c, 415, "unsupported_media_type", "JSON body required", nil)
		return false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16*1024)
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(new(any)) != io.EOF {
		fail(c, 400, "invalid_request", "Invalid request body", nil)
		return false
	}
	return true
}
func bearer(c *gin.Context) (string, bool) {
	values := c.Request.Header.Values("Authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		return "", false
	}
	token := strings.TrimPrefix(values[0], "Bearer ")
	return token, len(token) > 0 && !strings.ContainsAny(token, " \t\r\n,")
}
func (a *API) authenticate(c *gin.Context) {
	token, ok := bearer(c)
	if !ok {
		fail(c, 401, "unauthorized", "Authentication required", nil)
		return
	}
	p, e := a.config.Tokens.Authenticate(c.Request.Context(), token)
	if e != nil {
		if errors.Is(e, auth.ErrToken) {
			fail(c, 401, "unauthorized", "Authentication required", nil)
		} else {
			fail(c, 503, "unavailable", "Service temporarily unavailable", nil)
		}
		return
	}
	c.Set("principal", p)
	c.Set("access_token", token)
	c.Next()
}
func (a *API) refresh(c *gin.Context) {
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	if !decodeBody(c, &body) {
		return
	}
	pair, e := a.config.Tokens.Refresh(c.Request.Context(), body.RefreshToken, requestID(c))
	if e != nil {
		if errors.Is(e, auth.ErrToken) || errors.Is(e, auth.ErrTokenReuse) {
			fail(c, 401, "unauthorized", "Authentication required", nil)
		} else {
			fail(c, 503, "unavailable", "Service temporarily unavailable", nil)
		}
		return
	}
	c.JSON(http.StatusOK, pair)
}
func (a *API) logout(c *gin.Context) {
	if e := a.config.Tokens.Revoke(c.Request.Context(), c.GetString("access_token"), requestID(c)); e != nil {
		fail(c, 503, "unavailable", "Service temporarily unavailable", nil)
		return
	}
	c.Status(http.StatusNoContent)
}
func scope(c *gin.Context, action policy.Action) (repository.TenantScope, bool) {
	p, ok := c.MustGet("principal").(auth.Principal)
	if !ok {
		fail(c, 401, "unauthorized", "Authentication required", nil)
		return repository.TenantScope{}, false
	}
	s, e := repository.NewTenantScope(p, c.Param("tenant_id"), action)
	if e != nil {
		fail(c, 403, "forbidden", "Access denied", nil)
		return repository.TenantScope{}, false
	}
	return s, true
}
func (a *API) operation(c *gin.Context) {
	s, ok := scope(c, policy.Read)
	if !ok {
		return
	}
	id := c.Param("operation_id")
	if _, e := uuid.Parse(id); e != nil {
		fail(c, 400, "invalid_request", "Invalid resource identifier", map[string]string{"operation_id": "Invalid identifier"})
		return
	}
	v, e := (repository.PublicOperations{Pool: a.config.Pool}).Get(c.Request.Context(), s, id, requestID(c))
	if e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			fail(c, 404, "not_found", "Resource not found", nil)
		} else {
			fail(c, 503, "unavailable", "Service temporarily unavailable", nil)
		}
		return
	}
	c.JSON(200, v)
}
func (a *API) listOperations(c *gin.Context) {
	s, ok := scope(c, policy.Read)
	if !ok {
		return
	}
	limit, query, ok := pageSize(c)
	if !ok {
		return
	}
	p := c.MustGet("principal").(auth.Principal)
	cs := CursorScope{PrincipalID: p.ID(), TenantID: s.TenantID(), Collection: "operations", Direction: "forward"}
	var position CursorPosition
	if raw, exists := query["cursor"]; exists {
		v, e := a.config.Cursors.Decode(raw[0], cs, time.Now())
		if e != nil {
			fail(c, 400, "invalid_cursor", "Invalid or expired cursor", nil)
			return
		}
		position = v
	}
	rows, e := (repository.PublicOperations{Pool: a.config.Pool}).List(c.Request.Context(), s, position.CreatedAt, position.ID, limit+1, requestID(c))
	if e != nil {
		fail(c, 503, "unavailable", "Service temporarily unavailable", nil)
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
	c.JSON(200, struct {
		Items      []repository.PublicOperation `json:"items"`
		NextCursor string                       `json:"next_cursor,omitempty"`
	}{rows, next})
}

func (a *API) reviseRule(c *gin.Context) {
	s, ok := scope(c, policy.Mutate)
	if !ok {
		return
	}
	expected, ok := expectedRevision(c)
	if !ok {
		return
	}
	var body service.RuleInput
	if !decodeBody(c, &body) {
		return
	}
	routeID, e := uuid.Parse(c.Param("rule_id"))
	if e != nil || routeID == uuid.Nil || body.ID != routeID.String() {
		fail(c, 400, "invalid_request", "Invalid rule identifier", map[string]string{"id": "Must match route identifier"})
		return
	}
	result, e := (service.RuleService{Pool: a.config.Pool}).Mutate(c.Request.Context(), s, body, expected, requestID(c))
	if e != nil {
		switch {
		case errors.Is(e, compiler.RevisionConflict):
			fail(c, 412, "precondition_failed", "Resource revision changed", nil)
		case errors.Is(e, compiler.Invalid):
			fail(c, 400, "invalid_request", "Invalid rule", nil)
		case errors.Is(e, compiler.Unauthorized), errors.Is(e, policy.ErrForbidden):
			fail(c, 403, "forbidden", "Access denied", nil)
		case errors.Is(e, compiler.Unreachable):
			fail(c, 409, "rule_unavailable", "Rule path unavailable", nil)
		case errors.Is(e, pgx.ErrNoRows):
			fail(c, 404, "not_found", "Resource not found", nil)
		case errors.Is(e, service.ErrConflict):
			fail(c, 409, "conflict", "Resource state conflict", nil)
		default:
			fail(c, 503, "unavailable", "Service temporarily unavailable", nil)
		}
		return
	}
	c.Header("ETag", "\""+strconv.FormatInt(result.Revision.Revision, 10)+"\"")
	c.Header("Location", "/api/v1/tenants/"+s.TenantID()+"/operations/"+result.OperationID)
	c.JSON(202, struct {
		RuleID      string `json:"rule_id"`
		Revision    int64  `json:"revision"`
		OperationID string `json:"operation_id"`
		ChangeClass string `json:"change_class"`
		SyncStatus  string `json:"sync_status"`
	}{result.Revision.RuleID, result.Revision.Revision, result.OperationID, result.ChangeClass, "pending"})
}
