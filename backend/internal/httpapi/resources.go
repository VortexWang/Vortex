package httpapi

import (
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"net/http"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	"vortex/backend/internal/service"
)

func readError(c *gin.Context, e error) {
	if errors.Is(e, pgx.ErrNoRows) {
		fail(c, 404, "not_found", "Resource not found", nil)
	} else {
		fail(c, 503, "unavailable", "Service temporarily unavailable", nil)
	}
}
func (a *API) resource(kind, param string) gin.HandlerFunc {
	return func(c *gin.Context) {
		s, ok := scope(c, policy.Read)
		if !ok {
			return
		}
		if c.Request.URL.RawQuery != "" {
			fail(c, 400, "invalid_request", "Unexpected query parameters", nil)
			return
		}
		id := ""
		if param != "" {
			id = c.Param(param)
			u, e := uuid.Parse(id)
			if e != nil || u == uuid.Nil || id != u.String() {
				fail(c, 400, "invalid_request", "Invalid resource identifier", map[string]string{param: "Invalid identifier"})
				return
			}
		}
		if kind == "billing_explanation" {
			v, e := (service.LedgerService{Pool: a.config.Pool}).BillingExplanation(c.Request.Context(), s, id, requestID(c))
			if e != nil {
				readError(c, e)
				return
			}
			c.JSON(200, v)
			return
		}
		body, e := (repository.PublicResources{Pool: a.config.Pool}).Get(c.Request.Context(), s, kind, id, requestID(c))
		if e != nil {
			readError(c, e)
			return
		}
		c.Data(http.StatusOK, "application/json; charset=utf-8", body)
	}
}
func (a *API) registerResourceReads(secured *gin.RouterGroup) {
	base := "/tenants/:tenant_id"
	secured.GET(base, a.resource("tenant", ""))
	secured.GET(base+"/nodes/:node_id", a.resource("node", "node_id"))
	secured.GET(base+"/node-groups/:group_id", a.resource("node_group", "group_id"))
	secured.GET(base+"/subscription", a.resource("subscription", ""))
	secured.GET(base+"/packages/:package_id", a.resource("package", "package_id"))
	secured.GET(base+"/rules/:rule_id", a.resource("rule", "rule_id"))
	secured.GET(base+"/sources/:source_id/ledger", a.resource("source_ledger", "source_id"))
	secured.GET(base+"/limits", a.resource("limits", ""))
	secured.GET(base+"/billing-snapshots/:snapshot_id/explanation", a.resource("billing_explanation", "snapshot_id"))
}
