package hll

import (
	"math"
	"math/bits"
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

	// Alpha (αm) corrects for "Bucket Overcrowding" (a systematic multiplicative bias).
	// Since each bucket only stores the single maximum number of leading zeroes,the
	// raw math tends to overestimate the true number of elements due to hash collisions.
	// Eg: if there were 60 elements hashed to a bucket, the single highest max zero count
	// might cause the raw formula to assume there were significantly more than 60 elements in that bucket.
	// This constant scales that "optimistic" estimate back down to the ground truth.
	alpha float64

	sparse []uint32 //sparse representation: stores packed (index, value) pairs instead of a full register array to save memory
	//It stores "packed" uint32s [24 bits: index | 8 bits: value] to avoid allocating full register array until a saturation point
	//using 24 and 8 combo since it will be easier to convert it to dense mode later
	mode Mode //indicates if HLL is in sparse or dense mode
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
		alpha:  getAlpha(m),
		sparse: make([]uint32, 0), //will store register index and value, which will later be copied once when switching to dense mode
		mode:   SparseMode,
	}

}

// TODO : Add(hash uint64) -> will also have the trigger to switch to dense mode, hash will be computed by tracker from the ip addr
func (hll *HyperLogLog) Add(hash uint64) {
	//bucket index

	//keep a mask, incase hash is all zeroes(000...000) -> 64 zeroes
	//if we got 64 zeroes, the rho might overflow to 65
	index := uint32(hash >> (64 - hll.k))

	//extract leading zeroes
	remainingBits := hash << hll.k

	// Calculate rho, but cap it at the actual bit-space available (64 - k)
	// bits.LeadingZeros64 returns 64 if all bits are 0.
	// if hash after the bucket index has all zeroes, the left shift done will apend a new set of zeores
	// this will cause it to incorrectly do the remaning numbr of zeroes, if everything becomes 0,
	/// since those extra zeroes were not from the hash, but the left shift operation that appended 0, it falsely spikes the value

	lz := bits.LeadingZeros64(remainingBits)

	//check to cap
	if lz > 64-int(hll.k) {
		lz = 64 - int(hll.k) //cap it
	}
	//Add 1 because HLL math defines rho as the position of the first '1' bit.
	rho := uint8(lz) + 1

	if hll.mode == SparseMode {
		hll.updateSparse(index, rho)
	} else {
		hll.updateDense(index, rho)
	}

}

// Implement Estimate() uint64
func (hll *HyperLogLog) Estimate() uint64 {
	//we want harmonic mean of all obervations, 2^r1, 2^r2, 2^r3
	sum := 0.0
	emptyBuckets := 0.0
	//account for linear counting too
	if hll.mode == SparseMode {
		for _, packed := range hll.sparse {
			rho := packed & (0xFF)
			sum += 1.0 / float64((uint64(1) << rho))
		}
		// Every bucket not in sparse is empty i.e rho=0
		// 2^0 is 1.0, so we just add 1.0 for every missing bucket
		emptyBuckets = float64(hll.m - uint32(len(hll.sparse)))
		sum += emptyBuckets
	} else {
		for _, rho := range hll.registers {
			sum += 1.0 / float64((uint64(1) << rho))
			if rho == 0 {
				emptyBuckets++ //count this too for linear counting formula applied
			}
		}
	}
	raw := hll.alpha * float64(hll.m) * float64(hll.m) / sum

	if raw <= 2.5*float64(hll.m) {
		if emptyBuckets > 0 {
			//linear Counting Formula: m * ln(m/V)
			return uint64(float64(hll.m) * math.Log(float64(hll.m)/emptyBuckets))
		}
	}

	return uint64(raw)
}

func getAlpha(m uint32) float64 {
	switch m {
	case 16:
		return 0.673
	case 32:
		return 0.697
	case 64:
		return 0.709
	default:
		return 0.7213 / (1 + 1.079/float64(m))
	}
}

func (hll *HyperLogLog) updateSparse(index uint32, rho uint8) {
	for i, packed := range hll.sparse {
		if packed>>8 == index {
			//update only if rho is greater
			//to get last 8 bits do bitwise and with 255
			curRho := packed & (0xFF)
			if rho > uint8(curRho) {
				hll.sparse[i] = (index << 8) | uint32(rho) //pack it back
			}
			return
		}
	}
	//if index is not found, simply add it
	hll.sparse = append(hll.sparse, index<<8|uint32(rho))

	//trigger if sparse exceeds threshold to convert it to dense
	//why m/4? since size of our sparse here is 4 times size of dense
	//(uint32 = 4 bytes; uint8 = 1 byte)
	//so when size of sparse becomes m/4, it practically takes same amount of memory as dense of size m
	if uint32(len(hll.sparse)) >= (hll.m)/4 {
		hll.convertToDense()
	}
}

func (hll *HyperLogLog) convertToDense() {
	hll.registers = make([]uint8, hll.m)
	for _, packed := range hll.sparse {
		index := packed >> 8
		value := packed & (0xFF) //or rho
		hll.registers[index] = uint8(value)
	}
	hll.sparse = nil
	hll.mode = DenseMode
}

func (hll *HyperLogLog) updateDense(index uint32, rho uint8) {
	if rho > hll.registers[index] {
		hll.registers[index] = rho
	}
}
