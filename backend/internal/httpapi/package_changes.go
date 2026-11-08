package httpapi

import (
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/service"
)

type packageImpactInput struct {
	KeepRules   []string  `json:"keep_rules"`
	EffectiveAt time.Time `json:"effective_at"`
}
type packageChangeInput struct {
	packageImpactInput
	PackageID      string `json:"package_id"`
	PackageVersion int64  `json:"package_version"`
}
type packageChangeConfirmation struct {
	packageChangeInput
	Digest string `json:"digest"`
}
type entitlementChangeInput struct {
	packageImpactInput
	Code string `json:"code"`
}
type entitlementChangeConfirmation struct {
	entitlementChangeInput
	Digest string `json:"digest"`
}

func packageRequest(c *gin.Context) (service.TenantScope, bool) {
	s, ok := scope(c, policy.Mutate)
	if !ok {
		return s, false
	}
	if s.Platform() {
		fail(c, 403, "forbidden", "Access denied", nil)
		return s, false
	}
	if c.Request.URL.RawQuery != "" {
		fail(c, 400, "invalid_request", "Unexpected query parameters", nil)
		return s, false
	}
	return s, true
}
func canonicalResource(id string) bool {
	u, e := uuid.Parse(id)
	return e == nil && u != uuid.Nil && u.String() == id
}
func validImpact(c *gin.Context, b packageImpactInput) bool {
	if b.KeepRules == nil || len(b.KeepRules) > 10000 || b.EffectiveAt.IsZero() {
		fail(c, 400, "invalid_request", "Explicit rules and effective time required", nil)
		return false
	}
	seen := make(map[string]bool, len(b.KeepRules))
	for _, id := range b.KeepRules {
		if !canonicalResource(id) || seen[id] {
			fail(c, 400, "invalid_request", "Invalid kept rules", map[string]string{"keep_rules": "At most 10000 unique resource identifiers required"})
			return false
		}
		seen[id] = true
	}
	return true
}
func validPackageChange(c *gin.Context, b packageChangeInput) bool {
	if !canonicalResource(b.PackageID) || b.PackageVersion < 1 {
		fail(c, 400, "invalid_request", "Invalid package version", nil)
		return false
	}
	return validImpact(c, b.packageImpactInput)
}
func validCode(c *gin.Context, code string) bool {
	if len(code) < 16 || len(code) > 256 || strings.TrimSpace(code) != code {
		fail(c, 400, "invalid_request", "Invalid entitlement code", map[string]string{"code": "Must contain 16 to 256 bytes without surrounding whitespace"})
		return false
	}
	return true
}
func validDigest(c *gin.Context, d string) bool {
	b, e := hex.DecodeString(d)
	if e != nil || len(b) != 32 || hex.EncodeToString(b) != d {
		fail(c, 400, "invalid_request", "Invalid preview digest", map[string]string{"digest": "Lowercase SHA-256 required"})
		return false
	}
	return true
}
func packageKey(c *gin.Context) (string, bool) {
	values := c.Request.Header.Values("Idempotency-Key")
	if len(values) == 1 && len(values[0]) >= 1 && len(values[0]) <= 128 {
		valid := true
		for _, b := range []byte(values[0]) {
			if b < 0x21 || b > 0x7e {
				valid = false
				break
			}
		}
		if valid {
			return values[0], true
		}
	}
	fail(c, 400, "invalid_request", "Idempotency-Key required", map[string]string{"Idempotency-Key": "One value with 1 to 128 printable ASCII bytes and no spaces required"})
	return "", false
}
func packageExpectedRevision(c *gin.Context) (int64, bool) {
	v, ok := expectedRevision(c)
	if !ok {
		return 0, false
	}
	if v < 1 {
		fail(c, 400, "invalid_request", "Positive subscription revision required", nil)
		return 0, false
	}
	return v, true
}
func packageError(c *gin.Context, e error) {
	switch {
	case errors.Is(e, service.ErrEntitlementInvalid):
		fail(c, 409, "entitlement_not_applicable", "Entitlement cannot be applied", nil)
	case errors.Is(e, service.ErrPackageConfirmationRequired):
		fail(c, 409, "package_confirmation_required", "Preview and confirmation required", nil)
	default:
		mutationError(c, e)
	}
}
func packageAccepted(c *gin.Context, s service.TenantScope, result service.PackageResult) {
	if result.Version > 0 {
		c.Header("ETag", `"`+strconv.FormatInt(result.Version, 10)+`"`)
	}
	c.Header("Location", "/api/v1/tenants/"+s.TenantID()+"/operations/"+result.OperationID)
	c.JSON(202, struct {
		service.PackageResult
		SyncStatus string `json:"sync_status"`
	}{result, "pending"})
}
func (a *API) previewPackageChange(c *gin.Context) {
	s, ok := packageRequest(c)
	if !ok {
		return
	}
	var body packageChangeInput
	if !decodeBodyLimit(c, &body, 512*1024) || !validPackageChange(c, body) {
		return
	}
	result, e := (service.PackageService{Pool: a.config.Pool}).PreviewDowngrade(c.Request.Context(), s, body.PackageID, body.PackageVersion, body.KeepRules, body.EffectiveAt)
	if e != nil {
		packageError(c, e)
		return
	}
	c.Data(200, "application/json; charset=utf-8", result)
}
func (a *API) confirmPackageChange(c *gin.Context) {
	s, ok := packageRequest(c)
	if !ok {
		return
	}
	expected, ok := packageExpectedRevision(c)
	if !ok {
		return
	}
	key, ok := packageKey(c)
	if !ok {
		return
	}
	var body packageChangeConfirmation
	if !decodeBodyLimit(c, &body, 512*1024) || !validPackageChange(c, body.packageChangeInput) || !validDigest(c, body.Digest) {
		return
	}
	result, e := (service.PackageService{Pool: a.config.Pool}).ConfirmDowngradeAtVersion(c.Request.Context(), s, body.PackageID, body.PackageVersion, expected, body.KeepRules, body.EffectiveAt, body.Digest, key, requestID(c))
	if e != nil {
		packageError(c, e)
		return
	}
	packageAccepted(c, s, result)
}
func (a *API) previewEntitlementChange(c *gin.Context) {
	s, ok := packageRequest(c)
	if !ok {
		return
	}
	var body entitlementChangeInput
	if !decodeBodyLimit(c, &body, 512*1024) || !validCode(c, body.Code) || !validImpact(c, body.packageImpactInput) {
		return
	}
	result, e := (service.PackageService{Pool: a.config.Pool}).PreviewEntitlement(c.Request.Context(), s, body.Code, body.KeepRules, body.EffectiveAt)
	if e != nil {
		packageError(c, e)
		return
	}
	c.Data(200, "application/json; charset=utf-8", result)
}
func (a *API) confirmEntitlementChange(c *gin.Context) {
	s, ok := packageRequest(c)
	if !ok {
		return
	}
	expected, ok := packageExpectedRevision(c)
	if !ok {
		return
	}
	key, ok := packageKey(c)
	if !ok {
		return
	}
	var body entitlementChangeConfirmation
	if !decodeBodyLimit(c, &body, 512*1024) || !validCode(c, body.Code) || !validImpact(c, body.packageImpactInput) || !validDigest(c, body.Digest) {
		return
	}
	result, e := (service.PackageService{Pool: a.config.Pool}).ConfirmEntitlementAtVersion(c.Request.Context(), s, body.Code, expected, body.KeepRules, body.EffectiveAt, body.Digest, key, requestID(c))
	if e != nil {
		packageError(c, e)
		return
	}
	packageAccepted(c, s, result)
}
func (a *API) redeemEntitlement(c *gin.Context) {
	s, ok := scope(c, policy.Mutate)
	if !ok {
		return
	}
	if c.Request.URL.RawQuery != "" {
		fail(c, 400, "invalid_request", "Unexpected query parameters", nil)
		return
	}
	key, ok := packageKey(c)
	if !ok {
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if !decodeBody(c, &body) || !validCode(c, body.Code) {
		return
	}
	result, e := (service.PackageService{Pool: a.config.Pool}).RedeemEntitlement(c.Request.Context(), s, body.Code, key, requestID(c))
	if e != nil {
		packageError(c, e)
		return
	}
	packageAccepted(c, s, result)
}
