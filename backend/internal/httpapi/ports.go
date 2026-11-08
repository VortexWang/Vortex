package httpapi

import (
	"encoding/hex"
	"errors"
	"net/netip"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	"vortex/backend/internal/service"
)

// Public port DTOs enumerate business fields. Generated allocations include
// control/listen details and must never be serialized directly.
type portSummary struct {
	ID                string   `json:"id"`
	RuleID            string   `json:"rule_id"`
	GroupID           string   `json:"group_id"`
	Port              int32    `json:"port"`
	Protocols         []string `json:"protocols"`
	State             string   `json:"state"`
	Version           int64    `json:"version"`
	MemberSetVersion  string   `json:"member_set_version"`
	NeedsRevalidation bool     `json:"needs_revalidation"`
}

func publicPort(a generated.PortAllocation) portSummary {
	return portSummary{a.ID, a.RuleID, a.GroupID, a.Port, a.Protocols, a.State, a.Version, a.MemberSetVersion, a.NeedsRevalidation}
}
func portError(c *gin.Context, e error) {
	var conflict *service.PortConflictError
	if errors.As(e, &conflict) {
		var fields map[string]string
		if conflict.SuggestedPort > 0 {
			fields = map[string]string{"suggested_port": strconv.FormatInt(int64(conflict.SuggestedPort), 10)}
		}
		fail(c, 409, "port_unavailable", "Requested port unavailable", fields)
		return
	}
	mutationError(c, e)
}
func (a *API) preparePort(c *gin.Context) {
	s, ok := scope(c, policy.Mutate)
	if !ok {
		return
	}
	if c.Request.URL.RawQuery != "" {
		fail(c, 400, "invalid_request", "Unexpected query parameters", nil)
		return
	}
	var body struct {
		RuleID           string   `json:"rule_id"`
		GroupID          string   `json:"group_id"`
		MemberSetVersion string   `json:"member_set_version"`
		ListenAddress    string   `json:"listen_address"`
		Port             int32    `json:"port"`
		Protocols        []string `json:"protocols"`
	}
	if !decodeBody(c, &body) {
		return
	}
	digest, e := hex.DecodeString(body.MemberSetVersion)
	addressOK := body.ListenAddress == "*"
	if ip, err := netip.ParseAddr(body.ListenAddress); err == nil {
		addressOK = ip.Zone() == "" && ip.String() == body.ListenAddress
	}
	if !canonicalResource(body.RuleID) || !canonicalResource(body.GroupID) || e != nil || len(digest) != 32 || strings.ToLower(body.MemberSetVersion) != body.MemberSetVersion || !addressOK || body.Port < 0 || body.Port > 65535 || len(body.Protocols) < 1 || len(body.Protocols) > 2 {
		fail(c, 400, "invalid_request", "Invalid port reservation", nil)
		return
	}
	seen := map[string]bool{}
	for _, p := range body.Protocols {
		if (p != "tcp" && p != "udp") || seen[p] {
			fail(c, 400, "invalid_request", "Invalid protocols", nil)
			return
		}
		seen[p] = true
	}
	result, e := (service.PortService{Pool: a.config.Pool}).PreparePortReservation(c.Request.Context(), s, service.PortRequest{RuleID: body.RuleID, GroupID: body.GroupID, MemberSetVersion: body.MemberSetVersion, ListenAddress: body.ListenAddress, Port: body.Port, Protocols: body.Protocols, Purpose: "service"}, requestID(c))
	if e != nil {
		portError(c, e)
		return
	}
	c.Header("ETag", `"`+strconv.FormatInt(result.Version, 10)+`"`)
	c.Header("Location", "/api/v1/tenants/"+s.TenantID()+"/operations/"+result.OperationID)
	c.JSON(202, struct {
		portSummary
		OperationID           string `json:"operation_id"`
		SyncStatus            string `json:"sync_status"`
		EntryAddressMayChange bool   `json:"entry_address_may_change"`
	}{publicPort(result.PortAllocation), result.OperationID, "pending", result.EntryAddressMayChange})
}
func (a *API) port(c *gin.Context) {
	s, ok := scope(c, policy.Read)
	if !ok {
		return
	}
	id := c.Param("allocation_id")
	if !canonicalResource(id) || c.Request.URL.RawQuery != "" {
		fail(c, 400, "invalid_request", "Invalid port allocation request", nil)
		return
	}
	result, e := (repository.PortRepository{Pool: a.config.Pool}).Allocation(c.Request.Context(), s, id, requestID(c))
	if e != nil {
		readError(c, e)
		return
	}
	c.Header("ETag", `"`+strconv.FormatInt(result.Version, 10)+`"`)
	c.JSON(200, publicPort(result))
}
