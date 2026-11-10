package service_test

import (
	"math"
	"math/rand/v2"
	"reflect"
	"testing"
	"vortex/backend/internal/service"
)

func TestDistributionExactDemandAndFIFOQueue(t *testing.T) {
	in := []service.AllocationDemand{{NodeID: "b", Demand: 1, ExhaustionPPM: 1000000, QueueOrder: 1}, {NodeID: "a", Demand: 1, QueueOrder: 2}}
	out, e := service.DistributeShares(120, false, in)
	if e != nil || out[0].Amount != 50 || out[1].Amount != 70 {
		t.Fatal(out, e)
	}
	out, e = service.DistributeShares(1, true, in)
	if e != nil || !out[0].Queued || out[0].Amount != 0 || out[1].Amount != 1 || out[1].Queued {
		t.Fatal("not real FIFO queue", out, e)
	}
	out, e = service.DistributeShares(5, false, []service.AllocationDemand{{NodeID: "c"}, {NodeID: "a"}, {NodeID: "b"}})
	if e != nil || out[0].Amount != 2 || out[1].Amount != 2 || out[2].Amount != 1 {
		t.Fatal("unstable zero demand remainder", out, e)
	}
	for _, bad := range [][]service.AllocationDemand{{{NodeID: "a"}, {NodeID: "a"}}, {{NodeID: "a", ExhaustionPPM: 1000001}}, {{NodeID: "a", QueueOrder: -1}}} {
		if _, e = service.DistributeShares(1, false, bad); e == nil {
			t.Fatal("invalid demand accepted")
		}
	}
}
func TestDistributionPropertiesConservationFloorAndPermutation(t *testing.T) {
	random := rand.New(rand.NewPCG(61, 606))
	for round := 0; round < 3000; round++ {
		n := random.IntN(40) + 1
		total := random.Int64N(1000000)
		if round%10 == 0 {
			total = math.MaxInt64
		}
		in := make([]service.AllocationDemand, n)
		for i := range in {
			in[i] = service.AllocationDemand{NodeID: string(rune('A' + i)), Demand: random.Uint64(), ExhaustionPPM: uint32(random.IntN(1000001)), QueueOrder: int64(i)}
		}
		for _, connections := range []bool{false, true} {
			out, e := service.DistributeShares(total, connections, in)
			if e != nil {
				t.Fatal(e)
			}
			sum := int64(0)
			for _, v := range out {
				if v.Amount < 0 || v.Amount > total-sum {
					t.Fatal("overflow/negative", round, out)
				}
				sum += v.Amount
				if (!connections || total >= int64(n)) && v.Amount < (total/2)/int64(n) {
					t.Fatal("floor lost", round, out)
				}
			}
			if sum != total {
				t.Fatal("capacity not conserved", round, sum, total)
			}
			perm := append([]service.AllocationDemand(nil), in...)
			random.Shuffle(n, func(i, j int) { perm[i], perm[j] = perm[j], perm[i] })
			again, e := service.DistributeShares(total, connections, perm)
			if e != nil || !reflect.DeepEqual(out, again) {
				t.Fatal("input order changes allocation", round, out, again, e)
			}
		}
	}
}
