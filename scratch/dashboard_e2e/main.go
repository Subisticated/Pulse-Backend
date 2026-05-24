package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type logPayload struct {
	Endpoint    string                 `json:"endpoint"`
	Method      string                 `json:"method"`
	Status      int                    `json:"status"`
	Latency     int                    `json:"latency"`
	Error       string                 `json:"error,omitempty"`
	Service     string                 `json:"service"`
	Environment string                 `json:"environment"`
	TraceID     string                 `json:"traceId,omitempty"`
	Meta        map[string]interface{} `json:"meta,omitempty"`
}

const (
	baseURL   = "http://localhost:8080"
	apiPrefix = baseURL + "/api/v1"
)

func main() {
	fmt.Println("🌟 Pulse E2E Dashboard API Test Suite")
	fmt.Println("=========================================================")

	_ = godotenv.Load()
	_ = godotenv.Load("../.env")
	_ = godotenv.Load("../../.env")

	mongoURI := os.Getenv("MONGO_URI")
	dbName := os.Getenv("DB_NAME")
	if dbName == "" {
		dbName = "pulse"
	}

	// ── Step 1: Pre-clean MongoDB Collections ────────────────────────────────
	fmt.Println("🧹 Preparing MongoDB collection states...")
	tlsConf := &tls.Config{InsecureSkipVerify: true}
	mongoClient, err := mongo.Connect(context.Background(),
		options.Client().ApplyURI(mongoURI).SetTLSConfig(tlsConf).
			SetServerSelectionTimeout(10*time.Second))
	if err != nil {
		fmt.Printf("❌ Mongo connect failed: %v\n", err)
		return
	}
	defer mongoClient.Disconnect(context.Background())

	db := mongoClient.Database(dbName)
	_ = db.Collection("logs").Drop(context.Background())
	_ = db.Collection("incidents").Drop(context.Background())
	fmt.Println("✅ Database collection states ready.")

	client := &http.Client{Timeout: 5 * time.Second}

	// ── Step 2: Test Health Endpoint ─────────────────────────────────────────
	fmt.Println("\n🩺 Testing Health Endpoint...")
	resp, err := client.Get(apiPrefix + "/health")
	if err != nil {
		fmt.Printf("❌ Health check request failed: %v\n", err)
		return
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	fmt.Printf("   HTTP %d → %s\n", resp.StatusCode, string(body))

	// ── Step 3: Ingest Multi-Service Telemetry logs ──────────────────────────
	fmt.Println("\n📨 Ingesting multi-service logs to populate dashboard...")
	services := []string{"checkout", "payments", "auth", "inventory"}
	endpoints := map[string]string{
		"checkout":  "/api/v1/checkout",
		"payments":  "/api/v1/payments/capture",
		"auth":      "/api/v1/auth/login",
		"inventory": "/api/v1/inventory/items",
	}

	rand.Seed(time.Now().UnixNano())

	// Send 60 successful logs across services
	for i := 0; i < 60; i++ {
		svc := services[rand.Intn(len(services))]
		payload := logPayload{
			Endpoint:    endpoints[svc],
			Method:      "POST",
			Status:      200,
			Latency:     50 + rand.Intn(100),
			Service:     svc,
			Environment: "production",
			TraceID:     fmt.Sprintf("trace-%d", i),
			Meta:        map[string]interface{}{"userId": fmt.Sprintf("user-%d", i)},
		}
		sendIngestRequest(client, payload)
	}

	// Trigger a latency spike on payments service (> 1000ms avg trigger)
	fmt.Println("\n⚠️  Sending heavy latency spike logs on 'payments' service...")
	for i := 0; i < 10; i++ {
		sendIngestRequest(client, logPayload{
			Endpoint:    "/api/v1/payments/capture",
			Method:      "POST",
			Status:      200,
			Latency:     1250 + rand.Intn(100),
			Service:     "payments",
			Environment: "production",
		})
	}

	// Trigger high error counts on checkout service (>= 5 failures within 5 mins)
	fmt.Println("\n⚠️  Sending high error storm logs on 'checkout' service...")
	for i := 0; i < 6; i++ {
		sendIngestRequest(client, logPayload{
			Endpoint:    "/api/v1/checkout",
			Method:      "POST",
			Status:      500,
			Latency:     120,
			Error:       "payments connection lost",
			Service:     "checkout",
			Environment: "production",
		})
	}

	fmt.Println("\n⏳ Waiting 3.5s for queues to batch-flush and detector to process...")
	time.Sleep(3500 * time.Millisecond)

	// ── Step 4: Test GET /api/v1/metrics ─────────────────────────────────────
	fmt.Println("\n📊 Fetching dashboard aggregated metrics...")
	resp, err = client.Get(apiPrefix + "/metrics")
	if err != nil {
		fmt.Printf("❌ Metrics request failed: %v\n", err)
		return
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	prettyPrintJSON("Aggregated Metrics Response", body)

	// ── Step 5: Test GET /api/v1/incidents ───────────────────────────────────
	fmt.Println("\n🚨 Listing dashboard SRE incident feed...")
	resp, err = client.Get(apiPrefix + "/incidents?status=active")
	if err != nil {
		fmt.Printf("❌ Incidents request failed: %v\n", err)
		return
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	prettyPrintJSON("Incidents Feed Response", body)

	var incidentsList []struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(body, &incidentsList)

	// ── Step 6: Test POST /api/v1/rca (AI diagnostics) ───────────────────────
	if len(incidentsList) > 0 {
		targetID := incidentsList[0].ID
		fmt.Printf("\n🤖 Requesting AI Root Cause Analysis for Incident %s...\n", targetID)
		rcaPayload := map[string]string{"incidentId": targetID, "requester": "ui"}
		jsonB, _ := json.Marshal(rcaPayload)
		resp, err = client.Post(apiPrefix+"/rca", "application/json", bytes.NewBuffer(jsonB))
		if err != nil {
			fmt.Printf("❌ RCA request failed: %v\n", err)
			return
		}
		body, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		prettyPrintJSON("AI Root Cause Analysis Response", body)
	} else {
		fmt.Println("\n⚠️  No incidents found to run RCA test against.")
	}

	// ── Step 7: Test GET /api/v1/topology ────────────────────────────────────
	fmt.Println("\n🕸️  Querying service topology dependency graph...")
	resp, err = client.Get(apiPrefix + "/topology")
	if err != nil {
		fmt.Printf("❌ Topology request failed: %v\n", err)
		return
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	prettyPrintJSON("Service Topology Graph", body)

	// ── Step 8: Test Chaos / Demo Scenarios ──────────────────────────────────
	fmt.Println("\n💥 Activating Chaos Scenario: 'payment_failure'...")
	chaosPayload := map[string]interface{}{"duration": 45}
	jsonC, _ := json.Marshal(chaosPayload)
	resp, err = client.Post(baseURL+"/chaos/payment_failure", "application/json", bytes.NewBuffer(jsonC))
	if err != nil {
		fmt.Printf("❌ Chaos activation failed: %v\n", err)
		return
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	fmt.Printf("   Activation response: %s\n", string(body))

	fmt.Println("\n🔍 Checking Chaos Status...")
	resp, err = client.Get(baseURL + "/chaos/status")
	if err != nil {
		fmt.Printf("❌ Chaos status retrieval failed: %v\n", err)
		return
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	fmt.Printf("   Chaos status: %s\n", string(body))

	fmt.Println("\n=========================================================")
	fmt.Println("🎉 Dashboard API Test Suite completed successfully!")
}

func sendIngestRequest(client *http.Client, payload logPayload) {
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", apiPrefix+"/logs", bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}

func prettyPrintJSON(title string, body []byte) {
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, body, "", "  "); err == nil {
		fmt.Printf("--- %s ---\n%s\n---------------------------------------\n", title, pretty.String())
	} else {
		fmt.Printf("--- %s (Raw) ---\n%s\n---------------------------------------\n", title, string(body))
	}
}
