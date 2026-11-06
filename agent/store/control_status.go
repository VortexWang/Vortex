package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"
)

// HubControlSnapshot reads existing authority only. It never refreshes backend
// time, opens a signer or creates allocation from a node heartbeat.
type HubControlSnapshot struct {
	Mode                       string
	Epoch                      uint64
	BackendHeartbeat, Deadline time.Time
	Confirmed, Pending         uint64
}

func (s *Store) RuntimeSessionCount(ctx context.Context, nodeID, bootID string) (uint64, error) {
	var count uint64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM runtime_sessions WHERE node_id=? AND boot_id=? AND state='open'`, nodeID, bootID).Scan(&count)
	return count, err
}

func (s *Store) HubControlSnapshot(ctx context.Context, hubID string, now time.Time) (HubControlSnapshot, error) {
	result := HubControlSnapshot{Mode: "blocked"}
	if s.owner != "hub" || !validIssuerUUID(hubID) {
		return result, ErrAuthorizationDenied
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	var epoch, state string
	err = tx.QueryRowContext(ctx, "SELECT signer_epoch,state FROM issuer_epochs WHERE hub_id=?", hubID).Scan(&epoch, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	result.Epoch, err = strconv.ParseUint(epoch, 10, 63)
	if err != nil {
		return result, err
	}
	hb, err := stateValue(ctx, tx, "backend_heartbeat")
	if err != nil {
		return result, err
	}
	result.BackendHeartbeat, err = time.Parse(time.RFC3339Nano, hb)
	if err != nil {
		return result, err
	}
	result.Deadline = deadlineFromTx(ctx, tx)
	mode, err := stateValue(ctx, tx, "authority_mode")
	if err != nil {
		return result, err
	}
	if state != "open" || now.Before(result.BackendHeartbeat) || !now.Before(result.Deadline) {
		return result, nil
	}
	switch mode {
	case "online":
		result.Mode = "online"
		if !now.Before(result.BackendHeartbeat.Add(BackendLossGrace)) {
			result.Mode = "autonomous"
		}
	case "autonomous", "recovering":
		result.Mode = mode
	}
	checkpoint, err := stateValue(ctx, tx, "ledger_checkpoint")
	if err == nil {
		result.Confirmed, err = strconv.ParseUint(checkpoint, 10, 63)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM hub_ledger WHERE confirmed=0").Scan(&result.Pending); err != nil {
		return result, err
	}
	return result, tx.Commit()
}
