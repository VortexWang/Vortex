package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"

	"google.golang.org/protobuf/proto"
	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/engine/apply"
	ruleruntime "vortex/engine/runtime/rule"
)

// ApplyRuleCommand is deliberately separate from legacy ApplyRuntimeConfig:
// Prepare durably holds resources and returns without invoking Commit.
func (r *Reconciler) ApplyRuleCommand(ctx context.Context, node string, c *controlv1.RuleCommand) *controlv1.RuleReceipt {
	out := &controlv1.RuleReceipt{State: "failed", ErrorCode: "invalid_config"}
	if c == nil {
		return out
	}
	out.Phase = c.Phase
	out.TaskKey = c.TaskKey
	out.TransactionId = append([]byte(nil), c.TransactionId...)
	out.TargetRevision = c.TargetRevision
	out.IdempotencyKey = c.IdempotencyKey
	out.ArtifactSha256 = append([]byte(nil), c.ArtifactSha256...)
	out.FencingGeneration = c.FencingGeneration
	if r == nil || r.Store == nil || r.Apply == nil || !canonicalUUID(node) || len(c.TransactionId) != 16 || len(c.TaskKey) == 0 || len(c.TaskKey) > 128 || len(c.IdempotencyKey) == 0 || len(c.IdempotencyKey) > 128 || c.TargetRevision == 0 || c.FencingGeneration == 0 || len(c.ArtifactSha256) != 32 || !noUnknownFields(c.ProtoReflect()) {
		return out
	}
	if c.Phase != "prepare" && c.Phase != "commit" && c.Phase != "query" && c.Phase != "abort" && c.Phase != "release" {
		return out
	}
	digest := sha256.Sum256(c.Payload)
	if !bytes.Equal(digest[:], c.ArtifactSha256) {
		out.ErrorCode = "payload_digest_mismatch"
		return out
	}
	if len(c.Payload) == 0 || len(c.Payload) > 524288 {
		return out
	}
	idText := hex.EncodeToString(c.TransactionId)
	var id [16]byte
	copy(id[:], c.TransactionId)
	if id == ([16]byte{}) {
		return out
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	correlation := proto.Clone(c).(*controlv1.RuleCommand)
	correlation.Payload = nil
	raw, _ := proto.MarshalOptions{Deterministic: true}.Marshal(correlation)
	if e := r.Store.BindRuleCommand(ctx, c.TaskKey, idText, raw); e != nil {
		out.ErrorCode = storeConfigError(e)
		return out
	}
	record, e := r.Store.GetConfigResult(ctx, idText)
	if c.Phase == "prepare" {
		// The versioned artifact decoder belongs to the actual runtime applier.
		validator, ok := r.Apply.(interface {
			ValidateRuleArtifact([]byte, string, uint64, uint64) error
		})
		if !ok {
			out.ErrorCode = "unsupported_config"
			return out
		}
		if e = validator.ValidateRuleArtifact(c.Payload, node, c.TargetRevision, c.FencingGeneration); e != nil {
			out.ErrorCode = stableConfigError(e)
			return out
		}
		artifact, decodeErr := ruleruntime.Decode(c.Payload)
		if decodeErr != nil {
			return out
		}
		record, e = r.Store.SaveRuleConfigRequest(ctx, artifact.RuleID, store.ConfigResult{TransactionID: idText, Version: c.TargetRevision, FencingGeneration: c.FencingGeneration, ExpectedVersion: c.ExpectedVersion, Payload: c.Payload, PayloadSHA256: c.ArtifactSha256})
		if e != nil {
			out.ErrorCode = storeConfigError(e)
			return out
		}
		// The first platform group is the billing/rate execution point.
		// All rule references at this node reuse one tenant/node aggregate.
		execution := -1
		for hop, group := range artifact.Intent.Rule.Path {
			if artifact.InputVersions["grant/"+group] != "" {
				execution = hop
				break
			}
		}
		if execution == artifact.Intent.Hop {
			if e = r.Store.BindPlatformShareRule(ctx, artifact.TenantID, artifact.NodeID, artifact.RuleID); e != nil {
				out.ErrorCode = storeConfigError(e)
				return out
			}
		}
		if record.State == "received" {
			executor, ok := r.Apply.(interface{ ExecutionID() string })
			if !ok {
				out.ErrorCode = "unsupported_config"
				return out
			}
			if e = r.Store.BindActivationExecutor(ctx, idText, executor.ExecutionID()); e != nil {
				out.ErrorCode = storeConfigError(e)
				return out
			}
			preparer, ok := r.Apply.(interface {
				PrepareRuleArtifact([16]byte, uint64, []byte) error
			})
			if !ok {
				out.ErrorCode = "unsupported_config"
				return out
			}
			if e = preparer.PrepareRuleArtifact(id, c.FencingGeneration, c.Payload); e != nil {
				_ = r.Store.SaveConfigResult(ctx, idText, c.TargetRevision, "prepare-failed", e.Error())
				out.ErrorCode = stableConfigError(e)
				return out
			}
			if e = r.Store.SaveConfigResult(ctx, idText, c.TargetRevision, "prepared", ""); e != nil {
				out.State = "unknown"
				out.ErrorCode = "activation_unknown"
				return out
			}
		}
	} else if e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			out.State = "unknown"
			out.ErrorCode = "activation_unknown"
			return out
		}
		out.ErrorCode = storeConfigError(e)
		return out
	}
	if record.Version != c.TargetRevision || record.FencingGeneration != c.FencingGeneration || !bytes.Equal(record.PayloadSHA256, c.ArtifactSha256) || !bytes.Equal(record.Payload, c.Payload) {
		out.ErrorCode = "tx_conflict"
		return out
	}
	executor, ok := r.Apply.(interface {
		ExecutionID() string
		Query([16]byte) (apply.Transaction, bool)
	})
	if !ok {
		out.ErrorCode = "unsupported_config"
		return out
	}
	same, e := r.Store.ActivationExecutorMatches(ctx, idText, executor.ExecutionID())
	if e != nil || !same {
		out.State = "unknown"
		out.ErrorCode = "activation_unknown"
		return out
	}
	tx, found := executor.Query(id)
	if !found {
		out.State = "unknown"
		out.ErrorCode = "activation_unknown"
		_ = r.Store.ObserveActivation(ctx, idText, c.FencingGeneration, "missing")
		return out
	}
	switch c.Phase {
	case "release":
		releaser, ok := r.Apply.(interface {
			ReleaseRule(context.Context, [16]byte) error
		})
		if !ok || releaser.ReleaseRule(ctx, id) != nil {
			out.State = "unknown"
			out.ErrorCode = "activation_unknown"
			return out
		}
	case "commit":
		if tx.State == apply.Prepared {
			if e = r.Store.SaveConfigResult(ctx, idText, c.TargetRevision, "committing", ""); e != nil {
				out.ErrorCode = storeConfigError(e)
				return out
			}
			if _, e = r.Apply.Commit(id, false); e != nil {
				_ = r.Store.SaveConfigResult(ctx, idText, c.TargetRevision, "activation-unknown", e.Error())
				out.State = "unknown"
				out.ErrorCode = "activation_unknown"
				return out
			}
		}
	case "abort":
		if tx.State == apply.Prepared {
			if _, e = r.Apply.Abort(id); e != nil {
				out.State = "unknown"
				out.ErrorCode = "activation_unknown"
				return out
			}
		}
	}
	tx, found = executor.Query(id)
	if !found {
		out.State = "unknown"
		out.ErrorCode = "activation_unknown"
		return out
	}
	state := string(tx.State)
	if e = r.Store.ObserveActivation(ctx, idText, c.FencingGeneration, state); e != nil {
		out.State = "unknown"
		out.ErrorCode = "activation_unknown"
		return out
	}
	switch tx.State {
	case apply.Prepared:
		out.State = "prepared"
	case apply.Active:
		out.State = "active"
	case apply.Released:
		if c.Phase == "release" {
			out.State = "released"
		} else {
			out.State = "aborted"
		}
	case apply.Aborted:
		out.State = "aborted"
	default:
		out.State = "unknown"
	}
	out.ErrorCode = ""
	return out
}
