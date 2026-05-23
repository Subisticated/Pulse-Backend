package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
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

func main() {
	_ = godotenv.Load()
	_ = godotenv.Load("../.env")
	_ = godotenv.Load("../../.env")

	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		fmt.Println("❌ MONGO_URI not set")
		return
	}

	totalLogs := 1500
	concurrency := 50
	targetURL := "http://localhost:8080/api/v1/logs"

	// ── Step 1: Drop collection for clean count ───────────────────────────────
	fmt.Println("🧹 Dropping logs collection for clean count test...")
	tlsConf := &tls.Config{InsecureSkipVerify: true}
	mongoClient, err := mongo.Connect(context.Background(),
		options.Client().ApplyURI(mongoURI).SetTLSConfig(tlsConf).
			SetServerSelectionTimeout(30*time.Second))
	if err != nil {
		fmt.Printf("❌ Mongo connect failed: %v\n", err)
		return
	}
	defer mongoClient.Disconnect(context.Background())

	db := mongoClient.Database(os.Getenv("DB_NAME"))
	if db.Name() == "" {
		db = mongoClient.Database("pulse")
	}
	_ = db.Collection("logs").Drop(context.Background())
	fmt.Println("✅ Collection dropped. Starting with 0 documents.")

	// ── Step 2: Stress 1500 logs ──────────────────────────────────────────────
	fmt.Printf("\n🚀 Sending %d logs with %d workers...\n", totalLogs, concurrency)

	endpoints := []string{"/api/checkout", "/api/users", "/api/billing", "/api/products"}
	methods := []string{"GET", "POST", "PUT"}
	services := []string{"auth", "checkout", "inventory"}
	rand.Seed(time.Now().UnixNano())

	payloads := make([]logPayload, totalLogs)
	for i := range payloads {
		roll := rand.Intn(100)
		status, latency := 200, 30+rand.Intn(70)
		errMsg := ""
		if roll >= 90 {
			status = 500
			latency = 100 + rand.Intn(50)
			errMsg = "DB timeout"
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

	jobs := make(chan logPayload, totalLogs)
	for _, p := range payloads {
		jobs <- p
	}
	close(jobs)

	var accepted, rejected int64
	client := &http.Client{
		Transport: &http.Transport{
			MaxIdleConnsPerHost: concurrency,
			IdleConnTimeout:     90 * time.Second,
		},
		Timeout: 5 * time.Second,
	}

	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobs {
				b, _ := json.Marshal(p)
				req, _ := http.NewRequest("POST", targetURL, bytes.NewBuffer(b))
				req.Header.Set("Content-Type", "application/json")
				resp, err := client.Do(req)
				if err != nil {
					atomic.AddInt64(&rejected, 1)
					continue
				}
				resp.Body.Close()
				if resp.StatusCode == 202 || resp.StatusCode == 201 {
					atomic.AddInt64(&accepted, 1)
				} else {
					atomic.AddInt64(&rejected, 1)
				}
			}
		}()
	}
	wg.Wait()
	duration := time.Since(start)

	fmt.Printf("⏱  Duration: %v | ✅ Accepted: %d | ❌ Rejected: %d | RPS: %.0f\n",
		duration.Round(time.Millisecond), accepted, rejected,
		float64(totalLogs)/duration.Seconds())

	// ── Step 3: Wait for queue batch flush ────────────────────────────────────
	fmt.Println("\n⏳ Waiting 3s for batch worker to flush queue to MongoDB...")
	time.Sleep(3 * time.Second)

	// ── Step 4: Count documents in Mongo ─────────────────────────────────────
	fmt.Println("\n📊 Counting persisted documents in MongoDB...")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	count, err := db.Collection("logs").CountDocuments(ctx, bson.M{})
	if err != nil {
		fmt.Printf("❌ Count failed: %v\n", err)
		return
	}

	lost := int64(totalLogs) - count
	persistRate := float64(count) / float64(totalLogs) * 100

	fmt.Println("=========================================================")
	fmt.Printf("📨 Logs sent to API:    %d\n", totalLogs)
	fmt.Printf("✅ API accepted:         %d\n", accepted)
	fmt.Printf("💾 Persisted in Mongo:   %d\n", count)
	fmt.Printf("💀 Lost in transit:      %d\n", lost)
	fmt.Printf("📈 Persistence rate:     %.2f%%\n", persistRate)

	if count == int64(totalLogs) {
		fmt.Println("\n🎉 PERFECT — All 1500 logs persisted to MongoDB!")
	} else if persistRate >= 99.0 {
		fmt.Printf("\n✅ EXCELLENT — %.2f%% persistence (minor batch timing loss)\n", persistRate)
	} else if persistRate >= 95.0 {
		fmt.Printf("\n⚠️  ACCEPTABLE — %.2f%% persistence (some queue drops)\n", persistRate)
	} else {
		fmt.Printf("\n❌ NEEDS FIX — %.2f%% persistence (significant queue drops)\n", persistRate)
	}
	fmt.Println("=========================================================")
}
