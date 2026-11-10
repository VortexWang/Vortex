package httpapi

import (
	"github.com/gin-gonic/gin"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	"vortex/backend/internal/service"
)

func (a *API) recovery(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		s, ok := scope(c, policy.Read)
		if !ok {
			return
		}
		if !s.Platform() {
			fail(c, 403, "forbidden", "Access denied", nil)
			return
		}
		id := c.Param("recovery_id")
		if !canonicalResource(id) || c.Request.URL.RawQuery != "" {
			fail(c, 400, "invalid_request", "Invalid recovery request", nil)
			return
		}
		body, e := (repository.PublicResources{Pool: a.config.Pool}).Recovery(c.Request.Context(), s, kind, id, requestID(c))
		if e != nil {
			readError(c, e)
			return
		}
		c.Data(200, "application/json; charset=utf-8", body)
	}
}

func (a *API) advanceRecovery(c *gin.Context) {
	s, ok := scope(c, policy.Grant)
	if !ok {
		return
	}
	if !s.Platform() {
		fail(c, 403, "forbidden", "Access denied", nil)
		return
	}
	id := c.Param("recovery_id")
	if !canonicalResource(id) || c.Request.URL.RawQuery != "" {
		fail(c, 400, "invalid_request", "Invalid recovery request", nil)
		return
	}
	key, ok := packageKey(c)
	if !ok {
		return
	}
	var body *struct{}
	if !decodeBody(c, &body) {
		return
	}
	if body == nil {
		fail(c, 400, "invalid_request", "Empty JSON object required", nil)
		return
	}
	result, e := (service.ShareService{Pool: a.config.Pool}).RequestRecoveryAdvance(c.Request.Context(), s, id, key, requestID(c))
	if e != nil {
		mutationError(c, e)
		return
	}
	c.Header("Location", "/api/v1/tenants/"+s.TenantID()+"/operations/"+result.ID)
	c.JSON(202, struct {
		RecoveryID  string `json:"recovery_id"`
		OperationID string `json:"operation_id"`
		State       string `json:"state"`
	}{id, result.ID, result.State})
}
