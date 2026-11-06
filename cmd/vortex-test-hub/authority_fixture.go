package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"google.golang.org/protobuf/proto"
	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/hub"
)

// Explicit protocol-backend fixture, never production allocation authority.
// Delegation.BootID is bound once by the authenticated initial heartbeat.
// Persistent Store identity rejects a different boot using the same delegation.
type authorityFixture struct {
	Delegation       store.HubDelegation `json:"delegation"`
	InitialRequestID string              `json:"initial_request_id"`
	InitialBytes     uint64              `json:"initial_bytes"`
	TTLSeconds       uint64              `json:"ttl_seconds"`
}

func loadAuthorityFixture(path string) (authorityFixture, error) {
	var c authorityFixture
	f, err := os.Open(path)
	if err != nil {
		return c, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil {
		return c, err
	}
	if len(raw) > 1<<20 {
		return c, fmt.Errorf("authority fixture exceeds 1 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&c); err != nil {
		return c, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return c, fmt.Errorf("authority fixture requires one JSON object")
	}
	if c.Delegation.BootID != "" || c.Delegation.NodeID == "" || c.InitialRequestID == "" || c.InitialBytes == 0 || c.InitialBytes > c.Delegation.RawBytes || c.TTLSeconds == 0 || c.TTLSeconds > 300 {
		return c, fmt.Errorf("invalid authority fixture identity, funding or TTL")
	}
	return c, nil
}

func configureAuthorityFixture(ctx context.Context, s, backend *store.Store, c authorityFixture) *hub.TestServer {
	clock := func() store.IssuerTime {
		now := time.Now().UTC()
		return store.IssuerTime{Earliest: now, Latest: now, Trusted: true}
	}
	issuer := &hub.Issuer{Store: s, HubID: c.Delegation.HubID, Clock: clock}
	return &hub.TestServer{
		Store: s, DemandIssuer: issuer, AcceptUsage: true,
		Usage: &hub.UsageRelay{Store: s, Clock: clock, Backend: &fixtureUsageBackend{store: backend}},
		AuthorizationUpdate: func(nodeID, bootID string) (*controlv1.AuthorizationUpdate, error) {
			if nodeID != c.Delegation.NodeID || bootID == "" {
				return nil, fmt.Errorf("authority fixture node/boot mismatch")
			}
			d := c.Delegation
			d.BootID = bootID
			if err := s.InstallHubDelegation(ctx, d, clock()); err != nil {
				return nil, err
			}
			return issuer.Issue(ctx, d.ID, nodeID, bootID, c.InitialRequestID, c.InitialBytes, time.Duration(c.TTLSeconds)*time.Second)
		},
	}
}

type fixtureUsageBackend struct{ store *store.Store }

func (b *fixtureUsageBackend) AcceptUsage(ctx context.Context, u store.HubUsage) ([]byte, error) {
	if b == nil || b.store == nil {
		return nil, fmt.Errorf("fixture backend store missing")
	}
	digest := sha256.Sum256(u.Payload)
	if !bytes.Equal(digest[:], u.Digest) {
		return nil, fmt.Errorf("fixture usage digest mismatch")
	}
	var err error
	switch u.Kind {
	case "unknown":
		err = b.store.PersistUnknownUsage(ctx, u.NodeID, u.Payload)
	case "known":
		var batch controlv1.UsageBatch
		if err = proto.Unmarshal(u.Payload, &batch); err != nil {
			return nil, err
		}
		if batch.BatchId != u.BatchID {
			return nil, fmt.Errorf("fixture usage identity mismatch")
		}
		err = b.store.PersistUsageLedger(ctx, store.UsageBatch{NodeID: u.NodeID, BootID: u.BootID, BatchID: u.BatchID, SnapshotID: batch.BillingSnapshotId, Payload: u.Payload})
	default:
		return nil, fmt.Errorf("unsupported fixture usage kind")
	}
	if err != nil {
		return nil, err
	}
	return digest[:], nil
}
