package hll

import (
	"fmt"
	"testing"
)

//tests

// zero test
func TestHLLLifeCycle(t *testing.T) {
	//init an hll with 3.25% error rate
	//This should result in m=1024 (Pivot threshold from sparse to dense = 256)
	hll := NewHyperLogLog(0.0325)

	if val := hll.Estimate(); val != 0 {
		t.Errorf("Expected 0 elements but got %d", val)
	}

	testIp := "221.241.132.145"
	hll.Add(GetHashString(testIp))
	//check deduplication, for 1 element linear counting gives 1.0004 approx 1
	if val := hll.Estimate(); val != 1 {
		t.Errorf("Deduplication check failed for 1 element got %d instead", val)
	}

	//check with 100 entries, again should be linear counting
	for i := 0; i < 100; i++ {
		ip := fmt.Sprintf("192.168.1.%d", i)
		hll.Add(GetHashString(ip))
	}

	//total unique elements now: 101(100 ips + 1 deduplication testIP)
	if mode := hll.mode; mode != SparseMode {
		t.Errorf("Expected sparse mode got %v", mode)
	}

	//crosses 256, should switch mode
	for i := 0; i < 200; i++ {
		ip := fmt.Sprintf("10.0.0.%d", i)
		hll.Add(GetHashString(ip))
	}
	if mode := hll.mode; mode != DenseMode {
		t.Errorf("Expected dense mode got %v", mode)
	}

	t.Logf("Final Estimate for 301 IPs: %d", hll.Estimate())

}

func TestHLLAccuracy(t *testing.T) {
	//create a hll with 2% error rate
	hll := NewHyperLogLog(0.02)

	//Add 10000 unique elements
	// We divide 'i' by 256 for the 3rd octet and take 'i' modulo 256 for the 4th,
	// so the IP values safely stay between 0 and 255.
	for i := 0; i < 10000; i++ {
		ip := fmt.Sprintf("192.168.%d.%d", i/256, i%256)
		hll.Add(GetHashString(ip))
	}

	// check accuracy
	estimate := hll.Estimate()
	t.Logf("Estimate for 10000 unique elements: %d", estimate)
	// Since we set a 2% standard error rate, most estimates should fall within 2%

	//add another 50000 unique elements
	for i := 0; i < 50000; i++ {
		ip := fmt.Sprintf("192.169.%d.%d", i/256, i%256)
		hll.Add(GetHashString(ip))
	}

	// check accuracy
	estimate = hll.Estimate()
	t.Logf("Estimate for 60000 unique elements: %d", estimate)

}
