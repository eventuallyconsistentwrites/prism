// Loadgen — a concurrent HTTP load generator using the Producer-Consumer pattern.
//
// Architecture: Producer → Buffered Channel → Worker Pool → HTTP Server
//
// PATTERN DECISION: Jobs Channel (not Context-only loop)
//   - We have a FINITE number of requests to send (500,000)
//   - Workers pull from the channel; when the channel is drained + closed, they exit
//   - Backpressure: if workers are slow, buffered channel blocks the producer automatically
//   - Context timeout acts as a safety net (kills everything if 500k isn't done in time)
//
// If the work were INFINITE (blast as fast as possible for N seconds), use Context-only loop instead.
package main

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// ============ CONFIGURATION ============
// Keep target URLs as constants so they're easy to change and match your server routes exactly.
// IMPORTANT: these must match the paths registered in cmd/server/main.go, including trailing slashes.
const (
	ProductsPath = "http://localhost:8080/api/v1/products/list"
	CheckoutPath = "http://localhost:8080/api/v1/checkout"
	StatsPath    = "http://localhost:8080/internal/stats"
	TotalJobs    = 500_000
)

// ============ JOB STRUCT ============
// Each Job represents one HTTP request to fire.
// Pre-generating all job data (IP + path) avoids doing work inside the hot worker loop.
type Job struct {
	ip   string
	path string
}

func main() {

	// ============ PHASE 1: SETUP ============
	// Create all shared resources BEFORE launching any goroutines.
	// Rule: never let a goroutine initialize something another goroutine depends on.

	// Context with timeout — the fire alarm. If 500k jobs aren't done in time, kills everything.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var totalOps uint64
	start := time.Now()

	// POOL SIZE DECISION:
	// This loadgen is I/O bound (waiting on HTTP round trips), not CPU bound.
	// So we can run many more goroutines than CPU cores.
	// GOMAXPROCS * 20 is a heuristic for I/O heavy work.
	// If the bottleneck were a DB with 20 max connections, pool size = 20 instead.
	numWorkers := runtime.GOMAXPROCS(0) * 20
	fmt.Printf("Launching %d workers...\n", numWorkers)

	// BUFFER SIZE DECISION:
	// buffer = numWorkers * 2 ensures every worker always has a job ready when it finishes.
	// Larger buffer wastes memory with no throughput gain (workers can't process faster).
	// Smaller buffer (0 or 1) causes brief stalls when worker finishes before producer pushes.
	jobsCh := make(chan Job, numWorkers*2)

	var wg sync.WaitGroup

	// HTTP CLIENT SETUP:
	// Create ONE shared client. Do NOT create a client per worker (wastes TCP connections).
	// MaxIdleConnsPerHost must be >= numWorkers so each worker reuses its own persistent TCP connection.
	// Default is 2, which forces 158 out of 160 workers to do a fresh TCP handshake every time.
	transport := &http.Transport{
		MaxIdleConnsPerHost: numWorkers,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   5 * time.Second,
	}

	// ============ PHASE 2: LAUNCH WORKERS (before producer!) ============
	// Workers MUST start before the producer, otherwise the producer pushes into
	// the channel with nobody pulling from it, and blocks after the buffer fills.
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					// Context expired (timeout). Stop immediately.
					return
				case j, open := <-jobsCh:
					if !open {
						// Channel was closed by producer AND all remaining jobs are drained.
						// This is the normal, clean exit path.
						return
					}
					sendReq(client, j)
					atomic.AddUint64(&totalOps, 1)
				}
			}
		}(i)
	}

	// ============ PHASE 3: LAUNCH PRODUCER ============
	// ORDERING RULE: producer goroutine must launch AFTER workers.
	// If producer runs first and calls close(jobsCh) before workers start,
	// workers will see a closed channel immediately and exit (processing nothing).
	//
	// The producer runs in its own goroutine so main() can proceed to wg.Wait().
	// If the producer ran on the main goroutine, main() would block here pushing jobs,
	// and wg.Wait() (which needs to run to collect workers) would never execute — deadlock.
	go func() {
		for i := 0; i < TotalJobs; i++ {
			j := Job{
				ip:   generateFakeIP(),
				path: randomPath(),
			}
			select {
			case jobsCh <- j:
				// Job accepted into channel. If buffer is full, this blocks (backpressure).
			case <-ctx.Done():
				// Timeout hit before all jobs were produced. Stop producing.
				close(jobsCh)
				return
			}
		}
		// All jobs produced. Close the channel so workers know no more work is coming.
		// Without this close(), workers block on <-jobsCh forever — deadlock.
		close(jobsCh)
	}()

	// ============ PHASE 4: WAIT FOR ALL WORKERS ============
	// Blocks until every worker's defer wg.Done() fires.
	// At this point, either all 500k jobs are done, or the context timed out.
	wg.Wait()

	// ============ PHASE 5: RESULTS ============
	// All goroutines are dead. Safe to read atomic counters without locks.
	elapsed := time.Since(start).Seconds()
	fmt.Printf("\n--- Results ---\n")
	fmt.Printf("Total ops: %d\n", atomic.LoadUint64(&totalOps))
	fmt.Printf("Elapsed:   %.2fs\n", elapsed)
	fmt.Printf("RPS:       %.0f\n", float64(totalOps)/elapsed)

	// Fetch final stats from the server to see what the HLL estimated
	fmt.Printf("\n--- HLL Accuracy ---\n")
	fetchStats(ProductsPath)
	fetchStats(CheckoutPath)
}

// ============ HELPER FUNCTIONS ============

// generateFakeIP creates a random IPv4 address string.
// Called by the producer, NOT inside worker hot loops (pre-generated into Job struct).
func generateFakeIP() string {
	return fmt.Sprintf("%d.%d.%d.%d",
		rand.Intn(256),
		rand.Intn(256),
		rand.Intn(256),
		rand.Intn(256),
	)
}

// randomPath selects a target path with weighted probability.
// 80% of traffic hits the products page (simulates high-cardinality viral traffic).
// 20% hits checkout (simulates low-cardinality returning buyers).
// IMPORTANT: p < 80 means values 0-79 (80 values out of 100) → 80% probability.
func randomPath() string {
	if rand.Intn(100) < 80 {
		return ProductsPath
	}
	return CheckoutPath
}

// sendReq fires a single HTTP request with a spoofed IP via the X-Forwarded-For header.
// RULES:
//  1. Use http.NewRequest (not http.Get) because we need to set custom headers.
//  2. Always drain the response body with io.Copy(io.Discard, ...) before closing.
//     If you just Close() without draining, Go cannot reuse the TCP connection,
//     and you'll exhaust file descriptors after ~10,000 requests ("too many open files").
//  3. Never create http.Client inside this function — share one from main().
func sendReq(client *http.Client, j Job) {
	req, err := http.NewRequest("GET", j.path, nil)
	if err != nil {
		return
	}
	req.Header.Set("X-Forwarded-For", j.ip)
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
}

// fetchStats queries the server's metrics endpoint and prints the HLL estimate for a given path.
// Called once after the attack, to see how accurately the HLL counted unique visitors.
func fetchStats(targetPath string) {
	// The stats endpoint expects a query param: /internal/stats?target=/api/v1/products/list
	// We need to extract just the path portion from the full URL constant.
	// e.g., "http://localhost:8080/api/v1/products/list" → "/api/v1/products/list"
	url := StatsPath + "?target=" + targetPath[len("http://localhost:8080"):]

	resp, err := http.Get(url)
	if err != nil {
		fmt.Printf("Failed to fetch stats for %s: %v\n", targetPath, err)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("%s", body)
}
