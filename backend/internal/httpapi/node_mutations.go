package httpapi

import (
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"strconv"
	"strings"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/service"
)

func expectedRevision(c *gin.Context) (int64, bool) {
	headers := c.Request.Header.Values("If-Match")
	if len(headers) == 0 {
		fail(c, 428, "precondition_required", "If-Match required", nil)
		return 0, false
	}
	if len(headers) != 1 {
		fail(c, 400, "invalid_request", "Invalid If-Match", nil)
		return 0, false
	}
	tag := headers[0]
	if len(tag) < 3 || !strings.HasPrefix(tag, `"`) || !strings.HasSuffix(tag, `"`) {
		fail(c, 400, "invalid_request", "Invalid If-Match", nil)
		return 0, false
	}
	digits := tag[1 : len(tag)-1]
	expected, e := strconv.ParseInt(digits, 10, 64)
	if e != nil || expected < 0 || strconv.FormatInt(expected, 10) != digits {
		fail(c, 400, "invalid_request", "Invalid If-Match", nil)
		return 0, false
	}
	return expected, true
}
func mutationError(c *gin.Context, e error) {
	switch {
	case errors.Is(e, service.ErrPrecondition):
		fail(c, 412, "precondition_failed", "Resource revision changed", nil)
	case errors.Is(e, policy.ErrForbidden):
		fail(c, 403, "forbidden", "Access denied", nil)
	case errors.Is(e, pgx.ErrNoRows):
		fail(c, 404, "not_found", "Resource not found", nil)
	case errors.Is(e, service.ErrConflict):
		fail(c, 409, "conflict", "Resource state conflict", nil)
	default:
		fail(c, 503, "unavailable", "Service temporarily unavailable", nil)
	}
}
func (a *API) transitionNode(c *gin.Context) {
	s, ok := scope(c, policy.Mutate)
	if !ok {
		return
	}
	if c.Request.URL.RawQuery != "" {
		fail(c, 400, "invalid_request", "Unexpected query parameters", nil)
		return
	}
	node := c.Param("node_id")
	id, e := uuid.Parse(node)
	if e != nil || id == uuid.Nil || id.String() != node {
		fail(c, 400, "invalid_request", "Invalid resource identifier", map[string]string{"node_id": "Invalid identifier"})
		return
	}
	expected, ok := expectedRevision(c)
	if !ok {
		return
	}
	var body struct {
		Lifecycle    string `json:"lifecycle"`
		DrainSeconds *int   `json:"drain_seconds,omitempty"`
	}
	if !decodeBody(c, &body) {
		return
	}
	switch body.Lifecycle {
	case "pending", "online", "draining", "offline", "disabled":
	default:
		fail(c, 400, "invalid_request", "Invalid lifecycle", map[string]string{"lifecycle": "Invalid lifecycle"})
		return
	}
	drain := 0
	if body.DrainSeconds != nil {
		if body.Lifecycle != "draining" || *body.DrainSeconds < 1 || *body.DrainSeconds > 86400 {
			fail(c, 400, "invalid_request", "Invalid drain duration", map[string]string{"drain_seconds": "Allowed only for draining; must be between 1 and 86400"})
			return
		}
		drain = *body.DrainSeconds
	}
	result, e := (service.NodeService{Pool: a.config.Pool}).TransitionWithOperation(c.Request.Context(), s, node, body.Lifecycle, expected, drain, requestID(c))
	if e != nil {
		mutationError(c, e)
		return
	}
	c.Header("ETag", `"`+strconv.FormatInt(result.Version, 10)+`"`)
	c.Header("Location", "/api/v1/tenants/"+s.TenantID()+"/operations/"+result.OperationID)
	c.JSON(202, struct {
		NodeID           string `json:"node_id"`
		OperationID      string `json:"operation_id"`
		DesiredLifecycle string `json:"desired_lifecycle"`
		Version          int64  `json:"version"`
		SyncStatus       string `json:"sync_status"`
	}{result.NodeID, result.OperationID, result.DesiredLifecycle, result.Version, "pending"})
}
