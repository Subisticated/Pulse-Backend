# 🚀 Pulse Backend

AI-powered API monitoring and observability backend. High-throughput telemetry ingestion pipeline with real-time incident detection, Groq AI root cause analysis, and a fully-featured dashboard API.

---

## Architecture

```
SDK / Client
    ↓  POST /api/v1/logs
Gin HTTP Server (Release Mode)
    ↓  non-blocking enqueue
Async Log Queue (8192-slot buffered channel)
    ↓  InsertMany every 50 logs or 500ms
MongoDB Atlas (indexed collections)
    ↓  parallel
Rule-based Incident Detector → WebSocket Hub → Dashboard
    ↓  on demand
Groq AI RCA (llama-3.1-8b-instant)
```

---

## Quick Start

```bash
cp .env.example .env
# Fill in MONGO_URI and GROQ_API_KEY

go run cmd/server/main.go
```

Server starts on `http://localhost:8080`

---

## Environment Variables

| Variable | Required | Description |
|---|---|---|
| `PORT` | No | HTTP port (default: `8080`) |
| `MONGO_URI` | **Yes** | MongoDB Atlas connection string |
| `DB_NAME` | No | MongoDB database name (default: `pulse`) |
| `GROQ_API_KEY` | No | Groq API key for AI RCA (falls back to local analysis) |
| `DISCORD_WEBHOOK` | No | Discord webhook URL for critical incident alerts |

---

## API Reference

### Differentiation by API Type

| Type | Prefix | Consumers |
|---|---|---|
| **Telemetry Ingestion** | `POST /api/v1/logs` | SDK only |
| **Dashboard Data** | `GET /api/v1/*` | Frontend dashboard |
| **AI Diagnostics** | `POST /api/v1/rca` | Dashboard + SDK |
| **Demo/Chaos** | `/chaos/*` | Demo UI only |
| **Real-time** | `ws://host/ws` | Dashboard WebSocket |

---

### GET /api/v1/health
Health check for dashboard to detect backend availability.

**Response 200:**
```json
{
  "status": "ok",
  "version": "2.0.0",
  "uptime": 12345
}
```

---

### POST /api/v1/logs — SDK Endpoint
**Primary SDK endpoint.** Accepts structured telemetry from the Pulse Node/Python SDK. Non-blocking — returns `202 Accepted` immediately while persisting in background.

**Request body:**
```json
{
  "endpoint": "/api/cart/checkout",
  "method": "POST",
  "status": 504,
  "latency": 1200,
  "error": "upstream timeout",
  "service": "checkout",
  "environment": "production",
  "traceId": "abc-xyz-123",
  "meta": { "userId": "user_123", "region": "us-east-1" }
}
```

> `latencyMs` is also accepted as a field alias for `latency`.

**Response 202:**
```json
{
  "id": "664fa721be263ad20b601ad8",
  "timestamp": "2026-05-24T10:00:00Z",
  "queued": true
}
```

**Error responses:**
- `400 Bad Request` — missing required fields (`endpoint`, `method`, `status`)
- `503 Service Unavailable` — queue full (retry after brief backoff)

---

### GET /api/v1/metrics
Aggregated time-series and current metrics for the dashboard.

**Query params (all optional):**

| Param | Format | Default | Description |
|---|---|---|---|
| `start` | ISO 8601 | 5 min ago | Window start |
| `end` | ISO 8601 | now | Window end |

**Response 200:**
```json
{
  "start": "2026-05-24T10:00:00Z",
  "end": "2026-05-24T10:05:00Z",
  "rps": 3.4,
  "rpm": 204.0,
  "totalRequests": 1024,
  "errorRate": 4.2,
  "avgLatencyMs": 312.0,
  "p95LatencyMs": 980.0,
  "requestsLastHour": 4800,
  "errorsLastHour": 192,
  "latencySeries": [
    { "t": 1748080800000, "value": 200 },
    { "t": 1748080860000, "value": 312 }
  ],
  "errorSeries": [
    { "t": 1748080800000, "value": 2 },
    { "t": 1748080860000, "value": 5 }
  ],
  "byService": {
    "payments":  { "errorRate": 12.3, "avgLatencyMs": 1200.0 },
    "checkout":  { "errorRate": 1.4,  "avgLatencyMs": 340.0  }
  }
}
```

- `latencySeries` and `errorSeries` are aligned 1-minute buckets.
- `t` values are Unix epoch **milliseconds**.

---

### GET /api/v1/incidents
Recent and active incidents for the timeline and incident cards.

**Query params:**

| Param | Values | Default | Description |
|---|---|---|---|
| `status` | `active` \| `resolved` \| `all` | `all` | Filter by incident status |
| `limit` | integer | `50` | Maximum results |
| `since` | ISO 8601 | — | Only incidents after this time |

