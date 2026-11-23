// Minimal HTTP service for autoscaling load tests.
//
// GET /burn burns ~BURN_MS milliseconds of CPU (busy loop) and returns
// "ok" — this makes per-request CPU cost measurable and tunable, which
// HPA (CPU utilization) and the LLMAutoscaler (metrics) both react to.
// GET /healthz returns immediately (readiness probe).
package main

import (
	"fmt"
	"math"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"time"
)

var burnMS int64

// sink prevents the compiler from optimizing the busy loop away.
var sink float64

func burn(ms int64) {
	deadline := time.Now().Add(time.Duration(ms) * time.Millisecond)
	x := 1.0
	for time.Now().Before(deadline) {
		for i := 0; i < 20000; i++ {
			x += math.Sqrt(x) * math.Pi
		}
	}
	sink = x
}

func burnHandler(w http.ResponseWriter, r *http.Request) {
	burn(burnMS)
	fmt.Fprint(w, "ok")
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "healthy")
}

func main() {
	burnMS = 50
	if v := os.Getenv("BURN_MS"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			burnMS = n
		}
	}
	runtime.GOMAXPROCS(runtime.NumCPU())
	http.HandleFunc("/burn", burnHandler)
	http.HandleFunc("/healthz", healthHandler)
	http.ListenAndServe(":8080", nil)
}
