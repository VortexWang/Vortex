package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"

	"google.golang.org/protobuf/proto"
	controlv1 "vortex/contracts/protobuf/v1"
)

// PendingRuleCommit observes an explicit durable Commit for the exact prepared
// artifact and live executor. It grants no execution or financial authority.
// Prepare/query-only history and another process cannot request bootstrap work.
func (s *Store) PendingRuleCommit(ctx context.Context, id, executor string) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var state, version, fence, execution string
	var digest []byte
	err = tx.QueryRowContext(ctx, `SELECT c.state,c.version_u64,c.fencing_generation,c.payload_sha256,e.execution_id FROM config_results c JOIN rule_config_scopes r USING(transaction_id) JOIN activation_executors e USING(transaction_id) WHERE c.transaction_id=?`, id).Scan(&state, &version, &fence, &digest, &execution)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if state != "prepared" || executor == "" || execution != executor {
		return false, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT task_key,correlation FROM rule_command_bindings WHERE transaction_id=?`, id)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var task string
		var raw []byte
		if err = rows.Scan(&task, &raw); err != nil {
			return false, err
		}
		c := new(controlv1.RuleCommand)
		if err = proto.Unmarshal(raw, c); err != nil {
			return false, err
		}
		if c.Phase == "commit" && c.TaskKey == task && c.IdempotencyKey != "" && len(c.Payload) == 0 && len(c.ProtoReflect().GetUnknown()) == 0 && hex.EncodeToString(c.TransactionId) == id && fmt.Sprint(c.TargetRevision) == version && fmt.Sprint(c.FencingGeneration) == fence && bytes.Equal(c.ArtifactSha256, digest) {
			return true, nil
		}
	}
	return false, rows.Err()
}
