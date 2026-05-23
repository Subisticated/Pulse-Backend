# Pulse Backend

Production-ready Go service powering an AI-driven API monitoring platform. Ingests telemetry logs, runs real-time incident detection, delivers analytics, and performs AI root cause analysis.

---

## Tech Stack

| Layer | Technology |
|---|---|
| Language | Go 1.20+ |
| Framework | Gin |
| Database | MongoDB |
| Logging | Zerolog |
| WebSockets | gorilla/websocket |
| Config | godotenv |
| AI RCA | OpenAI gpt-3.5-turbo |
| Alerts | Discord Webhook |

---

## Quickstart

### 1. Prerequisites
- Go 1.20+
- MongoDB (local or Atlas)

### 2. Configure environment

```bash
cp .env.example .env
```

Edit `.env`:

```env
PORT=8080
MONGO_URI=mongodb://localhost:27017
DB_NAME=pulse
OPENAI_API_KEY=sk-...          # optional — uses local analysis if omitted
DISCORD_WEBHOOK=https://discord.com/api/webhooks/...  # optional
```

### 3. Install dependencies

```bash
go mod tidy
```

### 4. Run

```bash
go run cmd/server/main.go
```

---

## API Reference

### Health Check

```
GET /
```
```json
{ "status": "ok", "service": "pulse-backend" }
```

---

### Ingest Log

```
POST /api/v1/logs
```

**Body:**
```json
{
  "endpoint": "/payment",
  "method": "POST",
  "status": 500,
  "latency": 1200,
  "error": "DB timeout",
  "service": "checkout",
  "environment": "production"
}
```

**Response `201`:**
```json
{
  "message": "Log ingested successfully",
  "data": { "id": "...", "timestamp": "...", ... }
}
```

> Incident detection runs **asynchronously** after each log is saved.

---

### Get Metrics

```
GET /api/v1/metrics
```

**Response `200`:**
```json
{
  "totalRequests": 1200,
  "errorRate": 4.3,
  "avgLatency": 220,
  "requestsLastHour": 400,
  "errorsLastHour": 15
}
```

All values computed from MongoDB aggregations in real time.

---

### Get Incidents

```
GET /api/v1/incidents
GET /api/v1/incidents?resolved=false   # active only
GET /api/v1/incidents?resolved=true    # resolved only
```

**Response `200`:**
```json
[
  {
    "id": "665abc...",
    "severity": "Critical",
    "cause": "high_error_rate",
    "description": "6 HTTP 5xx errors in the last 5 minutes on service 'checkout'",
    "service": "checkout",
    "environment": "production",
    "related_logs": ["665aaa...", "665bbb..."],
    "resolved": false,
    "createdAt": "2026-05-23T17:01:00Z"
  }
]
```

---

### Resolve Incident

```
PATCH /api/v1/incidents/:id/resolve
```

Sets `resolved: true` and records `resolvedAt` timestamp.

**Response `200`:** Updated incident document.

---

### AI Root Cause Analysis

```
POST /api/v1/rca
```

**Body:**
```json
{ "incidentId": "665abc..." }
```

**Response `200`:**
```json
{
  "incidentId": "665abc...",
  "cause": "DB connection exhaustion",
  "confidence": 89,
  "evidence": [
    "Multiple HTTP 5xx responses detected in service 'checkout'",
    "Error pattern: DB timeout"
  ],
  "fixes": [
    "Check database connection pool limits and increase max connections",
    "Inspect downstream dependencies for timeouts or outages",
    "Add circuit breaker patterns to prevent cascade failures"
  ],
  "generatedAt": "2026-05-23T17:05:00Z"
}
```

Uses OpenAI `gpt-3.5-turbo` when `OPENAI_API_KEY` is set, otherwise uses deterministic local analysis.

---

### WebSocket — Real-time Events

```
WS /ws
```

Connect with any WebSocket client. Events are pushed whenever a new incident is detected:

```json
{
  "type": "incident",
  "severity": "Critical",
  "cause": "high_error_rate",
  "service": "checkout"
}
```

**JavaScript example:**
```js
const ws = new WebSocket("ws://localhost:8080/ws");
ws.onmessage = (e) => {
  const event = JSON.parse(e.data);
  console.log("New incident:", event);
};
```

---

## Incident Detection Rules

The detector runs **asynchronously** after every log ingestion and checks 3 rules:

| Rule | Trigger | Severity |
|---|---|---|
| Error Count | >5 HTTP 5xx in last 5 minutes | Critical |
| Error Rate | >10% error rate in last 5 minutes | Critical |
| Latency Spike | Single request latency >1000ms | Medium |

Deduplication: only one active incident per `(service, environment, cause)` tuple.

---

## Architecture

```
cmd/server/main.go          — entry point, wires DB + Hub + Router
internal/
  config/                   — env loading, MongoDB connection
  models/                   — LogEvent, Incident structs
  middleware/                — Zerolog request logger
  detector/                 — rule-based anomaly detector (3 rules)
  services/                 — IngestionService, MetricsService, IncidentService
  handlers/                 — Gin HTTP + WebSocket handlers
  routes/                   — DI + route registration
  ai/                       — OpenAI RCA + local fallback
  alerts/                   — Discord webhook integration
  wsocket/                  — gorilla/websocket Hub
```

---

## Discord Alerts

Set `DISCORD_WEBHOOK` in `.env` to receive rich embeds in your Discord channel when a **Critical** incident is detected. Medium/Low incidents are skipped.