**Response 200:**
```json
[
  {
    "id": "664fa721be263ad20b601ad8",
    "title": "Error spike detected on checkout",
    "detail": "6 HTTP 5xx errors in the last 5 minutes on service 'checkout'",
    "severity": "critical",
    "status": "active",
    "service": "checkout",
    "services": ["checkout"],
    "environment": "production",
    "startTime": "2026-05-24T10:02:12Z",
    "endTime": null,
    "resolved": false,
    "links": {
      "rca": "/api/v1/rca?incident=664fa721be263ad20b601ad8"
    }
  }
]
```

---

### PATCH /api/v1/incidents/:id/resolve
Mark an active incident as resolved.

**Response 200:** Full updated incident object (same shape as incidents list item).

---

### POST /api/v1/rca
Request an AI-generated root cause analysis from Groq Llama-3.1.

**Request body:**
```json
{
  "incidentId": "664fa721be263ad20b601ad8",
  "requester": "ui"
}
```

**Response 200:**
```json
{
  "incidentId": "664fa721be263ad20b601ad8",
  "rca": {
    "summary": "Database connection timeout due to high load on checkout service",
    "probableRootCause": "Database connection timeout due to high load on checkout service",
    "evidence": [
      "DB connection timeout after 3000ms",
      "high error rate on checkout service"
    ],
    "suggestedFix": "Increase database connection pool size; Implement circuit breaker pattern",
    "confidence": 0.90
  },
  "generatedAt": "2026-05-24T10:02:20Z"
}
```

> `confidence` is a `0.0–1.0` float. Falls back to local deterministic analysis if `GROQ_API_KEY` is not set.

---

### GET /api/v1/topology
Service dependency graph for topology view.

**Response 200:**
```json
{
  "services": [
    {
      "id": "payments",
      "name": "Payments",
      "status": "degraded",
      "meta": { "avgLatencyMs": 1200, "totalRequests": 340, "errorCount": 42 }
    },
    {
      "id": "checkout",
      "name": "Checkout",
      "status": "critical",
      "meta": { "avgLatencyMs": 340, "totalRequests": 512, "errorCount": 61 }
    }
  ],
  "edges": [
    { "from": "checkout", "to": "payments", "type": "http", "avgLatencyMs": 1200 }
  ]
}
```

**Node status values:** `healthy` | `degraded` | `critical` | `unknown`

---

### POST /chaos/:scenario _(Demo only)_
Activate a named chaos scenario for dashboard demos.

**Optional body:**
```json
{ "duration": 60 }
```

**Response 200:**
```json
{ "mode": "payment_failure", "expiresAt": "2026-05-24T10:03:12Z", "expiresIn": 60 }
```

**Built-in scenarios:** `payment_failure` | `latency_spike` | `error_storm` | any custom string.

---

### GET /chaos/status _(Demo only)_
Return the currently active chaos scenario.

**Response 200:**
```json
{ "mode": "payment_failure", "expiresAt": "2026-05-24T10:03:12Z" }
```

Returns `{ "mode": "none" }` when no scenario is active.

---

### WebSocket: `ws://host/ws`
Real-time push updates for incidents and metrics.

**Server → Client message envelope:**
```json
{
  "type": "incident",
  "payload": {
    "id": "664fa721...",
    "title": "Error spike detected on checkout",
    "severity": "critical",
    "status": "active",
    "services": ["checkout"],
    "startTime": "2026-05-24T10:02:12Z"
  }
}
```

**`type` values:** `incident` | `metrics` | `topology` | `event`

---

## Autonomous AI SRE Agent API

Pulse includes an autonomous agent loop that executes the **Observe ➔ Think ➔ Plan ➔ Execute ➔ Evaluate** process on detected incidents. It runs diagnostics tools, triggers Groq AI reasoning, issues alerts, monitors recovery, and auto-resolves incidents.

### POST /agent/analyze
Trigger a new asynchronous autonomous SRE investigation on an active incident.

**Request body:**
```json
{
  "incidentId": "6a13eda106bb46e598dd4e41"
}
```

**Response 202:**
```json
{
  "sessionId": "1779690913909-4788",
  "incidentId": "6a13eda106bb46e598dd4e41",
  "state": "pending",
  "message": "Autonomous investigation started — connect to /ws for real-time updates"
}
```

---

### GET /agent/status
Get current execution phase, details, and dynamic AI diagnosis for an incident session.

**Query Parameters:**
* `incidentId` (optional): Retrieve the latest session for a specific incident. If omitted, returns the latest active/completed session overall.

