package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"strconv"
)

// renewalTriggerEpochTx observes an existing live family. A funded flat source
// may advance only through retained adjacent handoffs and exact independently
// provisioned root revisions. It neither spends R nor makes old credit current
// for new-session admission. Other share forms retain their current-only check.
func renewalTriggerEpochTx(ctx context.Context, tx *sql.Tx, node string, a AuthorizationRecord) (uint64, error) {
	_, state, err := readAuthorizationTx(ctx, tx, node, a.AuthorizationID)
	if err != nil {
		return 0, err
	}
	if state != "active" || a.Kind != "credit_grant" {
		return 0, ErrAuthorizationDenied
	}
	path, err := stateValue(ctx, tx, "authorization_path_generation")
	if err != nil {
		return 0, err
	}
	if path != strconv.FormatUint(a.PathGeneration, 10) {
		return 0, ErrAuthorizationGenerationStale
	}
	epochText, err := stateValue(ctx, tx, "authorization_signer_epoch")
	if err != nil {
		return 0, err
	}
	epoch, err := strconv.ParseUint(epochText, 10, 63)
	if err != nil || epoch < a.SignerEpoch || epoch-a.SignerEpoch > 4096 {
		return 0, ErrAuthorizationDenied
	}
	if err = checkSourceLineageTx(ctx, tx, node, a); err != nil {
		return 0, err
	}
	var rootID string
	if err = tx.QueryRowContext(ctx, `SELECT authorization_id FROM authorization_source_owners WHERE node_id=? AND source_id=?`, node, a.SourceID).Scan(&rootID); err != nil {
		return 0, err
	}
	root, rootState, err := readAuthorizationTx(ctx, tx, node, rootID)
	if err != nil {
		return 0, err
	}
	if rootState != "active" || root.Kind != "credit_grant" || root.IssuerID != a.IssuerID || root.BootID != a.BootID || root.ParentAuthorizationID != a.ParentAuthorizationID {
		return 0, ErrAuthorizationDenied
	}
	effective, err := sourceEpochTx(ctx, tx, node, a.SourceID, root.SignerEpoch)
	if err != nil {
		return 0, err
	}
	if effective != epoch {
		return 0, ErrAuthorityHandoffRequired
	}
	if epoch == a.SignerEpoch {
		return epoch, checkAuthorizationShareTx(ctx, tx, node, a, true)
	}
	prior, err := readShareTx(ctx, tx, node, a.ShareReferenceID, a.ShareRevision)
	if err != nil {
		return 0, err
	}
	if prior.ParentID != "" || prior.ParentRevision != 0 {
		return 0, ErrAuthorizationDenied
	}
	for from := a.SignerEpoch; from < epoch; from++ {
		if prior.Revision >= math.MaxInt64 {
			return 0, ErrAuthorizationDenied
		}
		next, err := readShareTx(ctx, tx, node, prior.ID, prior.Revision+1)
		if err != nil {
			return 0, ErrAuthorizationDenied
		}
		expected := prior
		expected.Revision++
		expected.SignerEpoch++
		want, _ := json.Marshal(expected)
		got, _ := json.Marshal(next)
		if !bytes.Equal(want, got) {
			return 0, ErrAuthorizationDenied
		}
		prior = next
	}
	if err = checkShareChainTx(ctx, tx, node, a.IssuerID, prior, true); err != nil {
		return 0, err
	}
	return epoch, nil
}
