# Bitemporal Audit Demo — XTDB

A self-contained demo showing how XTDB's bitemporal capabilities solve two structural audit problems in AI-driven cash application:

1. **Reconstructing decision state when models evolve** — what did the system know, what did it decide, and why?
2. **Late-arriving business facts** — a credit memo that existed in the business world before the system recorded it.

## Prerequisites

- Go 1.21+
- Docker / Docker Compose

## Quick Start

```bash
docker compose up -d        # start XTDB (port 5432)
go run .                    # seed data + start web UI on :3000
```

Open http://localhost:3000. Press `S` to toggle the presenter script panel.

## Commands

| Command | Description |
|---------|-------------|
| `go run .` | Seed XTDB + start web UI (default) |
| `go run . seed` | Seed data only |
| `go run . serve` | Start web UI only (assumes data already seeded) |
| `go run . cli` | Seed + print query results to terminal |

Environment variables: `XTDB_HOST` (default `localhost`), `XTDB_PORT` (default `5432`), `ADDR` (default `:3000`).

## What Gets Seeded

Three batches inserted with backdated `SYSTEM_TIME` to simulate a timeline:

| Batch | System Time | Contents |
|-------|------------|----------|
| 1 | Feb 13 4:30 PM EST | 3 invoices (CLOSED), $10K payment, composite match proposal (model v1.2, 87.4% confidence), AR Analyst approval, NetSuite ERP batch |
| 2 | Feb 18 12:00 PM EST | Model v1.4 re-evaluation (51.2% confidence, QUEUED for Controller review) |
| 3 | Mar 3 10:00 AM EST | $200 credit memo on I-7003 (system time Mar 3, valid time Feb 24) |

## Queries

### Scenario 1 — Decision State Reconstruction

- **1A** Transaction-time snapshot at Feb 13: invoices + decision + match proposal
- **1B** Same snapshot joined with payment details (payer, remittance, channel)
- **1C** Audit chain closure: decision &rarr; NetSuite ERP batch

### Scenario 1+ — Model Evolution

- **2** Model v1.2 (APPROVED) vs v1.4 (QUEUED) on the same payment

### Scenario 2 — Backdated Credit Memo

- **3A** `FOR SYSTEM_TIME AS OF` Feb 25 &rarr; zero rows (credit memo not yet entered)
- **3B** `FOR VALID_TIME AS OF` Feb 25 &rarr; one row (business fact existed since Feb 24)

## Static Export

Visit http://localhost:3000/api/export to download a self-contained HTML file with all query results pre-baked. Opens in any browser, no server needed.

## Project Structure

```
main.go                 CLI entry point, seeding, queries
server.go               HTTP server, API endpoints, static export
templates/index.html    UI (inline CSS + JS, embedded via go:embed)
docker-compose.yml      XTDB on port 5432
```

## How It Works

XTDB exposes a PostgreSQL wire protocol. The demo uses `pgx/v5` (standard Go Postgres driver) with two XTDB-specific patterns:

- **`INSERT INTO table RECORDS $1`** with a JSON document parameter (OID 114) for schemaless inserts
- **`BEGIN READ WRITE WITH (SYSTEM_TIME = ...)`** to backdate transaction timestamps for historical simulation

Temporal queries use standard SQL:2011 syntax: `FOR SYSTEM_TIME AS OF` and `FOR VALID_TIME AS OF`.
