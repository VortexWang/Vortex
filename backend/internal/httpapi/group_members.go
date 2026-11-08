package httpapi

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/service"
)

func (a *API) replaceGroupMembers(c *gin.Context) {
	s, ok := scope(c, policy.Mutate)
	if !ok {
		return
	}
	if c.Request.URL.RawQuery != "" {
		fail(c, 400, "invalid_request", "Unexpected query parameters", nil)
		return
	}
	group := c.Param("group_id")
	if !canonicalResource(group) {
		fail(c, 400, "invalid_request", "Invalid group identifier", nil)
		return
	}
	expected, ok := expectedRevision(c)
	if !ok {
		return
	}
	if expected < 1 {
		fail(c, 400, "invalid_request", "Positive group revision required", nil)
		return
	}
	var body struct {
		Members []struct {
			NodeID   string `json:"node_id"`
			Weight   *int32 `json:"weight"`
			Priority *int32 `json:"priority"`
		} `json:"members"`
	}
	if !decodeBodyLimit(c, &body, 128*1024) {
		return
	}
	if body.Members == nil || len(body.Members) > 1024 {
		fail(c, 400, "invalid_request", "Explicit member list required", map[string]string{"members": "At most 1024 unique members required"})
		return
	}
	members := make([]service.GroupMember, 0, len(body.Members))
	seen := make(map[string]bool, len(body.Members))
	for _, member := range body.Members {
		if !canonicalResource(member.NodeID) || seen[member.NodeID] || member.Weight == nil || *member.Weight < 1 || *member.Weight > 10000 || member.Priority == nil || *member.Priority < 0 {
			fail(c, 400, "invalid_request", "Invalid group member", map[string]string{"members": "Unique node identifiers, weight 1 to 10000 and nonnegative priority required"})
			return
		}
		seen[member.NodeID] = true
		members = append(members, service.GroupMember{NodeID: member.NodeID, Weight: *member.Weight, Priority: *member.Priority})
	}
	result, e := (service.NodeService{Pool: a.config.Pool}).SetMembersWithOperation(c.Request.Context(), s, group, expected, members, requestID(c))
	if e != nil {
		mutationError(c, e)
		return
	}
	c.Header("ETag", `"`+strconv.FormatInt(result.Version, 10)+`"`)
	c.Header("Location", "/api/v1/tenants/"+s.TenantID()+"/operations/"+result.OperationID)
	c.JSON(202, struct {
		GroupID       string `json:"group_id"`
		OperationID   string `json:"operation_id"`
		Version       int64  `json:"version"`
		MemberVersion int64  `json:"member_version"`
		SyncStatus    string `json:"sync_status"`
	}{result.GroupID, result.OperationID, result.Version, result.MemberVersion, "pending"})
}
