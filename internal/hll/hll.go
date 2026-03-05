package hll

import (
	"math"
)

type Mode string

const (
	SparseMode Mode = "sparse"
	DenseMode  Mode = "dense"
)

type HyperLogLog struct {
	k         uint8   //no of precision bits -> the first k bits of the hash determine the bucket index
	m         uint32  // number of registers (buckets) = 2^k
	registers []uint8 // dense register array; each register stores rho (number of leading zeros + 1).
	// no of leading zeroes are small (<64 for a 64-bit hash), so uint8 (0–255) is sufficient.
	sparse []uint32 //sparse representation: stores packed (index, value) pairs instead of a full register array to save memory
	mode   Mode     //indicates if HLL is in sparse or dense mode
}

func NewHyperLogLog(e float32) *HyperLogLog {
	//the error e ≈ 1.04 / √m
	//so we can solve for m and get the appropriate size, and compute k
	mFloat := math.Pow(1.04/float64(e), 2)

	k := uint8(math.Ceil(math.Log2(mFloat)))

	m := uint32(1) << k

	return &HyperLogLog{
		k:      k,
		m:      m,
		sparse: make([]uint32, 0), //will store register index and value, which will later be copied once when switching to dense mode
		mode:   SparseMode,
	}

}

//TODO : Add(hash uint64) -> will also have the trigger to switch to dense mode, hash will be computed by tracker from the ip addr

//TODO : Estimate() uint64
