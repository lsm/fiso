# Debezium + Fiso Link CDC Integration Guide

**Version:** 1.0.0
**Status:** Reference Guide
**Use Case:** Real-time Change Data Capture from databases to downstream systems

---

## Table of Contents

1. [Overview](#1-overview)
2. [Architecture](#2-architecture)
3. [Prerequisites](#3-prerequisites)
4. [Debezium Server Configuration](#4-debezium-server-configuration)
5. [Fiso Link Configuration for CDC](#5-fiso-link-configuration-for-cdc)
6. [Example: WASM Transformation for PII Masking](#6-example-wasm-transformation-for-pii-masking)
7. [Full End-to-End Example with PostgreSQL](#7-full-end-to-end-example-with-postgresql)
8. [Debezium Event Format](#8-debezium-event-format)
9. [Common Patterns](#9-common-patterns)
10. [Troubleshooting](#10-troubleshooting)

---

## 1. Overview

Change Data Capture (CDC) enables real-time streaming of database changes to downstream systems. This guide explains how to integrate **Debezium** with **Fiso** to build robust, production-ready CDC pipelines without embedding Kafka Connect or direct database drivers in your applications.

### Why Debezium + Fiso?

| Challenge | Debezium + Fiso Solution |
|-----------|--------------------------|
| **Vendor Lock-in** | Fiso abstracts the transport layer. Start with HTTP, switch to Kafka without changing application code. |
| **Complex Transformations** | Fiso's unified transform system and WASM interceptors handle PII masking, enrichment, and filtering. |
| **Resilience** | Built-in retry, circuit breaking, and dead letter queues handle failures gracefully. |
| **Observability** | Prometheus metrics, structured logging, and OpenTelemetry tracing out of the box. |
| **Kubernetes Native** | CRD-based configuration with the Fiso Operator for GitOps workflows. |

### Key Benefits

- **Zero Application Changes**: Your services receive normalized CloudEvents, not raw Debezium JSON
- **Idempotent Processing**: Built-in deduplication support via CloudEvent IDs
- **Schema Evolution**: Transform logic adapts to schema changes via CEL expressions
- **Multi-Sink Delivery**: Route the same CDC event to HTTP, Kafka, or Temporal workflows

---

## 2. Architecture

### High-Level Flow

```
┌─────────────┐     ┌──────────────────┐     ┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│  Database   │────▶│  Debezium Server │────▶│  Fiso-Link  │────▶│  Fiso-Flow  │────▶│   Target    │
│ (PostgreSQL)│     │   (HTTP Sink)    │     │  (Kafka)    │     │ (Transform) │     │  (HTTP/API) │
└─────────────┘     └──────────────────┘     └─────────────┘     └─────────────┘     └─────────────┘
       │                    │                      │                    │
       │   WAL/CDC          │   HTTP POST          │   Kafka Topic      │   CloudEvent
       │   Events           │   JSON Array         │   (Optional)       │   HTTP POST
       ▼                    ▼                      ▼                    ▼
   pgoutput           localhost:8080          orders-cdc           localhost:8081
```

### Architecture Options

Fiso supports two deployment patterns for CDC:

#### Option A: HTTP Passthrough (Simple)

Debezium Server sends CDC events directly to Fiso-Flow via HTTP. Best for low-volume workloads (<1000 events/sec).

```
Database -> Debezium Server -> HTTP -> Fiso-Flow -> Target
```

#### Option B: Kafka Buffer (Recommended)

Debezium Server publishes to Kafka, Fiso-Flow consumes and processes. Best for high-volume workloads and guaranteed delivery.

```
Database -> Debezium Server -> Kafka -> Fiso-Flow -> Target
```

#### Option C: Fiso-Link HTTP to Kafka Bridge

Debezium Server sends to Fiso-Link, which publishes to Kafka. Best when you want to leverage Fiso-Link's resilience features before Kafka.

```
Database -> Debezium Server -> Fiso-Link (HTTP) -> Kafka -> Fiso-Flow -> Target
```

---

## 3. Prerequisites

### Software Requirements

| Component | Version | Purpose |
|-----------|---------|---------|
| **Debezium Server** | 2.x+ | CDC capture and HTTP sink |
| **Fiso-Flow** | 0.8.0+ | Event transformation and delivery |
| **Fiso-Link** | 0.8.0+ | HTTP-to-Kafka bridge (Option C only) |
| **Kafka** | 3.x+ | Message broker (Options B, C only) |
| **PostgreSQL** | 12+ | Source database (example uses PostgreSQL) |

### Infrastructure Requirements

- **PostgreSQL**: Must have `wal_level = logical` for CDC
- **Kafka** (if used): At least one broker, topics auto-created or pre-created
- **Network**: Connectivity between Debezium Server, Fiso components, and the target database

### Quick Version Check

```bash
# Check Fiso version
fiso version

# Check Debezium Server (if installed)
java -jar debezium-server-core-*.jar --version

# Check Kafka connectivity
kafka-topics.sh --bootstrap-server localhost:9092 --list
```

---

## 4. Debezium Server Configuration

### 4.1 PostgreSQL Source Configuration

Configure Debezium Server to capture changes from PostgreSQL using the `pgoutput` plugin (recommended for PostgreSQL 10+).

**`application.properties`:**

```properties
# Debezium Server Core
debezium.source.connector.class=io.debezium.connector.postgresql.PostgresConnector
debezium.source.offset.storage.file.filename=/var/lib/debezium/offsets.dat

# PostgreSQL Connection
debezium.source.database.hostname=postgres.example.com
debezium.source.database.port=5432
debezium.source.database.user=debezium
debezium.source.database.password=${DB_PASSWORD}
debezium.source.database.dbname=inventory

# PostgreSQL CDC Configuration
debezium.source.plugin.name=pgoutput
debezium.source.slot.name=debezium_inventory
debezium.source.publication.name=debezium_inventory_pub
debezium.source.database.initial.discovery=false

# Capture Schema
debezium.source.schema.include.list=public
debezium.source.table.include.list=public.orders,public.customers,public.products

# Snapshot Configuration
debezium.source.snapshot.mode=initial
debezium.source.snapshot.locking.mode=none

# Heartbeat for idle connections
debezium.source.heartbeat.interval.ms=60000
```

### 4.2 HTTP Sink Configuration (Option A & C)

Configure Debezium Server to send events via HTTP to Fiso.

**Option A: Direct to Fiso-Flow:**

```properties
# HTTP Sink - Send to Fiso-Flow
debezium.sink.type=http
debezium.sink.http.url=http://fiso-flow:8081/ingest
debezium.sink.http.timeout.ms=30000
debezium.sink.http.retries=3
debezium.sink.http.retry.delay.ms=1000

# Authentication (if required)
# debezium.sink.http.auth.type=basic
# debezium.sink.http.auth.username=debezium
# debezium.sink.http.auth.password=${HTTP_PASSWORD}

# Batch configuration
debezium.sink.http.batch.size=100
debezium.sink.http.flush.interval.ms=5000
```

**Option C: Through Fiso-Link to Kafka:**

```properties
# HTTP Sink - Send to Fiso-Link (which publishes to Kafka)
debezium.sink.type=http
debezium.sink.http.url=http://fiso-link:3500/link/cdc-publisher
debezium.sink.http.timeout.ms=30000
debezium.sink.http.retries=3

# Headers for routing/tracing
debezium.sink.http.headers=X-Source-Debezium:true,X-CDC-Slot:debezium_inventory
```

### 4.3 Kafka Sink Configuration (Option B)

Configure Debezium Server to publish directly to Kafka.

**`application.properties`:**

```properties
# Kafka Sink
debezium.sink.type=kafka
debezium.sink.kafka.producer.bootstrap.servers=kafka:9092
debezium.sink.kafka.producer.key.serializer=org.apache.kafka.common.serialization.StringSerializer
debezium.sink.kafka.producer.value.serializer=org.apache.kafka.common.serialization.StringSerializer

# Topic naming
debezium.topic.prefix=cdc
debezium.topic.naming.strategy=io.debezium.schema.SchemaTopicNamingStrategy

# Reliability
debezium.sink.kafka.producer.acks=all
debezium.sink.kafka.producer.enable.idempotence=true
```

### 4.4 Transformation Configuration

Debezium can apply transformations before sending events. For most use cases, Fiso provides more flexibility, but you can apply basic filtering here.

```properties
# Filter out specific operations (optional)
debezium.transforms=unwrap,filter
debezium.transforms.unwrap.type=io.debezium.transforms.ExtractNewRecordState
debezium.transforms.unwrap.drop.tombstones=false
debezium.transforms.unwrap.add.fields=op,ts_ms

debezium.transforms.filter.type=io.debezium.transforms.Filter
debezium.transforms.filter.language=jsr223.groovy
debezium.transforms.filter.condition=value.op == 'c' || value.op == 'u'
```

---

## 5. Fiso Link Configuration for CDC

### 5.1 Fiso-Link HTTP-to-Kafka Bridge (Option C)

When using Fiso-Link as an HTTP-to-Kafka bridge, configure it to accept Debezium events and publish to Kafka.

**`link/config.yaml`:**

```yaml
listenAddr: ":3500"
metricsAddr: ":9091"

kafka:
  clusters:
    main:
      brokers:
        - kafka:9092

targets:
  # HTTP-to-Kafka bridge for Debezium CDC events
  - name: cdc-publisher
    protocol: kafka
    kafka:
      cluster: main
      topic: debezium-cdc-events
      # Use table name as key for partition ordering
      key:
        type: header
        field: X-Debezium-Table
      headers:
        source: debezium
        format: debezium-json
      requiredAcks: all
    circuitBreaker:
      enabled: true
      failureThreshold: 5
      resetTimeout: "30s"
    retry:
      maxAttempts: 3
      backoff: exponential
      initialInterval: "100ms"
      maxInterval: "1s"
    rateLimit:
      requestsPerSecond: 5000
      burst: 500
```

### 5.2 Fiso-Flow CDC Pipeline Configuration

Configure Fiso-Flow to consume CDC events and deliver them to your services.

#### Direct HTTP Ingestion (Option A)

**`flows/cdc-orders.yaml`:**

```yaml
name: cdc-orders
source:
  type: http
  config:
    listenAddr: ":8081"
    path: /ingest

transform:
  fields:
    # Extract Debezium envelope fields
    operation: "data.payload.op"
    before: "data.payload.before"
    after: "data.payload.after"
    source: "data.payload.source"
    timestamp: "data.payload.ts_ms"
    # Build simplified event
    event_type: '"cdc"'
    table: "data.payload.source.table"
    # Primary key for idempotency
    primary_key: "data.payload.after.id"

cloudevents:
  # Build unique ID from Debezium transaction info
  id: 'data.payload.source.table + "-" + data.payload.after.id + "-" + string(data.payload.ts_ms)'
  type: '"cdc." + data.payload.source.table + "." + data.payload.op'
  source: '"debezium/" + data.payload.source.db'

sink:
  type: http
  config:
    url: http://order-service:8080/events/cdc
    method: POST
    headers:
      Content-Type: application/cloudevents+json

errorHandling:
  deadLetterTopic: fiso-dlq-cdc-orders
  maxRetries: 5
  backoff: exponential
```

#### Kafka Consumption (Options B & C)

**`flows/cdc-orders-kafka.yaml`:**

```yaml
name: cdc-orders-kafka

kafka:
  clusters:
    main:
      brokers:
        - kafka:9092

source:
  type: kafka
  config:
    cluster: main
    topic: debezium-cdc-events
    consumerGroup: fiso-cdc-orders
    startOffset: earliest

transform:
  fields:
    operation: "data.payload.op"
    table: "data.payload.source.table"
    database: "data.payload.source.db"
    schema: "data.payload.source.schema"
    # Extract the actual row data
    row_data: "data.payload.after"
    # Include before state for updates/deletes
    before_data: "data.payload.before"
    # Timestamp from database transaction
    event_time: "data.payload.ts_ms"
    # Build routing key
    route_key: 'data.payload.source.table + "-" + data.payload.after.id'

cloudevents:
  id: 'data.payload.source.table + "-" + data.payload.after.id + "-" + string(data.payload.ts_ms)'
  type: '"cdc." + data.payload.source.table + "." + data.payload.op'
  source: '"debezium/" + data.payload.source.db'

sink:
  type: http
  config:
    url: http://order-service:8080/events/cdc
    method: POST

errorHandling:
  deadLetterTopic: fiso-dlq-cdc-orders
  maxRetries: 5
  backoff: exponential
```

---

## 6. Example: WASM Transformation for PII Masking

When handling CDC events containing sensitive data (SSNs, emails, phone numbers), use WASM interceptors to mask PII before events reach downstream services.

### 6.1 Go WASM Module for PII Masking

**`pii-mask/main.go`:**

```go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

// DebeziumEvent represents the structure of a Debezium CDC event
type DebeziumEvent struct {
	Specversion string                 `json:"specversion"`
	Type        string                 `json:"type"`
	Source      string                 `json:"source"`
	ID          string                 `json:"id"`
	Time        string                 `json:"time"`
	Data        map[string]interface{} `json:"data"`
}

// MaskingRule defines a PII masking rule
type MaskingRule struct {
	Field    string `json:"field"`
	Pattern  string `json:"pattern"`
	Replace  string `json:"replace"`
	Enabled  bool   `json:"enabled"`
}

var maskingRules = []MaskingRule{
	// SSN: XXX-XX-1234 -> ***-**-1234
	{Field: "ssn", Pattern: `\d{3}-\d{2}`, Replace: "***-**", Enabled: true},
	// Email: john.doe@example.com -> j***@example.com
	{Field: "email", Pattern: `(.)[^@]*(@)`, Replace: "$1***$2", Enabled: true},
	// Phone: (555) 123-4567 -> (***) ***-4567
	{Field: "phone", Pattern: `\(\d{3}\)\s*\d{3}`, Replace: "(***) ***", Enabled: true},
	// Credit Card: 4111111111111111 -> ************1111
	{Field: "credit_card", Pattern: `\d(?=\d{4})`, Replace: "*", Enabled: true},
	// Generic password fields
	{Field: "password", Pattern: `.*`, Replace: "********", Enabled: true},
}

// maskValue applies masking rules to a single value
func maskValue(field string, value string) string {
	lowerField := strings.ToLower(field)

	for _, rule := range maskingRules {
		if !rule.Enabled {
			continue
		}

		// Check if field name matches (case-insensitive, partial match)
		if strings.Contains(lowerField, rule.Field) || rule.Field == "*" {
			re, err := regexp.Compile(rule.Pattern)
			if err != nil {
				continue
			}
			value = re.ReplaceAllStringFunc(value, func(match string) string {
				return rule.Replace
			})
		}
	}

	return value
}

// maskPII recursively masks PII in nested structures
func maskPII(data interface{}) interface{} {
	switch v := data.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{})
		for key, val := range v {
			switch nested := val.(type) {
			case string:
				result[key] = maskValue(key, nested)
			case map[string]interface{}:
				result[key] = maskPII(nested)
			case []interface{}:
				result[key] = maskPII(nested)
			default:
				result[key] = val
			}
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(v))
		for i, val := range v {
			result[i] = maskPII(val)
		}
		return result
	case string:
		return maskValue("value", v)
	default:
		return data
	}
}

func main() {
	// Read CloudEvent from stdin
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading stdin: %v\n", err)
		os.Exit(1)
	}

	// Parse the CloudEvent
	var event DebeziumEvent
	if err := json.Unmarshal(input, &event); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing JSON: %v\n", err)
		os.Exit(1)
	}

	// Mask PII in the data field
	if event.Data != nil {
		// Handle Debezium payload structure
		if payload, ok := event.Data["payload"].(map[string]interface{}); ok {
			// Mask after state
			if after, ok := payload["after"].(map[string]interface{}); ok {
				payload["after"] = maskPII(after)
			}
			// Mask before state
			if before, ok := payload["before"].(map[string]interface{}); ok {
				payload["before"] = maskPII(before)
			}
		} else {
			// Direct data masking for simplified events
			event.Data = maskPII(event.Data).(map[string]interface{})
		}
	}

	// Add masking metadata
	if event.Data == nil {
		event.Data = make(map[string]interface{})
	}
	event.Data["pii_masked"] = true
	event.Data["masking_version"] = "1.0"

	// Output the masked CloudEvent
	output, err := json.Marshal(event)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling JSON: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(string(output))
}
```

### 6.2 Compiling to WASM

```bash
# Build for wasip1 target
cd pii-mask
GOOS=wasip1 GOARCH=wasm go build -o pii-mask.wasm .

# Verify the module works locally
echo '{"specversion":"1.0","type":"cdc.customers.create","source":"debezium/inventory","id":"customers-1-123","data":{"payload":{"op":"c","after":{"id":1,"name":"John Doe","email":"john.doe@example.com","ssn":"123-45-6789","phone":"(555) 123-4567"}}}}' | wasmtime pii-mask.wasm
```

**Expected output:**

```json
{"specversion":"1.0","type":"cdc.customers.create","source":"debezium/inventory","id":"customers-1-123","data":{"pii_masked":true,"masking_version":"1.0","payload":{"after":{"email":"j***@example.com","id":1,"name":"John Doe","phone":"(***) ***-4567","ssn":"***-**-6789"},"op":"c"}}}
```

### 6.3 Flow Configuration with WASM Interceptor

**`flows/cdc-customers-pii.yaml`:**

```yaml
name: cdc-customers-pii

kafka:
  clusters:
    main:
      brokers:
        - kafka:9092

source:
  type: kafka
  config:
    cluster: main
    topic: debezium-cdc-customers
    consumerGroup: fiso-cdc-customers

# Transform first to simplify structure
transform:
  fields:
    operation: "data.payload.op"
    customer_id: "data.payload.after.id"
    name: "data.payload.after.name"
    email: "data.payload.after.email"
    phone: "data.payload.after.phone"
    ssn: "data.payload.after.ssn"
    created_at: "data.payload.ts_ms"

# WASM interceptor for PII masking
interceptors:
  - type: wasm
    config:
      module: /etc/fiso/wasm/pii-mask.wasm
      timeout: "2s"

cloudevents:
  id: '"customer-" + data.customer_id + "-" + string(data.created_at)'
  type: '"cdc.customers." + data.operation'
  source: debezium/inventory

sink:
  type: http
  config:
    url: http://analytics-service:8080/events/customer-updates
    method: POST

errorHandling:
  deadLetterTopic: fiso-dlq-cdc-customers
  maxRetries: 5
  backoff: exponential
```

---

## 7. Full End-to-End Example with PostgreSQL

This section provides a complete, working example of CDC from PostgreSQL through Fiso to a target service.

### 7.1 PostgreSQL Setup

**Enable logical replication:**

```sql
-- postgresql.conf (requires restart)
wal_level = logical
max_wal_senders = 4
max_replication_slots = 4

-- Create Debezium user
CREATE USER debezium WITH REPLICATION PASSWORD 'your_password';

-- Grant permissions
GRANT CONNECT ON DATABASE inventory TO debezium;
GRANT USAGE ON SCHEMA public TO debezium;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO debezium;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO debezium;
```

**Create sample table:**

```sql
CREATE TABLE orders (
    id SERIAL PRIMARY KEY,
    customer_id INTEGER NOT NULL,
    order_date TIMESTAMP NOT NULL DEFAULT NOW(),
    total_amount DECIMAL(10,2) NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    shipping_address TEXT,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

-- Create publication for Debezium
CREATE PUBLICATION debezium_inventory_pub FOR ALL TABLES;
```

### 7.2 Docker Compose Setup

**`docker-compose.yml`:**

```yaml
version: '3.8'

services:
  postgres:
    image: postgres:15
    environment:
      POSTGRES_DB: inventory
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: postgres
    command:
      - "postgres"
      - "-c"
      - "wal_level=logical"
      - "-c"
      - "max_wal_senders=4"
      - "-c"
      - "max_replication_slots=4"
    ports:
      - "5432:5432"
    volumes:
      - postgres_data:/var/lib/postgresql/data
      - ./init-db.sql:/docker-entrypoint-initdb.d/init-db.sql
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U postgres"]
      interval: 10s
      timeout: 5s
      retries: 5

  kafka:
    image: docker.io/bitnami/kafka:3.7
    ports:
      - "9092:9092"
    environment:
      - KAFKA_CFG_NODE_ID=1
      - KAFKA_CFG_PROCESS_ROLES=broker,controller
      - KAFKA_CFG_CONTROLLER_QUORUM_VOTER=1@kafka:9093
      - KAFKA_CFG_LISTENERS=PLAINTEXT://:9092,CONTROLLER://:9093
      - KAFKA_CFG_ADVERTISED_LISTENERS=PLAINTEXT://kafka:9092
      - KAFKA_CFG_LISTENER_SECURITY_PROTOCOL_MAP=CONTROLLER:PLAINTEXT,PLAINTEXT:PLAINTEXT
      - KAFKA_CFG_CONTROLLER_LISTENER_NAMES=CONTROLLER
      - KAFKA_CFG_INTER_BROKER_LISTENER_NAME=PLAINTEXT
      - KAFKA_CFG_AUTO_CREATE_TOPICS_ENABLE=true
    volumes:
      - kafka_data:/bitnami/kafka

  debezium-server:
    image: debezium/server:2.7
    depends_on:
      postgres:
        condition: service_healthy
      kafka:
        condition: service_started
    environment:
      - DEBEZIUM_SOURCE_CONNECTOR_CLASS=io.debezium.connector.postgresql.PostgresConnector
      - DEBEZIUM_SOURCE_DATABASE_HOSTNAME=postgres
      - DEBEZIUM_SOURCE_DATABASE_PORT=5432
      - DEBEZIUM_SOURCE_DATABASE_USER=debezium
      - DEBEZIUM_SOURCE_DATABASE_PASSWORD=your_password
      - DEBEZIUM_SOURCE_DATABASE_DBNAME=inventory
      - DEBEZIUM_SOURCE_PLUGIN_NAME=pgoutput
      - DEBEZIUM_SOURCE_SLOT_NAME=debezium_inventory
      - DEBEZIUM_SOURCE_PUBLICATION_NAME=debezium_inventory_pub
      - DEBEZIUM_SOURCE_TABLE_INCLUDE_LIST=public.orders
      - DEBEZIUM_SOURCE_SNAPSHOT_MODE=initial
      - DEBEZIUM_SINK_TYPE=kafka
      - DEBEZIUM_SINK_KAFKA_PRODUCER_BOOTSTRAP_SERVERS=kafka:9092
      - DEBEZIUM_TOPIC_PREFIX=cdc
    volumes:
      - debezium_data:/var/lib/debezium

  fiso-flow:
    image: ghcr.io/lsm/fiso-flow:latest
    depends_on:
      - kafka
    ports:
      - "8081:8081"
      - "9090:9090"
    environment:
      - FISO_CONFIG_DIR=/etc/fiso/flows
      - FISO_METRICS_ADDR=:9090
    volumes:
      - ./fiso/flows:/etc/fiso/flows:ro
      - ./fiso/wasm:/etc/fiso/wasm:ro

  order-service:
    build:
      context: ./order-service
      dockerfile: Dockerfile
    ports:
      - "8080:8080"
    environment:
      - PORT=8080

volumes:
  postgres_data:
  kafka_data:
  debezium_data:
```

### 7.3 Fiso Flow Configuration

**`fiso/flows/orders-cdc.yaml`:**

```yaml
name: orders-cdc

kafka:
  clusters:
    main:
      brokers:
        - kafka:9092

source:
  type: kafka
  config:
    cluster: main
    # Debezium creates topics as: {prefix}.{database}.{schema}.{table}
    topic: cdc.inventory.public.orders
    consumerGroup: fiso-orders-cdc
    startOffset: earliest

transform:
  fields:
    # Extract operation type (c=create, u=update, d=delete)
    operation: "data.payload.op"
    # Extract row data
    order_id: "data.payload.after.id"
    customer_id: "data.payload.after.customer_id"
    total_amount: "data.payload.after.total_amount"
    status: "data.payload.after.status"
    shipping_address: "data.payload.after.shipping_address"
    # Include previous state for updates
    previous_status: "data.payload.before.status"
    # Metadata
    transaction_id: "data.payload.transaction"
    change_timestamp: "data.payload.ts_ms"
    # Source database info
    source_table: "data.payload.source.table"
    source_db: "data.payload.source.db"
    # Computed fields
    event_type: '"order." + data.payload.op'
    is_high_value: "data.payload.after.total_amount > 1000"

cloudevents:
  id: '"order-" + string(data.payload.after.id) + "-" + string(data.payload.ts_ms)'
  type: '"order." + data.payload.op'
  source: '"debezium/" + data.payload.source.db'

sink:
  type: http
  config:
    url: http://order-service:8080/events/cdc
    method: POST
    headers:
      Content-Type: application/cloudevents+json
      X-CDC-Source: debezium

errorHandling:
  deadLetterTopic: fiso-dlq-orders-cdc
  maxRetries: 5
  backoff: exponential
```

### 7.4 Target Service Example

**`order-service/main.go`:**

```go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
)

// CloudEvent represents a CloudEvent v1.0 structure
type CloudEvent struct {
	Specversion string                 `json:"specversion"`
	Type        string                 `json:"type"`
	Source      string                 `json:"source"`
	ID          string                 `json:"id"`
	Time        string                 `json:"time"`
	Data        map[string]interface{} `json:"data"`
}

func handleCDC(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var event CloudEvent
	if err := json.Unmarshal(body, &event); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Log the CDC event
	log.Printf("Received CDC event: type=%s id=%s operation=%s order_id=%v",
		event.Type, event.ID,
		event.Data["operation"],
		event.Data["order_id"])

	// Process based on operation type
	switch event.Data["operation"] {
	case "c":
		log.Printf("New order created: %+v", event.Data)
		// Handle order creation
	case "u":
		log.Printf("Order updated: %+v (previous status: %v)",
			event.Data, event.Data["previous_status"])
		// Handle order update
	case "d":
		log.Printf("Order deleted: %+v", event.Data)
		// Handle order deletion
	}

	// Acknowledge receipt
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "acknowledged",
		"id":     event.ID,
	})
}

func main() {
	http.HandleFunc("/events/cdc", handleCDC)
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	fmt.Println("Order service listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
```

### 7.5 Running the Example

```bash
# Start all services
docker compose up -d

# Check logs
docker compose logs -f fiso-flow

# Insert a test order into PostgreSQL
docker compose exec postgres psql -U postgres -d inventory -c "
INSERT INTO orders (customer_id, order_date, total_amount, status, shipping_address)
VALUES (1, NOW(), 150.00, 'pending', '123 Main St, City, ST 12345');
"

# Update the order
docker compose exec postgres psql -U postgres -d inventory -c "
UPDATE orders SET status = 'shipped' WHERE id = 1;
"

# Check Fiso metrics
curl http://localhost:9090/metrics | grep fiso_flow_events_total

# View DLQ if there were failures
docker compose exec kafka kafka-console-consumer.sh \
  --bootstrap-server localhost:9092 \
  --topic fiso-dlq-orders-cdc \
  --from-beginning
```

---

## 8. Debezium Event Format

Understanding the Debezium event structure is essential for writing effective transforms.

### 8.1 Standard Debezium Envelope

```json
{
  "payload": {
    "before": null,
    "after": {
      "id": 1,
      "customer_id": 100,
      "order_date": 1705000000000000,
      "total_amount": 150.00,
      "status": "pending",
      "shipping_address": "123 Main St",
      "created_at": 1705000000000000,
      "updated_at": 1705000000000000
    },
    "source": {
      "version": "2.7.0.Final",
      "connector": "postgresql",
      "name": "cdc",
      "ts_ms": 1705000000123,
      "snapshot": "false",
      "db": "inventory",
      "sequence": "[\"12345\",\"12346\"]",
      "schema": "public",
      "table": "orders",
      "txId": 12345,
      "lsn": 12345678,
      "xmin": null
    },
    "op": "c",
    "ts_ms": 1705000000456,
    "transaction": {
      "id": "12345:123456",
      "total_order": 1,
      "data_collection_order": 1
    }
  },
  "schema": { ... }
}
```

### 8.2 Field Reference

| Field | Description | Values |
|-------|-------------|--------|
| `payload.op` | Operation type | `c` (create), `u` (update), `d` (delete), `r` (read/snapshot) |
| `payload.before` | Row state before change | Object or `null` for inserts |
| `payload.after` | Row state after change | Object or `null` for deletes |
| `payload.source.table` | Table name | e.g., `"orders"` |
| `payload.source.db` | Database name | e.g., `"inventory"` |
| `payload.source.schema` | Schema name | e.g., `"public"` |
| `payload.ts_ms` | Event timestamp (ms) | Unix epoch milliseconds |
| `payload.transaction` | Transaction metadata | Contains `id`, `total_order` |

### 8.3 Operation Types

| Op | Name | Description | `before` | `after` |
|----|------|-------------|----------|---------|
| `c` | Create | New row inserted | `null` | Row data |
| `u` | Update | Row modified | Previous state | New state |
| `d` | Delete | Row removed | Previous state | `null` |
| `r` | Read | Snapshot read | `null` | Row data |

### 8.4 Transforming Debezium Events

Use Fiso's unified transform system to simplify Debezium's complex structure:

```yaml
transform:
  fields:
    # Extract operation
    op: "data.payload.op"
    is_create: 'data.payload.op == "c"'
    is_update: 'data.payload.op == "u"'
    is_delete: 'data.payload.op == "d"'

    # Extract row data (handles null for deletes)
    current_data: "data.payload.after"
    previous_data: "data.payload.before"

    # Safe field access with null checks
    order_id: 'data.payload.after != null ? data.payload.after.id : data.payload.before.id'
    status: 'data.payload.after != null ? data.payload.after.status : null'

    # Extract metadata
    table: "data.payload.source.table"
    database: "data.payload.source.db"
    timestamp: "data.payload.ts_ms"
```

---

## 9. Common Patterns

### 9.1 Table-Based Routing

Route different tables to different services based on the source table:

```yaml
name: cdc-router

kafka:
  clusters:
    main:
      brokers:
        - kafka:9092

source:
  type: kafka
  config:
    cluster: main
    topic: cdc.inventory.public.orders,cdc.inventory.public.customers
    consumerGroup: fiso-cdc-router

transform:
  fields:
    table: "data.payload.source.table"
    operation: "data.payload.op"
    # Include all row data
    row: "data.payload.after"

cloudevents:
  id: 'data.payload.source.table + "-" + (data.payload.after != null ? string(data.payload.after.id) : string(data.payload.before.id)) + "-" + string(data.payload.ts_ms)'
  type: '"cdc." + data.payload.source.table + "." + data.payload.op'
  source: '"debezium/" + data.payload.source.db'

sink:
  type: http
  config:
    # Route based on table name using CloudEvent type
    url: 'http://{{.type}}-service:8080/events'
    method: POST
```

### 9.2 Filtering by Operation Type

Process only specific operation types:

```yaml
name: cdc-orders-updates

kafka:
  clusters:
    main:
      brokers:
        - kafka:9092

source:
  type: kafka
  config:
    cluster: main
    topic: cdc.inventory.public.orders
    consumerGroup: fiso-cdc-order-updates

# Filter transform - only process updates
transform:
  fields:
    # Only pass through if operation is update
    _filter: 'data.payload.op == "u" ? "pass" : "filter"'
    order_id: "data.payload.after.id"
    old_status: "data.payload.before.status"
    new_status: "data.payload.after.status"
    changed_at: "data.payload.ts_ms"
```

Alternatively, use a WASM interceptor for more complex filtering logic:

```go
// filter.go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

func main() {
	input, _ := io.ReadAll(os.Stdin)

	var event map[string]interface{}
	json.Unmarshal(input, &event)

	// Navigate to payload.op
	if data, ok := event["data"].(map[string]interface{}); ok {
		if payload, ok := data["payload"].(map[string]interface{}); ok {
			if op, ok := payload["op"].(string); ok {
				// Filter: only process creates and updates
				if op == "d" {
					// Return empty object to signal drop
					fmt.Println("{}")
					return
				}
			}
		}
	}

	// Pass through
	fmt.Println(string(input))
}
```

### 9.3 Enrichment with External Data

Combine CDC events with reference data from external APIs:

```yaml
name: cdc-orders-enriched

source:
  type: kafka
  config:
    cluster: main
    topic: cdc.inventory.public.orders
    consumerGroup: fiso-cdc-enricher

# First transform: extract customer ID
transform:
  fields:
    order_id: "data.payload.after.id"
    customer_id: "data.payload.after.customer_id"
    amount: "data.payload.after.total_amount"
    status: "data.payload.after.status"

# WASM interceptor to fetch customer data
interceptors:
  - type: wasm
    config:
      module: /etc/fiso/wasm/enrich-customer.wasm
      timeout: "5s"

sink:
  type: http
  config:
    url: http://analytics-service:8080/events/enriched-orders
    method: POST
```

**`enrich-customer.wasm` (Go):**

```go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

type Event struct {
	Specversion string                 `json:"specversion"`
	Type        string                 `json:"type"`
	ID          string                 `json:"id"`
	Data        map[string]interface{} `json:"data"`
}

func main() {
	input, _ := io.ReadAll(os.Stdin)

	var event Event
	json.Unmarshal(input, &event)

	if customerID, ok := event.Data["customer_id"]; ok {
		// Fetch customer data from customer service
		client := &http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get(fmt.Sprintf("http://customer-service:8080/customers/%v", customerID))
		if err == nil {
			defer resp.Body.Close()
			var customer map[string]interface{}
			if json.NewDecoder(resp.Body).Decode(&customer) == nil {
				event.Data["customer"] = customer
			}
		}
	}

	output, _ := json.Marshal(event)
	fmt.Println(string(output))
}
```

### 9.4 Temporal Workflow Integration

Send CDC events to Temporal for durable processing:

```yaml
name: cdc-orders-temporal

source:
  type: kafka
  config:
    cluster: main
    topic: cdc.inventory.public.orders
    consumerGroup: fiso-cdc-temporal

transform:
  fields:
    order_id: "data.payload.after.id"
    customer_id: "data.payload.after.customer_id"
    total: "data.payload.after.total_amount"
    status: "data.payload.after.status"
    operation: "data.payload.op"

cloudevents:
  id: '"order-" + string(data.payload.after.id)'
  type: '"order." + data.payload.op'
  source: debezium/inventory

sink:
  type: temporal
  config:
    hostPort: temporal:7233
    namespace: default
    taskQueue: order-processing
    workflowType: ProcessOrderCDC
    workflowIdExpr: "order-{{.data.order_id}}"
    mode: signal
    signalName: order-cdc-event
```

**Temporal Workflow (Kotlin):**

```kotlin
@WorkflowInterface
interface ProcessOrderCDC {
    @WorkflowMethod
    fun processOrder(event: CloudEvent)

    @SignalMethod
    fun orderCdcEvent(event: CloudEvent)
}

data class CloudEvent(
    val specversion: String,
    val type: String,
    val source: String,
    val id: String,
    val data: OrderData
)

data class OrderData(
    val order_id: Long,
    val customer_id: Long,
    val total: Double,
    val status: String,
    val operation: String
)
```

### 9.5 Multi-Sink Fan-Out

Deliver the same CDC event to multiple destinations:

Create multiple flow definitions consuming from the same topic:

**`flows/cdc-orders-analytics.yaml`:**

```yaml
name: cdc-orders-analytics

source:
  type: kafka
  config:
    cluster: main
    topic: cdc.inventory.public.orders
    consumerGroup: fiso-cdc-analytics

sink:
  type: http
  config:
    url: http://analytics-service:8080/events/orders
```

**`flows/cdc-orders-audit.yaml`:**

```yaml
name: cdc-orders-audit

source:
  type: kafka
  config:
    cluster: main
    topic: cdc.inventory.public.orders
    consumerGroup: fiso-cdc-audit

sink:
  type: kafka
  config:
    cluster: main
    topic: audit-log
```

**`flows/cdc-orders-notifications.yaml`:**

```yaml
name: cdc-orders-notifications

source:
  type: kafka
  config:
    cluster: main
    topic: cdc.inventory.public.orders
    consumerGroup: fiso-cdc-notifications

transform:
  fields:
    # Only include high-value orders
    order_id: "data.payload.after.id"
    total: "data.payload.after.total_amount"
    _skip: 'data.payload.after.total_amount < 500 ? "skip" : "process"'

sink:
  type: http
  config:
    url: http://notification-service:8080/events/high-value-orders
```

---

## 10. Troubleshooting

### Common Issues

#### Debezium Connection Refused

**Symptom:** Debezium Server fails to connect to PostgreSQL

**Solution:**

1. Verify PostgreSQL is running and accessible
2. Check `wal_level = logical` is set in `postgresql.conf`
3. Verify the replication slot exists:

```sql
SELECT * FROM pg_replication_slots WHERE slot_name = 'debezium_inventory';
```

4. Check user permissions:

```sql
SELECT rolreplication FROM pg_roles WHERE rolname = 'debezium';
```

#### Fiso-Flow Not Receiving Events

**Symptom:** No events arriving at Fiso-Flow

**Solution:**

1. Verify Debezium is publishing to the correct topic:

```bash
kafka-console-consumer.sh --bootstrap-server localhost:9092 \
  --topic cdc.inventory.public.orders --from-beginning --max-messages 10
```

2. Check consumer group status:

```bash
kafka-consumer-groups.sh --bootstrap-server localhost:9092 \
  --describe --group fiso-cdc-orders
```

3. Verify topic name matches configuration (Debezium format: `{prefix}.{db}.{schema}.{table}`)

#### Transform Errors

**Symptom:** Events going to DLQ with `TRANSFORM_FAILED`

**Solution:**

1. Test transforms locally:

```bash
fiso transform test --flow flows/cdc-orders.yaml --input sample-debezium-event.json
```

2. Check for null field access - use conditional expressions:

```yaml
# Bad - fails if after is null (delete events)
order_id: "data.payload.after.id"

# Good - handles null gracefully
order_id: 'data.payload.after != null ? data.payload.after.id : data.payload.before.id'
```

3. Verify data types match expectations (timestamps are integers, not strings)

#### High Latency

**Symptom:** CDC events arriving with significant delay

**Solution:**

1. Check Debezium heartbeat configuration:

```properties
debezium.source.heartbeat.interval.ms=60000
debezium.source.heartbeat.topics.prefix=__debezium-heartbeat
```

2. Monitor consumer lag:

```bash
curl http://localhost:9090/metrics | grep fiso_flow_consumer_lag
```

3. Scale Fiso-Flow replicas if lag is growing

4. Check Kafka consumer configuration:

```yaml
source:
  type: kafka
  config:
    # Reduce poll interval for lower latency
    sessionTimeoutMs: 10000
    heartbeatIntervalMs: 3000
```

### Debugging Commands

```bash
# Check Debezium Server logs
docker compose logs debezium-server -f

# Check Fiso-Flow logs
fiso logs --follow

# Consume raw CDC topic
fiso consume --topic cdc.inventory.public.orders --follow

# Check Fiso metrics
curl http://localhost:9090/metrics | grep -E 'fiso_flow_(events|dlq|errors)'

# Test WASM module manually
echo '{"data":{"payload":{"op":"c","after":{"id":1}}}}' | \
  wasmtime /etc/fiso/wasm/transform.wasm
```

---

## Appendix A: Configuration Reference

### Debezium Server Properties

| Property | Default | Description |
|----------|---------|-------------|
| `debezium.source.connector.class` | Required | Connector class (e.g., `io.debezium.connector.postgresql.PostgresConnector`) |
| `debezium.source.plugin.name` | `pgoutput` | PostgreSQL logical replication plugin |
| `debezium.source.snapshot.mode` | `initial` | Snapshot mode: `initial`, `never`, `when_needed` |
| `debezium.source.table.include.list` | All tables | Comma-separated list of tables to capture |
| `debezium.sink.type` | Required | Sink type: `kafka`, `http`, `pubsub`, etc. |
| `debezium.topic.prefix` | Required | Prefix for Kafka topic names |

### Fiso-Flow CDC Configuration

| Field | Required | Description |
|-------|----------|-------------|
| `source.type` | Yes | `kafka` or `http` |
| `source.config.topic` | For Kafka | Kafka topic to consume |
| `source.config.consumerGroup` | For Kafka | Consumer group ID |
| `transform.fields` | No | Field mapping using CEL expressions |
| `interceptors[].type` | No | `wasm` for WASM interceptors |
| `sink.type` | Yes | `http`, `kafka`, or `temporal` |

---

## Appendix B: Glossary

| Term | Definition |
|------|------------|
| **CDC** | Change Data Capture - capturing database changes in real-time |
| **Debezium** | Open-source CDC platform that reads database transaction logs |
| **Logical Replication** | PostgreSQL feature that streams data changes |
| **pgoutput** | PostgreSQL's built-in logical replication plugin |
| **Replication Slot** | PostgreSQL mechanism to track consumer position in WAL |
| **WAL** | Write-Ahead Log - PostgreSQL's transaction log |
| **CloudEvent** | CNCF standard for event data format |
| **CEL** | Common Expression Language - used in Fiso transforms |

---

## Appendix C: Additional Resources

- [Debezium Documentation](https://debezium.io/documentation/reference/stable/)
- [Debezium Server HTTP Sink](https://debezium.io/documentation/reference/stable/configuration/sink-options.html#http-sink)
- [PostgreSQL Logical Replication](https://www.postgresql.org/docs/current/logical-replication.html)
- [Fiso Documentation](../README.md)
- [CloudEvents Specification](https://github.com/cloudevents/spec)
- [CEL Language Definition](https://github.com/google/cel-spec)
