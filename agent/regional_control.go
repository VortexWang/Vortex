package agent

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"time"
	"unicode/utf8"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	controlv1 "vortex/contracts/protobuf/v1"
)

// RegionalConfig timings are configurable development defaults. Endpoints are
// operator supplied; a status advertisement can never add a route or issuer.
type RegionalConfig struct {
	Region                                string
	Endpoints                             []HubEndpoint
	DialTimeout, Watchdog, FailoverBudget time.Duration
	RetryMin, RetryMax                    time.Duration
	onlineOnly                            bool
}

func (r RegionalConfig) validated() (RegionalConfig, error) {
	if r.DialTimeout == 0 {
		r.DialTimeout = 5 * time.Second
	}
	if r.Watchdog == 0 {
		r.Watchdog = 15 * time.Second
	}
	if r.FailoverBudget == 0 {
		r.FailoverBudget = 60 * time.Second
	}
	if r.RetryMin == 0 {
		r.RetryMin = time.Second
	}
	if r.RetryMax == 0 {
		r.RetryMax = 30 * time.Second
	}
	if r.Region == "" || len(r.Region) > 128 || !utf8.ValidString(r.Region) || r.DialTimeout <= 0 || r.DialTimeout > 60*time.Second || r.Watchdog <= 0 || r.Watchdog > 120*time.Second || r.FailoverBudget < r.DialTimeout || r.FailoverBudget > 300*time.Second || r.RetryMin <= 0 || r.RetryMax < r.RetryMin || r.RetryMax > 30*time.Second {
		return r, fmt.Errorf("agent: invalid regional configuration")
	}
	var selected []HubEndpoint
	ids, addresses := map[string]bool{}, map[string]bool{}
	for _, e := range r.Endpoints {
		if e.Region != r.Region {
			continue
		}
		if !canonicalUUID(e.HubID) || e.ServerName == "" || len(e.ServerName) > 253 || len(e.Address) > 253 || ids[e.HubID] || addresses[e.Address] {
			return r, fmt.Errorf("agent: invalid or duplicate configured Hub")
		}
		if _, _, err := net.SplitHostPort(e.Address); err != nil {
			return r, fmt.Errorf("agent: invalid Hub address: %w", err)
		}
		ids[e.HubID] = true
		addresses[e.Address] = true
		selected = append(selected, e)
	}
	if len(selected) == 0 || len(selected) > 16 {
		return r, fmt.Errorf("agent: no bounded same-region Hub list")
	}
	r.Endpoints = selected
	return r, nil
}

// RunRegion owns one joined control attempt at a time. A failed/stalled peer
// cannot hold a later candidate indefinitely. Existing Engine sessions are not
// stopped and no authority time/balance is changed by switching transport.
func (c *ControlClient) RunRegion(ctx context.Context, config RegionalConfig) error {
	if c == nil || c.Node == nil || (c.TLS == nil && c.TLSProvider == nil) {
		return fmt.Errorf("agent: incomplete regional client")
	}
	r, err := config.validated()
	if err != nil {
		return err
	}
	delay := r.RetryMin
	for ctx.Err() == nil {
		started := time.Now()
		for _, onlineOnly := range []bool{true, false} {
			for _, endpoint := range r.Endpoints {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				remaining := r.FailoverBudget - time.Since(started)
				if remaining <= 0 {
					break
				}
				attempt := r
				attempt.onlineOnly = onlineOnly
				if attempt.DialTimeout > remaining {
					attempt.DialTimeout = remaining
				}
				readyBefore := c.regionalReady.Load()
				attemptStarted := time.Now()
				_ = c.connect(ctx, endpoint, &attempt)
				// Long-lived healthy sessions reset the retry backoff. All goroutines in
				// connect have been joined before another incarnation can be selected.
				if c.regionalReady.Load() > readyBefore && time.Since(attemptStarted) >= r.Watchdog {
					delay = r.RetryMin
					// Healthy stream lifetime is outside the candidate sweep budget. A
					// disconnect after hours must still reach the next endpoint immediately.
					started = time.Now()
				}
			}
		}
		wait := regionalJitter(delay)
		if wait < r.RetryMin {
			wait = r.RetryMin
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if delay < r.RetryMax {
			delay *= 2
			if delay > r.RetryMax {
				delay = r.RetryMax
			}
		}
	}
	return ctx.Err()
}

func regionalJitter(d time.Duration) time.Duration {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return d
	}
	// Equal jitter never exceeds the configured retry cap, and avoids zero spin.
	return d/2 + time.Duration(binary.LittleEndian.Uint64(b[:])%uint64(d-d/2+1))
}

func (c *ControlClient) beginSession(parent context.Context) (context.Context, context.CancelFunc, uint64) {
	// Cancel a blocked sender before waiting for its mutation boundary. A
	// separate begin lock serializes competing ownership switches.
	c.beginMu.Lock()
	defer c.beginMu.Unlock()
	if c.sessionCancel != nil {
		c.sessionCancel()
	}
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	ctx, cancel := context.WithCancel(parent)
	c.sessionID++
	c.sessionCancel = cancel
	c.currentStatus = nil
	return ctx, cancel, c.sessionID
}

// CurrentHubStatus is observational only; consumers must still validate actual
// grants against their persisted scope/deadline. No tokens or payloads exposed.
func (c *ControlClient) CurrentHubStatus() *controlv1.HubStatus {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	if c.currentStatus == nil {
		return nil
	}
	return proto.Clone(c.currentStatus).(*controlv1.HubStatus)
}