**Response 200:**
```json
{
  "sessionId": "1779690913909-4788",
  "incidentId": "6a13eda106bb46e598dd4e41",
  "state": "completed",
  "startedAt": "2026-05-25T12:04:53Z",
  "updatedAt": "2026-05-25T12:05:43Z",
  "reasoning": {
    "cause": "Connection pool exhaustion and upstream service unavailability causing high error rate",
    "confidence": 0.95,
    "severity": "critical",
    "action": "escalate",
    "reasoning": "8 HTTP 500 exceptions occurred during checkout processing within a 5-minute window. Database pool exhaustion is critical.",
    "tools": ["fetch_logs", "fetch_metrics", "send_alert", "monitor_recovery"]
  }
}
```

**Agent States:** `idle` | `pending` | `executing` | `completed` | `failed`

---

### GET /agent/thoughts
Fetch the real-time reasoning and thought logs of the investigation session.

**Response 200:**
```json
{
  "sessionId": "1779690913909-4788",
  "incidentId": "6a13eda106bb46e598dd4e41",
  "state": "executing",
  "thoughts": [
    "🔍 Starting autonomous SRE investigation...",
    "📋 Incident: 8 HTTP 5xx errors detected in the last 5 minutes on demo-shopfast - Severity: Critical",
    "📊 Fetching recent logs for error pattern analysis...",
    "🧠 Reasoning over evidence with AI analysis...",
    "📌 Root cause identified: connection pool exhaustion (confidence: 95%)",
    "⚡ Chosen action: escalate - executing plan: [fetch_logs fetch_metrics send_alert monitor_recovery]"
  ]
}
```

---

### GET /agent/actions
Fetch the exact history of diagnostic SRE tools executed by the agent, their status, input arguments, and returning text outputs.

**Response 200:**
```json
{
  "sessionId": "1779690913909-4788",
  "actions": [
    {
      "id": "fetch_logs-1779690915120",
      "tool": "fetch_logs",
      "status": "completed",
      "input": "map[service:demo-shopfast sessionId:1779690913909-4788]",
      "output": "Recent logs for service 'demo-shopfast' (last 10 min):\n  [🔴] POST /api/checkout → 500 (1250ms) | err=\"connection pool exhausted\"\n  ...\nSummary: 8 total | 8 errors (100.0% error rate)\n",
      "startedAt": "2026-05-25T12:04:54Z",
      "endedAt": "2026-05-25T12:04:55Z"
    }
  ]
}
```

---

### POST /agent/demo
Trigger the full **90-second autonomous SRE lifecycle demo**. This does not require any external SDK traffic:
1. Automatically injects synthetic error traffic logs into the database.
2. Registers a new active incident.
3. Spawns the autonomous PulseAgent to investigate, query metrics, run Groq diagnostic analysis, and broadcast its thoughts.
4. Auto-injects healthy traffic after 40 seconds to simulate a live recovery.
5. Polls recovery and marks the incident resolved.

**Response 202:**
```json
{
  "sessionId": "1779690913909-4788",
  "incidentId": "6a13eda106bb46e598dd4e41",
  "state": "pending",
  "message": "🎭 Demo started — autonomous agent is investigating. Connect to /ws for live updates.",
  "pollUrl": "/agent/thoughts?incidentId=6a13eda106bb46e598dd4e41",
  "recoveryIn": "40 seconds — healthy traffic will be injected automatically"
}
```

---

### WebSocket Agent Events (`ws://host/ws`)

During execution, the agent broadcasts events on the `/ws` hub:

#### 1. Real-time Agent Thoughts (`agent_thought`)
```json
{
  "type": "agent_thought",
  "sessionId": "1779690913909-4788",
  "payload": {
    "thought": "📊 Fetching recent logs for error pattern analysis...",
    "timestamp": "2026-05-25T12:04:54Z"
  }
}
```

#### 2. Tool Execution Updates (`agent_action`)
```json
{
  "type": "agent_action",
  "sessionId": "1779690913909-4788",
  "payload": {
    "id": "fetch_logs-1779690915120",
    "tool": "fetch_logs",
    "status": "executing",
    "startedAt": "2026-05-25T12:04:54Z"
  }
}
```

#### 3. Investigation Complete (`agent_complete`)
Broadcasts the final complete session context including the AI's final diagnosis and recovery status.

---

## Error Response Format

All error responses use a consistent envelope:

```json
{
  "error": {
    "code": "INVALID_PARAM",
    "message": "start must be ISO 8601 e.g. 2026-05-24T10:00:00Z"
  }
}
```

| HTTP Status | Code | Meaning |
|---|---|---|
| `400` | `INVALID_PARAM` / `MISSING_FIELD` | Bad request / validation |
| `404` | `NOT_FOUND` | Resource not found |
| `500` | `*_ERROR` | Internal processing error |
| `503` | `QUEUE_FULL` | Log queue at capacity — retry |

