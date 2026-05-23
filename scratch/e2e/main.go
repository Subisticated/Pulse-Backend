package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
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

type metricsResponse struct {
	TotalRequests    int64   `json:"totalRequests"`
	ErrorRate        float64 `json:"errorRate"`
	AvgLatency       float64 `json:"avgLatency"`
	RequestsLastHour int64   `json:"requestsLastHour"`
	ErrorsLastHour   int64   `json:"errorsLastHour"`
}

type incident struct {
	ID          string    `json:"id"`
	Severity    string    `json:"severity"`
	Cause       string    `json:"cause"`
	Description string    `json:"description"`
	Service     string    `json:"service"`
	Environment string    `json:"environment"`
	Resolved    bool      `json:"resolved"`
	CreatedAt   time.Time `json:"createdAt"`
}

type rcaResponse struct {
	IncidentID  string   `json:"incidentId"`
	Cause       string   `json:"cause"`
	Confidence  int      `json:"confidence"`
	Evidence    []string `json:"evidence"`
	Fixes       []string `json:"fixes"`
	GeneratedAt string   `json:"generatedAt"`
}

func main() {
	_ = godotenv.Load()
	_ = godotenv.Load("../.env")
	_ = godotenv.Load("../../.env")

	mongoURI := os.Getenv("MONGO_URI")
	dbName := os.Getenv("DB_NAME")
	if mongoURI == "" {
		fmt.Println("❌ Error: MONGO_URI is not set in .env")
		return
	}
	if dbName == "" {
		dbName = "pulse"
	}

	fmt.Println("🧹 Step 1: Cleaning up MongoDB database for clean testing...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tlsConfig := &tls.Config{InsecureSkipVerify: true}
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI).SetTLSConfig(tlsConfig))
	if err != nil {
		fmt.Printf("❌ DB cleanup failed: %v\n", err)
		return
	}
	db := client.Database(dbName)
	_ = db.Collection("logs").Drop(ctx)
	_ = db.Collection("incidents").Drop(ctx)
	_ = client.Disconnect(ctx)
	fmt.Println("✅ Database reset complete.")

	// Wait 2 seconds for server to be ready
	time.Sleep(2 * time.Second)

	// Step 2: Health Check
	fmt.Println("\n🏥 Step 2: Querying server health check...")
	resp, err := http.Get("http://localhost:8080/")
	if err != nil {
		fmt.Printf("❌ Health check request failed: %v\n", err)
		return
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	fmt.Printf("📡 Health Response: %s\n", string(body))

	// Step 3: Log Ingestions
	fmt.Println("\n📥 Step 3: Ingesting telemetry log waves...")
	
	// Ingest 5 successful requests
	fmt.Println("  -> Ingesting 5 successful logs (status 200, latency 80ms)...")
	for i := 0; i < 5; i++ {
		ingestLog(logPayload{
			Endpoint:    "/api/users",
			Method:      "GET",
			Status:      200,
			Latency:     80 + i*5,
			Service:     "checkout",
			Environment: "production",
		})
	}

	// Ingest 1 high latency request (>1000ms) -> triggers latency_spike (Medium)
	fmt.Println("  -> Ingesting 1 high latency log (status 200, latency 1250ms)...")
	ingestLog(logPayload{
		Endpoint:    "/api/checkout",
		Method:      "POST",
		Status:      200,
		Latency:     1250,
		Service:     "checkout",
		Environment: "production",
	})

	// Ingest 6 server error requests (status 500) -> triggers high_error_rate (Critical)
	fmt.Println("  -> Ingesting 6 server error logs (status 500, latency 200ms)...")
	for i := 0; i < 6; i++ {
		ingestLog(logPayload{
			Endpoint:    "/api/pay",
			Method:      "POST",
			Status:      500,
			Latency:     220,
			Error:       "DB connection timeout after 3000ms",
			Service:     "checkout",
			Environment: "production",
		})
	}

	// Wait 3 seconds for asynchronous incident detection tasks to execute in background
	fmt.Println("\n⏱️ Waiting 3 seconds for async anomaly detector execution...")
	time.Sleep(3 * time.Second)

	// Step 4: Realtime Metrics Aggregations
	fmt.Println("\n📊 Step 4: Querying Realtime Aggregated Performance Metrics...")
	resp, err = http.Get("http://localhost:8080/api/v1/metrics")
	if err == nil {
		var metrics metricsResponse
		_ = json.NewDecoder(resp.Body).Decode(&metrics)
		resp.Body.Close()
		fmt.Printf("✅ Success!\n")
		fmt.Printf("  - Total Requests: %d\n", metrics.TotalRequests)
		fmt.Printf("  - Avg Latency:    %.2f ms\n", metrics.AvgLatency)
		fmt.Printf("  - Error Rate:     %.2f%%\n", metrics.ErrorRate)
		fmt.Printf("  - Requests (Hour): %d\n", metrics.RequestsLastHour)
		fmt.Printf("  - Errors (Hour):   %d\n", metrics.ErrorsLastHour)
	} else {
		fmt.Printf("❌ Failed to query metrics: %v\n", err)
	}

	// Step 5: Incident Detection
	fmt.Println("\n🚨 Step 5: Fetching Active Anomalies and Generated Incidents...")
	resp, err = http.Get("http://localhost:8080/api/v1/incidents")
	var incidents []incident
	if err == nil {
		_ = json.NewDecoder(resp.Body).Decode(&incidents)
		resp.Body.Close()
		fmt.Printf("✅ Success! Found %d active incidents:\n", len(incidents))
		for _, inc := range incidents {
			fmt.Printf("  - [%s] Severity: %-8s Cause: %-22s Description: %s\n",
				inc.ID, inc.Severity, inc.Cause, inc.Description)
		}
	} else {
		fmt.Printf("❌ Failed to fetch incidents: %v\n", err)
	}

	// Step 6: AI Root Cause Analysis
	if len(incidents) > 0 {
		targetIncidentID := incidents[0].ID
		fmt.Printf("\n🤖 Step 6: Performing Groq AI Root Cause Analysis (RCA) on Incident %s...\n", targetIncidentID)
		
		rcaPayload := map[string]string{"incidentId": targetIncidentID}
		jsonBytes, _ := json.Marshal(rcaPayload)
		
		resp, err = http.Post("http://localhost:8080/api/v1/rca", "application/json", bytes.NewBuffer(jsonBytes))
		if err == nil {
			var rca rcaResponse
			_ = json.NewDecoder(resp.Body).Decode(&rca)
			resp.Body.Close()
			fmt.Println("✅ Success! Groq Llama-3.1 Diagnostics Report:")
			fmt.Println("=========================================================================")
			fmt.Printf("Probable Cause: %s\n", rca.Cause)
			fmt.Printf("Confidence:     %d%%\n", rca.Confidence)
			fmt.Println("Evidence Found:")
			for _, ev := range rca.Evidence {
				fmt.Printf("  - %s\n", ev)
			}
			fmt.Println("Suggested SRE Fixes:")
			for _, fix := range rca.Fixes {
				fmt.Printf("  - %s\n", fix)
			}
			fmt.Println("=========================================================================")
		} else {
			fmt.Printf("❌ Failed to query AI RCA: %v\n", err)
		}
	} else {
		fmt.Println("❌ Step 6 skipped: No incidents generated during test.")
	}
}

func ingestLog(payload logPayload) {
	bytesData, _ := json.Marshal(payload)
	resp, err := http.Post("http://localhost:8080/api/v1/logs", "application/json", bytes.NewBuffer(bytesData))
	if err != nil {
		fmt.Printf("  ❌ Ingestion failure: %v\n", err)
		return
	}
	resp.Body.Close()
}