func noUnknownFields(m protoreflect.Message) bool {
	if len(m.GetUnknown()) != 0 {
		return false
	}
	valid := true
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.IsList() && fd.Message() != nil {
			for i := 0; i < v.List().Len(); i++ {
				if !noUnknownFields(v.List().Get(i).Message()) {
					valid = false
					return false
				}
			}
		} else if fd.Message() != nil && !fd.IsMap() {
			valid = noUnknownFields(v.Message())
		}
		return valid
	})
	return valid
}

func validateRegionalStatus(s *controlv1.HubStatus, e HubEndpoint, r *RegionalConfig, session, issuer string, now time.Time) error {
	if s == nil || !noUnknownFields(s.ProtoReflect()) || !canonicalUUID(s.HubId) || s.HubId != e.HubID || issuer != e.ServerName || s.SessionId != session || s.RegionId != r.Region || s.ControlVersion != "1.3.0" || len(s.Capabilities) != 1 || s.Capabilities[0] != "status-v1" {
		return fmt.Errorf("agent: invalid authenticated regional status")
	}
	for _, n := range []uint64{s.SignerEpoch, s.RecoveryTargetSequence, s.ConfirmedSequence, s.PendingTailRecords, s.RevocationRevision, s.RestrictionRevision} {
		if n > math.MaxInt64 {
			return fmt.Errorf("agent: status counter overflow")
		}
	}
	if s.Mode != "online" && s.Mode != "autonomous" && s.Mode != "recovering" {
		return fmt.Errorf("agent: Hub is not ready")
	}
	heartbeat, err := time.Parse(time.RFC3339Nano, s.BackendHeartbeatAt)
	if err != nil || heartbeat.IsZero() || heartbeat.Location() != time.UTC || heartbeat.After(now) {
		return fmt.Errorf("agent: invalid backend bound")
	}
	deadline, err := time.Parse(time.RFC3339Nano, s.AutonomyDeadline)
	if err != nil || deadline.Location() != time.UTC || !deadline.After(heartbeat) || deadline.After(heartbeat.Add(12*time.Hour)) || !now.Before(deadline) || s.SignerEpoch == 0 {
		return fmt.Errorf("agent: invalid original autonomy deadline")
	}
	if s.Mode == "online" && !now.Before(heartbeat.Add(30*time.Second)) {
		return fmt.Errorf("agent: stale online advertisement")
	}
	if r.onlineOnly && s.Mode != "online" {
		return fmt.Errorf("agent: prefer an online same-region Hub")
	}
	return nil
}

func (c *ControlClient) withSession(ctx context.Context, id uint64, fn func() error) error {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	if c.sessionID != id || ctx.Err() != nil {
		return fmt.Errorf("agent: stale control session")
	}
	return fn()
}

// RegionalConfigFromRuntime consumes the approved runtime envelope, not an
// Engine configuration. It refuses ambiguous JSON and unapproved extra keys.
func RegionalConfigFromRuntime(payload []byte, nodeID string) (RegionalConfig, error) {
	var envelope struct {
		Version   uint64 `json:"version"`
		Lifecycle *struct {
			Region struct {
				ID   string `json:"region_id"`
				Hubs []struct {
					ID         string `json:"hub_id"`
					Endpoint   string `json:"endpoint"`
					ServerName string `json:"server_name"`
				} `json:"hubs"`
			} `json:"region"`
			Dial     int `json:"dial_timeout_seconds"`
			Watchdog int `json:"watchdog_seconds"`
			Budget   int `json:"failover_budget_seconds"`
			RetryMin int `json:"retry_min_ms"`
			RetryMax int `json:"retry_max_ms"`
		} `json:"lifecycle"`
	}
	if len(payload) == 0 || len(payload) > 1<<20 {
		return RegionalConfig{}, fmt.Errorf("agent: runtime configuration exceeds bound")
	}
	if err := rejectDuplicateJSONKeys(payload); err != nil {
		return RegionalConfig{}, err
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return RegionalConfig{}, err
	}
	if err := validateRuntimeConfig(payload, nodeID, envelope.Version); err != nil {
		return RegionalConfig{}, err
	}
	if envelope.Lifecycle == nil {
		return RegionalConfig{}, fmt.Errorf("agent: runtime lifecycle configuration required")
	}
	v := envelope.Lifecycle
	r := RegionalConfig{Region: v.Region.ID, DialTimeout: time.Duration(v.Dial) * time.Second, Watchdog: time.Duration(v.Watchdog) * time.Second, FailoverBudget: time.Duration(v.Budget) * time.Second, RetryMin: time.Duration(v.RetryMin) * time.Millisecond, RetryMax: time.Duration(v.RetryMax) * time.Millisecond}
	for _, e := range v.Region.Hubs {
		r.Endpoints = append(r.Endpoints, HubEndpoint{Region: r.Region, Address: e.Endpoint, HubID: e.ID, ServerName: e.ServerName})
	}
	return r.validated()
}

// RegionalCounters are aggregate values, without tenant/rule/address/session
// metric labels. Current peer identity and original bounds live separately in
// CurrentHubStatus and cannot confer financial authority.
type RegionalCounters struct {
	Attempts, ReadySessions, ReadinessTimeouts, WatchdogTimeouts uint64
}

func (c *ControlClient) RegionalCounters() RegionalCounters {
	return RegionalCounters{Attempts: c.regionalAttempts.Load(), ReadySessions: c.regionalReady.Load(), ReadinessTimeouts: c.regionalReadinessTimeouts.Load(), WatchdogTimeouts: c.regionalWatchdogTimeouts.Load()}
}
