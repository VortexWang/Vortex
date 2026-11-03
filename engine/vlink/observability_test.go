package vlink

import (
	"testing"
	"time"
)

func TestObservabilityRetainsSafeBoundedEventsAndCounters(t *testing.T) {
	observer, err := NewMemoryObserver(2)
	if err != nil {
		t.Fatal(err)
	}
	observer.Frame(PeerRolePeer, FrameData, 1, 4)
	observer.Frame(PeerRolePeer, FrameDgram, 2, 5)
	observer.Rejected(PeerRolePeer, ErrorClassAuthorization)
	observer.Closed(PeerRolePeer, ErrorClassProtocol)
	observer.WindowWait(time.Millisecond)
	observer.ActiveStreams(3)
	observer.Event(VLinkEvent{Kind: "security_rejection", PeerRole: PeerRolePeer, ErrorClass: ErrorClassAuthorization, FrameType: FrameOpen})
	observer.Event(VLinkEvent{Kind: "link_state", PeerRole: PeerRolePeer, ErrorClass: ErrorClassNone})
	observer.Event(VLinkEvent{Kind: "link_close", PeerRole: PeerRolePeer, ErrorClass: ErrorClassProtocol})
	if got := len(observer.Events()); got != 2 {
		t.Fatalf("event retention=%d", got)
	}
	snapshot := observer.Snapshot()
	if snapshot.FramesIn[FrameData] != 1 || snapshot.BytesOut[FrameDgram] != 5 || snapshot.Rejects[ErrorClassAuthorization] != 1 || snapshot.CloseCount[ErrorClassProtocol] != 1 || snapshot.Streams != 3 {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	observer.Shutdown()
	observer.Frame(PeerRolePeer, FrameData, 1, 1)
	if observer.Snapshot().FramesIn[FrameData] != 1 {
		t.Fatal("accepted metrics after shutdown")
	}
}

func TestObservabilityRejectsInvalidHighCardinalityShapes(t *testing.T) {
	observer, err := NewMemoryObserver(1)
	if err != nil {
		t.Fatal(err)
	}
	observer.Frame(PeerRole("tenant-42"), FrameData, 1, 3)
	observer.Event(VLinkEvent{Kind: "normal_data", PeerRole: PeerRolePeer, ErrorClass: ErrorClassNone})
	if len(observer.Events()) != 0 || len(observer.Snapshot().FramesIn) != 0 {
		t.Fatal("accepted invalid observability labels")
	}
}
