package httpapi

import (
	"errors"
	"sort"

	"github.com/gin-gonic/gin"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	"vortex/backend/internal/service"
)

func (a *API) serving(c *gin.Context) {
	s, ok := scope(c, policy.Read)
	if !ok {
		return
	}
	group := c.Param("group_id")
	if !canonicalResource(group) || c.Request.URL.RawQuery != "" {
		fail(c, 400, "invalid_request", "Invalid serving request", nil)
		return
	}
	rows, e := (repository.PortRepository{Pool: a.config.Pool}).Serving(c.Request.Context(), s, group, requestID(c))
	if e != nil {
		mutationError(c, e)
		return
	}
	// These are business entry addresses, deduplicated across eligible members.
	// Node incarnation, credential and per-task records never enter the response.
	set := map[string]bool{}
	for _, r := range rows {
		set[r.PublicAddress] = true
	}
	addresses := make([]string, 0, len(set))
	for address := range set {
		addresses = append(addresses, address)
	}
	sort.Strings(addresses)
	c.JSON(200, struct {
		Addresses []string `json:"addresses"`
	}{addresses})
}
func (a *API) recomputeDNS(c *gin.Context) {
	s, ok := scope(c, policy.Mutate)
	if !ok {
		return
	}
	group := c.Param("group_id")
	if !canonicalResource(group) || c.Request.URL.RawQuery != "" {
		fail(c, 400, "invalid_request", "Invalid DNS request", nil)
		return
	}
	key, ok := packageKey(c)
	if !ok {
		return
	}
	var body struct {
		AccessID string `json:"access_id"`
		Name     string `json:"name"`
	}
	if !decodeBody(c, &body) {
		return
	}
	if !canonicalResource(body.AccessID) || !service.ValidEntryDomain(body.Name) {
		fail(c, 400, "invalid_request", "Invalid entry domain", nil)
		return
	}
	result, e := (service.ServingService{Pool: a.config.Pool}).RecomputeDNS(c.Request.Context(), s, group, body.AccessID, body.Name, key, requestID(c))
	if e != nil {
		if errors.Is(e, repository.ErrDNSOperation) {
			fail(c, 409, "conflict", "Idempotency key conflict", nil)
		} else {
			mutationError(c, e)
		}
		return
	}
	c.Header("Location", "/api/v1/tenants/"+s.TenantID()+"/operations/"+result.ID)
	c.JSON(202, struct {
		OperationID string `json:"operation_id"`
		State       string `json:"state"`
	}{result.ID, result.State})
}
