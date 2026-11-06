package store

import (
	"context"
	"strconv"
)

// HoldRuntimeShares binds immutable chain revisions to the owning configuration
// transaction. It reserves no new funds: allocation is already charged to the
// parent by share_references. Unknown activation keeps the historical revisions
// in that capacity calculation until an executor proves release.
func (s *Store) HoldRuntimeShares(ctx context.Context, id string, scope AuthorizationUse) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var fence, state string
	if err = tx.QueryRowContext(ctx, `SELECT fencing_generation,state FROM config_results WHERE transaction_id=?`, id).Scan(&fence, &state); err != nil {
		return err
	}
	if fence != strconv.FormatUint(scope.FencingGeneration, 10) || state == "released" || state == "aborted" {
		return ErrConfigGenerationStale
	}
	if err = checkRuntimeFenceTx(ctx, tx, scope.FencingGeneration); err != nil {
		return err
	}
	choices, err := selectRuntimeAuthorityTx(ctx, tx, scope, 1, nil)
	if err != nil {
		return err
	}
	for _, a := range []AuthorizationRecord{choices.grant, choices.runtime, choices.share} {
		if a.ShareReferenceID == "" {
			continue
		} // previously installed legacy contract
		r, err := readShareTx(ctx, tx, scope.NodeID, a.ShareReferenceID, a.ShareRevision)
		if err != nil {
			return err
		}
		for depth := 0; depth < 32; depth++ {
			if _, err = tx.ExecContext(ctx, `INSERT INTO config_share_holds(transaction_id,node_id,boot_id,share_id,revision,generation,state) VALUES(?,?,?,?,?,?,'held') ON CONFLICT DO NOTHING`, id, scope.NodeID, scope.BootID, r.ID, strconv.FormatUint(r.Revision, 10), fence); err != nil {
				return err
			}
			if r.ParentID == "" {
				break
			}
			r, err = readShareTx(ctx, tx, scope.NodeID, r.ParentID, r.ParentRevision)
			if err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
