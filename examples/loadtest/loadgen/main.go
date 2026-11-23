// Fixed-rate load generator with a piecewise-linear RPS schedule and
// latency recording, for autoscaling comparisons.
//
//   -schedule "0:10,120:40,300:40,540:10"   t(seconds):rps pairs, linear ramp
//   -duration 720                            total seconds (clips schedule)
//   -workers 64 -timeout 5s -out per-second JSONL, -summary summary JSON
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"
)

type seg struct {
	t   float64
	rps float64
}

func parseSchedule(s string) ([]seg, error) {
	var out []seg
	var last float64
	first := true
	for _, part := range splitComma(s) {
		var t, r float64
		if _, err := fmt.Sscanf(part, "%f:%f", &t, &r); err != nil {
			return nil, err
		}
		if !first && t <= last {
			return nil, fmt.Errorf("schedule times must increase")
		}
		out = append(out, seg{t, r})
		last = t
		first = false
	}
	return out, nil
}

func splitComma(s string) []string {
	var out []string
	cur := ""
	for _, c := range s {
		if c == ',' {
			out = append(out, cur)
			cur = ""
		} else {
			cur += string(c)
		}
	}
	out = append(out, cur)
	return out
}

func rateAt(segs []seg, t float64) float64 {
	if t <= segs[0].t {
		return segs[0].rps
	}
	for i := 1; i < len(segs); i++ {
		if t <= segs[i].t {
			a, b := segs[i-1], segs[i]
			f := (t - a.t) / (b.t - a.t)
			return a.rps + f*(b.rps-a.rps)
		}
	}
	return segs[len(segs)-1].rps
}

type rec struct {
	lat float64
	ok  bool
	t   float64
}

func main() {
	url := flag.String("url", "http://burn.default.svc:8080/burn", "target URL")
	schedS := flag.String("schedule", "0:10,120:40,300:40,540:10", "t:rps pairs")
	dur := flag.Float64("duration", 720, "seconds")
	workers := flag.Int("workers", 64, "concurrent workers")
	timeout := flag.Duration("timeout", 10*time.Second, "per-request timeout")
	out := flag.String("out", "persecond.jsonl", "per-second JSONL")
	summary := flag.String("summary", "summary.json", "summary JSON")
	flag.Parse()

	segs, err := parseSchedule(*schedS)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bad schedule:", err)
		os.Exit(1)
	}

	client := &http.Client{Timeout: *timeout,
		Transport: &http.Transport{MaxIdleConnsPerHost: *workers * 2}}

	var mu sync.Mutex
	recs := make([]rec, 0, 40000)
	stop := make(chan struct{})
	tokens := make(chan struct{}, 4096)

	// pacer: refill tokens per 100ms tick according to the schedule
	start := time.Now()
	go func() {
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				t := time.Since(start).Seconds()
				n := int(rateAt(segs, t) * 0.1)
				for i := 0; i < n; i++ {
					select {
					case tokens <- struct{}{}:
					default:
					}
				}
			}
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				case <-tokens:
				}
				t := time.Since(start).Seconds()
				reqStart := time.Now()
				ok := false
				func() {
					defer func() { recover() }()
					resp, err := client.Get(*url)
					if err == nil {
						resp.Body.Close()
						ok = resp.StatusCode == http.StatusOK
					}
				}()
				mu.Lock()
				recs = append(recs, rec{time.Since(reqStart).Seconds(), ok, t})
				mu.Unlock()
			}
		}()
	}

	time.Sleep(time.Duration(*dur * float64(time.Second)))
	close(stop)
	// drain a moment for in-flight requests
	time.Sleep(2 * time.Second)
	wg.Wait()

	// per-second aggregates
	perSec := map[int][]float64{}
	errs := map[int]int{}
	for _, r := range recs {
		s := int(r.t)
		perSec[s] = append(perSec[s], r.lat)
		if !r.ok {
			errs[s]++
		}
	}
	f, _ := os.Create(*out)
	defer f.Close()
	enc := json.NewEncoder(f)
	seconds := int(*dur)
	for s := 0; s < seconds; s++ {
		lats := perSec[s]
		stats := pct(lats, []float64{0.5, 0.95, 0.99})
		enc.Encode(map[string]any{
			"t": s, "rps": len(lats), "errors": errs[s],
			"p50": round(stats[0]), "p95": round(stats[1]), "p99": round(stats[2]),
			"avg": round(avg(lats)),
		})
	}

	// overall + peak window (300–540 s)
	peak := make([]float64, 0)
	peakErrs := 0
	for _, r := range recs {
		if r.t >= 300 && r.t < 540 {
			if r.ok {
				peak = append(peak, r.lat)
			} else {
				peakErrs++
			}
		}
	}
	sort.Float64s(peak)
	all := make([]float64, 0, len(recs))
	okAll := 0
	for _, r := range recs {
		if r.ok {
			all = append(all, r.lat)
			okAll++
		}
	}
	sort.Float64s(all)
	sum := map[string]any{
		"total_requests": len(recs), "ok": okAll, "errors": len(recs) - okAll,
		"overall_p50": pctOf(all, 0.5), "overall_p95": pctOf(all, 0.95),
		"overall_p99": pctOf(all, 0.99), "overall_avg": avg(all),
		"peak_p50": pctOf(peak, 0.5), "peak_p95": pctOf(peak, 0.95),
		"peak_p99": pctOf(peak, 0.99), "peak_avg": avg(peak),
		"peak_errors": peakErrs,
	}
	sf, _ := os.Create(*summary)
	defer sf.Close()
	json.NewEncoder(sf).Encode(sum)
	fmt.Printf("done: %d requests, %d errors\n", len(recs), len(recs)-okAll)
}

func pct(lats []float64, qs []float64) []float64 {
	s := append([]float64{}, lats...)
	sort.Float64s(s)
	out := make([]float64, len(qs))
	for i, q := range qs {
		out[i] = pctOf(s, q)
	}
	return out
}

func pctOf(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(q * float64(len(sorted))))
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func avg(l []float64) float64 {
	if len(l) == 0 {
		return 0
	}
	s := 0.0
	for _, v := range l {
		s += v
	}
	return s / float64(len(l))
}

func round(v float64) float64 { return math.Round(v*1000) / 1000 }
