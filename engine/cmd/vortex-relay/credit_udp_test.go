package main

import (
	"context"
	"io"
	"testing"
	core "vortex/engine/accounting"
	credit "vortex/engine/runtime/accounting"
)

func TestAuthorizedDatagramDenialQueueRefusalAndAmbiguousWrite(t *testing.T) {
	for _, tc := range []struct {
		name          string
		budget        int64
		n             int
		err           error
		queue         bool
		used, unknown int64
		calls         int
	}{
		{"denied", 0, 4, nil, false, 0, 0, 0},
		{"accepted", 4, 4, nil, false, 4, 0, 1},
		{"queue refusal", 4, 0, nil, true, 0, 0, 1},
		{"short network", 4, 2, nil, false, 0, 4, 1},
		{"zero network", 4, 0, nil, false, 0, 4, 1},
		{"error", 4, 2, io.ErrUnexpectedEOF, false, 0, 4, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			g, e := credit.NewCreditGate(credit.CreditConfig{SnapshotID: "s", Budget: tc.budget, InitialBlock: 4, MaxBlock: 4, GrantTarget: 4, RenewPercent: 30})
			if e != nil {
				t.Fatal(e)
			}
			// Keep an admitted session but exhaust it before Reserve in the denial case.
			if tc.budget == 0 {
				g, _ = credit.NewCreditGate(credit.CreditConfig{SnapshotID: "s", Budget: 4, InitialBlock: 4, MaxBlock: 4, GrantTarget: 4, RenewPercent: 30})
			}
			s, e := g.Open(ctx, core.Flow{Key: "flow"})
			if e != nil {
				t.Fatal(e)
			}
			if tc.budget == 0 {
				b, _ := s.Reserve(ctx, core.LeftToRight, 4)
				b.Commit(ctx, 4)
			}
			calls := 0
			ok, err := authorizedDatagram(ctx, s, core.RightToLeft, []byte("data"), tc.queue, func([]byte) (int, error) { calls++; return tc.n, tc.err })
			if calls != tc.calls {
				t.Fatal("writer bypass", calls)
			}
			wantErr := tc.budget == 0 || tc.err != nil || (!tc.queue && tc.n != 4)
			if (err != nil) != wantErr || ok != (tc.budget > 0 && tc.n == 4 && tc.err == nil) {
				t.Fatal(ok, err)
			}
			s.Close()
			v := g.State()
			wantUsed := tc.used
			if tc.budget == 0 {
				wantUsed = 4
			}
			if v.Used != wantUsed || v.Unknown != tc.unknown || v.Held != 0 || v.Sessions != 0 {
				t.Fatal(v)
			}
		})
	}
}
