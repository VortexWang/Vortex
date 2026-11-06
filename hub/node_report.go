package hub

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/proto"
	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
)

type NodeReportSnapshot struct {
	BootID, SessionID string
	Report            *controlv1.NodeReport
}

func (s *TestServer) beginReportSession(nodeID, bootID, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reports == nil {
		s.reports = make(map[string]NodeReportSnapshot)
	}
	if _, exists := s.reports[nodeID]; !exists && len(s.reports) >= 4096 {
		return fmt.Errorf("test hub: current telemetry capacity exhausted")
	}
	s.reports[nodeID] = NodeReportSnapshot{BootID: bootID, SessionID: sessionID}
	return nil
}

func (s *TestServer) endReportSession(nodeID, sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reports[nodeID].SessionID == sessionID {
		delete(s.reports, nodeID)
	}
}

func (s *TestServer) acceptReport(ctx context.Context, nodeID, bootID, sessionID string, r *controlv1.NodeReport) error {
	if err := contracts.ValidateNodeReport(r); err != nil {
		return err
	}
	s.mu.Lock()
	current := s.reports[nodeID]
	if current.SessionID != sessionID || current.BootID != bootID {
		s.mu.Unlock()
		return fmt.Errorf("test hub: stale report session")
	}
	current.Report = proto.Clone(r).(*controlv1.NodeReport)
	s.reports[nodeID] = current
	s.mu.Unlock()
	if s.ReportObserver != nil {
		return s.ReportObserver(ctx, nodeID, bootID, sessionID, proto.Clone(r).(*controlv1.NodeReport))
	}
	return nil
}

// CurrentNodeReport is an ephemeral current-session view, never an authority
// clock or an allocation input. Disconnect drops the view, not any ledger row.
func (s *TestServer) CurrentNodeReport(nodeID string) *NodeReportSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.reports[nodeID]
	if !ok || r.Report == nil {
		return nil
	}
	r.Report = proto.Clone(r.Report).(*controlv1.NodeReport)
	return &r
}
