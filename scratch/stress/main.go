package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type logPayload struct {
	Endpoint    string `json:"endpoint"`
	Method      string `json:"method"`
	Status      int    `json:"status"`
	Latency     int    `json:"latency"`
	Error       string `json:"error,omitempty"`
	Service     string `json:"service"`
	Environment string `json:"environment"`
}

const (
	totalRequests = 1500
	concurrency   = 50 // Increased to stress test at higher load
	targetURL     = "http://localhost:8080/api/v1/logs"
)

func main() {
	fmt.Println("🚀 Pulse Backend — High-Throughput Stress Test")
	fmt.Printf("📦 Volume:      %d HTTP logs\n", totalRequests)
	fmt.Printf("🧵 Concurrency: %d concurrent workers\n", concurrency)
	fmt.Println("📡 Target API:  POST /api/v1/logs")
	fmt.Println("---------------------------------------------------------")

	endpoints := []string{"/api/v1/users", "/api/v1/checkout", "/api/v1/billing", "/api/v1/products"}
	methods := []string{"GET", "POST", "PUT", "DELETE"}
	services := []string{"auth", "checkout", "inventory", "notification"}

	rand.Seed(time.Now().UnixNano())

	// Pre-generate all payloads
	payloads := make([]logPayload, totalRequests)
	for i := 0; i < totalRequests; i++ {
		roll := rand.Intn(100)
		var status, latency int
		var errMsg string

		switch {
		case roll < 80:
			status = 200
			latency = 20 + rand.Intn(80)
		case roll < 90:
			status = 200
			latency = 1000 + rand.Intn(500)
		default:
			status = 500
			latency = 100 + rand.Intn(100)
			errMsg = "DB connection timeout"
		}

		payloads[i] = logPayload{
			Endpoint:    endpoints[rand.Intn(len(endpoints))],
			Method:      methods[rand.Intn(len(methods))],
			Status:      status,
			Latency:     latency,
			Error:       errMsg,
			Service:     services[rand.Intn(len(services))],
			Environment: "production",
		}
	}

	jobs := make(chan logPayload, totalRequests)
	for _, p := range payloads {
		jobs <- p
	}
	close(jobs)

	var successCount int64
	var failureCount int64
	var statusCodes sync.Map // track response code distribution

	// Shared HTTP client with pooled connections
	client := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        concurrency * 2,
			MaxIdleConnsPerHost: concurrency,
			IdleConnTimeout:     90 * time.Second,
			DisableKeepAlives:   false,
		},
		Timeout: 10 * time.Second,
	}

	start := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for payload := range jobs {
				jsonBytes, _ := json.Marshal(payload)
				req, err := http.NewRequest("POST", targetURL, bytes.NewBuffer(jsonBytes))
				if err != nil {
					atomic.AddInt64(&failureCount, 1)
					continue
				}
				req.Header.Set("Content-Type", "application/json")

				resp, err := client.Do(req)
				if err != nil {
					atomic.AddInt64(&failureCount, 1)
					continue
				}
				resp.Body.Close()

				code := resp.StatusCode
				actual, _ := statusCodes.LoadOrStore(code, new(int64))
				atomic.AddInt64(actual.(*int64), 1)

				// Accept both 201 (sync) and 202 (async queued)
				if code == http.StatusCreated || code == http.StatusAccepted {
					atomic.AddInt64(&successCount, 1)
				} else {
					atomic.AddInt64(&failureCount, 1)
				}
			}
		}()
	}

	wg.Wait()
	duration := time.Since(start)
	rps := float64(totalRequests) / duration.Seconds()
	successRate := float64(successCount) / float64(totalRequests) * 100

	fmt.Println("---------------------------------------------------------")
	fmt.Println("📊 Stress Test Results:")
	fmt.Printf("⏱️  Total Duration:     %v\n", duration.Round(time.Millisecond))
	fmt.Printf("✅ Success Logs:        %d / %d\n", successCount, totalRequests)
	fmt.Printf("❌ Failed Requests:     %d\n", failureCount)
	fmt.Printf("📈 Success Rate:        %.2f%%\n", successRate)
	fmt.Printf("🚀 Throughput (RPS):    %.2f requests/sec\n", rps)
	fmt.Println()
	fmt.Println("HTTP Response Code Distribution:")
	statusCodes.Range(func(k, v interface{}) bool {
		fmt.Printf("  HTTP %d → %d responses\n", k, atomic.LoadInt64(v.(*int64)))
		return true
	})
	fmt.Println("=========================================================")
	fmt.Println("🎉 Stress Test Completed!")
}
