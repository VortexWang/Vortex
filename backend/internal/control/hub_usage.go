package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"google.golang.org/protobuf/proto"
	"io"
	"vortex/backend/db/generated"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/hub"
)

func (h *HubAdapter) ingestHubUsage(ctx context.Context, id string, record *controlv1.LedgerRecord) error {
	if h.Metering == nil {
		return errors.New("Hub usage authority unavailable")
	}
	var e error
	var envelope hub.UsageEnvelope
	decoder := json.NewDecoder(bytes.NewReader(record.Payload))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(&envelope); e != nil {
		return e
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return errors.New("trailing usage envelope")
	}
	if envelope.Version != 1 || envelope.BatchID != record.RecordId || envelope.Sequence != record.LedgerSequence || !bytes.Equal(sha256Bytes(envelope.Payload), envelope.Digest) {
		return errors.New("original usage envelope mismatch")
	}
	owner, e := generated.New(h.Metering.Pool).UsageNodeHub(ctx, envelope.NodeID)
	if e != nil || owner != id {
		return errors.New("Hub cannot report another Hub node")
	}
	if envelope.Kind == "known" {
		if envelope.OriginBootID != envelope.ReportBootID {
			return errors.New("known usage origin boot mismatch")
		}
		batch := new(controlv1.UsageBatch)
		if e = proto.Unmarshal(envelope.Payload, batch); e != nil {
			return e
		}
		if batch.BatchId != envelope.BatchID {
			return errors.New("known usage batch mismatch")
		}
		if _, e = h.Metering.IngestWireUsage(ctx, envelope.NodeID, envelope.ReportBootID, envelope.Sequence, batch); e != nil {
			return e
		}
	} else if envelope.Kind == "unknown" {
		batch := new(controlv1.UnknownUsageBatch)
		if e = proto.Unmarshal(envelope.Payload, batch); e != nil {
			return e
		}
		if batch.BatchId != envelope.BatchID || batch.OriginBootId != envelope.OriginBootID {
			return errors.New("unknown usage origin mismatch")
		}
		if _, e = h.Metering.IngestWireUnknown(ctx, envelope.NodeID, envelope.ReportBootID, envelope.Sequence, batch); e != nil {
			return e
		}
	} else {
		return errors.New("unsupported Hub usage kind")
	}

	return nil
}
