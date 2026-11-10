package httpapi

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"strconv"
	"unicode/utf8"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/service"
)

func (a *API) putPackageCatalog(c *gin.Context) {
	s, ok := scope(c, policy.Mutate)
	if !ok {
		return
	}
	if !s.Platform() || s.Role() != auth.PlatformAdmin {
		fail(c, 403, "forbidden", "Access denied", nil)
		return
	}
	if c.Request.URL.RawQuery != "" {
		fail(c, 400, "invalid_request", "Unexpected query parameters", nil)
		return
	}
	id := c.Param("package_id")
	u, e := uuid.Parse(id)
	if e != nil || u == uuid.Nil || u.String() != id {
		fail(c, 400, "invalid_request", "Invalid resource identifier", map[string]string{"package_id": "Invalid identifier"})
		return
	}
	expected, ok := expectedRevision(c)
	if !ok {
		return
	}
	var body struct {
		Name  string               `json:"name"`
		Terms service.PackageTerms `json:"terms"`
	}
	if !decodeBody(c, &body) {
		return
	}
	if !utf8.ValidString(body.Name) || utf8.RuneCountInString(body.Name) == 0 || utf8.RuneCountInString(body.Name) > 128 {
		fail(c, 400, "invalid_request", "Invalid package name", map[string]string{"name": "Must contain 1 to 128 characters"})
		return
	}
	terms, e := service.NormalizePackageTerms(body.Terms)
	if e != nil {
		fail(c, 400, "invalid_request", "Invalid package terms", map[string]string{"terms": "Invalid package terms"})
		return
	}
	result, e := (service.PackageService{Pool: a.config.Pool}).PutCatalogWithOperation(c.Request.Context(), s, id, body.Name, expected, terms, requestID(c))
	if e != nil {
		mutationError(c, e)
		return
	}
	c.Header("ETag", `"`+strconv.FormatInt(result.Version, 10)+`"`)
	c.Header("Location", "/api/v1/tenants/"+s.TenantID()+"/operations/"+result.OperationID)
	c.JSON(202, struct {
		PackageID   string `json:"package_id"`
		Version     int64  `json:"version"`
		OperationID string `json:"operation_id"`
		SyncStatus  string `json:"sync_status"`
	}{result.PackageID, result.Version, result.OperationID, "pending"})
}