---

## SDK Integration Notes

### Required SDK Changes

The SDK must be updated to match this backend's ingestion contract:

#### 1. Endpoint URL
```
POST http://<host>:8080/api/v1/logs
```

#### 2. Accept HTTP 202 as success
The backend returns `202 Accepted` (not `201 Created`) for queued logs. SDK retry logic must treat both `201` and `202` as success.

#### 3. Payload field names
| SDK field | Backend field | Notes |
|---|---|---|
| `latencyMs` | `latency` or `latencyMs` | Both accepted |
| `traceId` | `traceId` | Optional, pass-through |
| `meta` | `meta` | Optional arbitrary object |

#### 4. Required fields
`endpoint`, `method`, `status` are required. All others are optional but recommended for full dashboard functionality.

#### 5. Error field convention
Only include `error` field when `status >= 500`. Set to a descriptive error message string.

#### 6. Recommended SDK payload shape
```js
{
  endpoint: req.path,           // string, required
  method: req.method,           // string, required
  status: res.statusCode,       // number, required
  latency: responseTimeMs,      // number (ms)
  service: "my-service",        // string
  environment: "production",    // string
  error: err?.message ?? "",    // string, only if status >= 500
  traceId: req.headers["x-trace-id"] ?? "",
  meta: { userId: req.user?.id }
}
```

#### 7. Graceful back-pressure handling
If the backend returns `503`, the SDK should buffer the event locally and retry after `1–5s` with exponential backoff (max 3 retries).

---

## Performance Benchmarks

| Scenario | Result |
|---|---|
| API acceptance throughput | **2,114 RPS** (50 workers, 1500 logs) |
| Raw queue throughput | **~188M ops/sec** (local benchmark) |
| Handler latency | `< 1ms` (async queue) |
| Persistence rate | **100%** (verified via Mongo count) |
| Batch size | 50 logs per `InsertMany` |
| Flush interval | 500ms partial-batch timer |

---

## MongoDB Collections & Indexes

### `logs`
| Index | Fields | Purpose |
|---|---|---|
| `idx_logs_timestamp` | `timestamp DESC` | Primary sort |
| `idx_logs_status` | `status` | Error queries |
| `idx_logs_service_env_ts` | `service, environment, timestamp DESC` | Sliding window |
| `idx_logs_service_status_ts` | `service, status, timestamp DESC` | Error rate |
| `idx_logs_endpoint` | `endpoint` | Endpoint grouping |

### `incidents`
| Index | Fields | Purpose |
|---|---|---|
| `idx_incidents_dedup` | `service, environment, cause, resolved` | Deduplication |
| `idx_incidents_created_at` | `created_at DESC` | List queries |
| `idx_incidents_resolved` | `resolved` | Status filter |

---

## Project Structure

```
pulse-backend/
├── cmd/server/main.go              # Entry point, graceful shutdown
├── internal/
│   ├── ai/rca.go                   # Groq AI RCA integration
│   ├── alerts/discord.go           # Discord webhook alerts
│   ├── config/db.go                # MongoDB connection pool
│   ├── config/env.go               # Environment config
│   ├── detector/incident_detector.go # Rule-based anomaly engine
│   ├── handlers/
│   │   ├── log_handler.go          # POST /api/v1/logs
│   │   ├── metrics_handler.go      # GET /api/v1/metrics
│   │   ├── incident_handler.go     # GET/PATCH incidents + RCA
│   │   ├── topology_handler.go     # GET /api/v1/topology
│   │   ├── chaos_handler.go        # /chaos/* demo endpoints
│   │   └── ws_handler.go           # WebSocket upgrade
│   ├── middleware/logger.go        # Zerolog request logger
│   ├── models/log.go               # LogEvent model
│   ├── models/incident.go          # Incident model
│   ├── queue/queue.go              # Async batch ingestion queue
│   ├── routes/routes.go            # Router + DI wiring
│   ├── services/
│   │   ├── ingestion_service.go    # Queue wire-up
│   │   ├── metrics_service.go      # Metrics aggregations
│   │   ├── incident_service.go     # Incident CRUD
│   │   ├── topology_service.go     # Service graph derivation
│   │   ├── chaos_service.go        # Demo chaos state
│   │   ├── indexes.go              # MongoDB index management
│   │   └── helpers.go              # Shared type helpers
│   └── wsocket/hub.go              # WebSocket hub
└── scratch/                        # Dev validation scripts
    ├── e2e/main.go                 # Full pipeline E2E test
    ├── stress/main.go              # 1500+ log stress test
    ├── validate/main.go            # Persistence verification
    ├── groq/main.go                # Groq API connectivity test
    ├── mongodb/main.go             # MongoDB connectivity test
    └── count/main.go               # Document counter
```
