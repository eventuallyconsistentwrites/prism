package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"net/http"

	"github.com/eventuallyconsistentwrites/prism/internal/analytics"
	"github.com/eventuallyconsistentwrites/prism/internal/tracker"
)

const (
	//read-heavy route, most visited(high cardinality)
	ProductsAPIPath = "/api/v1/products/list"

	//relatively lower cardinality path
	CheckoutPath = "/api/v1/checkout"

	MetricsPath = "/internal/stats" //stats path, not tracked
)

func main() {

	benchmark := flag.Bool("benchmark", false, "enable exact counting for accuracy comparison")
	flag.Parse()
	tr := tracker.NewTracker(0.01, *benchmark) //hll with 1% error rate

	mux := http.NewServeMux()

	ProductsHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Products page"))
	})

	CheckoutHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Checkout page"))
	})

	// mux.HandleFunc("/admin/benchmark/on", func(w http.ResponseWriter, r *http.Request) {
	// 	tr.EnableBenchmarking()
	// 	w.Write([]byte("Benchmarking Enabled!Exact comparisons are now tracking."))
	// })

	// mux.HandleFunc("/admin/benchmark/off", func(w http.ResponseWriter, r *http.Request) {
	// 	tr.DisableBenchmarking()
	// 	w.Write([]byte("Benchmarking Disabled!"))
	// })

	mux.Handle(ProductsAPIPath, analytics.TrackingMiddleware(tr, ProductsHandler))

	mux.Handle(CheckoutPath, analytics.TrackingMiddleware(tr, CheckoutHandler))

	mux.HandleFunc(MetricsPath, func(w http.ResponseWriter, r *http.Request) {
		//estimate the unique visitors on each path
		//query will be of type: GET /internal/stats?target=/api/v1/products/list
		target := r.URL.Query().Get("target")
		if target == "" {
			http.Error(w, "missing target query param", http.StatusBadRequest) //throw 400
			return
		}
		estimate, exact, exists := tr.Estimate(target)

		if !exists {
			fmt.Fprintf(w, "Total Unique Visitors for %s: 0\n", target)
			return
		}
		//becnhmarking is on
		if exact > 0 {
			fmt.Fprintf(w, "HLL Estimate: %d | Exact: %d | Error: %.2f%%\n", estimate, exact, math.Abs(float64(estimate)-float64(exact))/float64(exact)*100)
		}
		fmt.Fprintf(w, "Total Unique Visitors for %s: %d\n", target, estimate)
	})

	var serverRunningMsg string
	if *benchmark {
		serverRunningMsg = "Server running on :8080 with benchmarking"
	} else {
		serverRunningMsg = "Server running on :8080"
	}
	log.Println(serverRunningMsg)
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
