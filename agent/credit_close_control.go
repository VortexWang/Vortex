package agent

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"

	"vortex/agent/store"
	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
)

func (c *ControlClient) closeCreditFromBackend(ctx context.Context, issuer string, command *controlv1.CreditCloseCommand) (*controlv1.CreditCloseReceipt, error) {
	if c == nil || c.Node == nil || !canonicalUUID(c.SourceBackendID) || issuer != c.SourceBackendID || command == nil || len(command.ProtoReflect().GetUnknown()) != 0 || !bytes.Equal(command.PayloadSha256, sha256BytesControl(command.Payload)) {
		return nil, contracts.ErrCreditClose
	}
	p, err := contracts.DecodeCreditCloseCommand(command.Payload)
	if err != nil || p.BackendID != issuer || p.NodeID != c.Node.NodeID || p.BootID != c.Node.BootID {
		return nil, contracts.ErrCreditClose
	}
	final, err := c.Node.CloseIssuedCreditGrant(ctx, p.HubID, &controlv1.AuthorizationUpdate{Generation: p.Generation, Payload: p.Original}, p.GrantID)
	r := contracts.CreditCloseReceipt{Version: 1, CommandID: p.ID, BackendID: issuer, NodeID: c.Node.NodeID, BootID: c.Node.BootID, GrantID: p.GrantID, CommandSHA256: hex.EncodeToString(command.PayloadSha256), State: "unsettled"}
	if err == nil {
		r.State = "final"
		r.Final, err = json.Marshal(final)
	} else if errors.Is(err, store.ErrCreditGrantUnsettled) {
		err = nil
	}
	if err != nil {
		return nil, err
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	return &controlv1.CreditCloseReceipt{Payload: raw, PayloadSha256: sha256BytesControl(raw)}, nil
}
