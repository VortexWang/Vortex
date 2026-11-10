package service

import (
	"errors"
	"math/big"
	"sort"
)

// AllocationDemand is one distinct tenant/node execution point. Demand is
// measured over the completed 60-second window; exhaustion is parts per million.
// These measurements influence targets only, never mint or release ownership.
type AllocationDemand struct {
	NodeID        string `json:"node_id"`
	Demand        uint64 `json:"demand"`
	ExhaustionPPM uint32 `json:"exhaustion_ppm"`
	// QueueOrder is persisted when the candidate first becomes eligible.
	QueueOrder int64 `json:"queue_order"`
}
type AllocationTarget struct {
	NodeID string `json:"node_id"`
	Amount int64  `json:"amount"`
	Queued bool   `json:"queued"`
}

var ErrAllocationInput = errors.New("allocation_input_invalid")

// DistributeShares applies a 50% equal floor and a demand*(1+exhaustion)
// remainder. Arithmetic is exact even at the uint64 demand boundary. Remainders
// use ascending node ID; a connection slot shortage uses persisted FIFO order.
// Returned amounts are targets: retained unknown ownership must still be paid
// for by PrepareShare before any target can be used.
func DistributeShares(total int64, connections bool, input []AllocationDemand) ([]AllocationTarget, error) {
	if total < 0 || len(input) > 10000 {
		return nil, ErrAllocationInput
	}
	candidates := append([]AllocationDemand(nil), input...)
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].NodeID < candidates[j].NodeID })
	for i, c := range candidates {
		if c.NodeID == "" || c.ExhaustionPPM > 1000000 || c.QueueOrder < 0 || i > 0 && candidates[i-1].NodeID == c.NodeID {
			return nil, ErrAllocationInput
		}
	}
	out := make([]AllocationTarget, len(candidates))
	for i, c := range candidates {
		out[i].NodeID = c.NodeID
	}
	if len(candidates) == 0 {
		return out, nil
	}
	eligible := make([]int, len(candidates))
	for i := range eligible {
		eligible[i] = i
	}
	if connections && total < int64(len(candidates)) {
		sort.Slice(eligible, func(i, j int) bool {
			a, b := candidates[eligible[i]], candidates[eligible[j]]
			if a.QueueOrder != b.QueueOrder {
				return a.QueueOrder < b.QueueOrder
			}
			return a.NodeID < b.NodeID
		})
		for n, i := range eligible {
			if int64(n) < total {
				out[i].Amount = 1
			} else {
				out[i].Queued = true
			}
		}
		return out, nil
	}
	// Give each kind exactly half (rounded down) as an even guaranteed pool.
	equal := total / 2
	n := int64(len(eligible))
	for k, i := range eligible {
		out[i].Amount = equal / n
		if int64(k) < equal%n {
			out[i].Amount++
		}
	}
	rest := total - equal
	weights := make([]*big.Int, len(candidates))
	sum := new(big.Int)
	for i, c := range candidates {
		weights[i] = new(big.Int).SetUint64(c.Demand)
		weights[i].Mul(weights[i], new(big.Int).SetUint64(1000000+uint64(c.ExhaustionPPM)))
		sum.Add(sum, weights[i])
	}
	if sum.Sign() == 0 {
		for k, i := range eligible {
			out[i].Amount += rest / n
			if int64(k) < rest%n {
				out[i].Amount++
			}
		}
		return out, nil
	}
	remaining := rest
	for i := range candidates {
		amount := new(big.Int).Mul(big.NewInt(rest), weights[i])
		amount.Quo(amount, sum)
		v := amount.Int64()
		out[i].Amount += v
		remaining -= v
	}
	for i := int64(0); i < remaining; i++ {
		out[i].Amount++
	}
	return out, nil
}
