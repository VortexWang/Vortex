package path

import "testing"

func node(v byte) NodeID { var n NodeID; n[0] = v; return n }

func TestSelectRejectsBypassAndDuplicates(t *testing.T) {
	if _, err := Select([]Candidate{{Nodes: []NodeID{node(1), node(1)}, Authorized: true, Healthy: true}}); err != ErrNoRoute {
		t.Fatalf("duplicate route accepted: %v", err)
	}
	got, err := Select([]Candidate{{Nodes: []NodeID{node(1), node(2), node(3)}, Authorized: true, Healthy: true}})
	if err != nil || len(got.Nodes) != 3 {
		t.Fatalf("route=%v err=%v", got, err)
	}
}

func TestGenerationDrainsTCPAndRejectsNewOpen(t *testing.T) {
	c := NewCoordinator(nil)
	g1, _ := c.Activate(Candidate{Nodes: []NodeID{node(1), node(2)}, Authorized: true, Healthy: true})
	if err := c.Open(g1, false); err != nil {
		t.Fatal(err)
	}
	g2, _ := c.Activate(Candidate{Nodes: []NodeID{node(1), node(3)}, Authorized: true, Healthy: true})
	if g2 != g1+1 {
		t.Fatalf("generation=%d", g2)
	}
	if err := c.Open(g1, false); err == nil {
		t.Fatal("draining generation accepted new TCP")
	}
	if err := c.Open(g2, false); err != nil {
		t.Fatal(err)
	}
	c.Close(g1, false)
	if _, ok := c.Snapshot(g1); ok {
		t.Fatal("drained generation retained")
	}
}
