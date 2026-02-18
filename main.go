package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/jackc/pgx/v5"
)

// PostgreSQL JSON type OID — required for XTDB's INSERT INTO ... RECORDS $1 syntax
const jsonOID = 114

// ---------------------------------------------------------------------------
// Table schemas (illustrative — XTDB infers schema from first insert)
//
//   invoice:          _id, merchant_id, entity_id, currency, amount, status, due_date, issued_at
//   payment:          _id, payer_name, amount, currency, value_date, bank_ref, channel, raw_ref_text
//   credit_memo:      _id, invoice_id, amount, reason, issued_date
//   match_proposal:   _id, proposal_type, payment_ids, invoice_ids, cm_ids, confidence, model_version
//   decision_journal: _id, proposal_id, payment_id, outcome, actor_user_id, actor_role, reason_summary, tolerance_used
//   erp_upload_batch: _id, erp, batch_type, submitted_by, status
// ---------------------------------------------------------------------------

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()

	host := envOr("XTDB_HOST", "localhost")
	port := envOr("XTDB_PORT", "5432")
	conn, err := pgx.Connect(ctx, fmt.Sprintf("postgres://xtdb:xtdb@%s:%s/xtdb", host, port))
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close(ctx)
	fmt.Println("Connected to XTDB")

	cmd := "demo"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	switch cmd {
	case "seed":
		return seedAll(ctx, conn)
	case "cli":
		if err := seedAll(ctx, conn); err != nil {
			return err
		}
		return runCLIQueries(ctx, conn)
	case "serve":
		addr := envOr("ADDR", ":3000")
		return startServer(ctx, conn, addr)
	case "demo":
		if err := seedAll(ctx, conn); err != nil {
			return err
		}
		addr := envOr("ADDR", ":3000")
		return startServer(ctx, conn, addr)
	default:
		return fmt.Errorf("unknown command: %s (use: demo, seed, serve, cli)", cmd)
	}
}

// ---------------------------------------------------------------------------
// Seed data
// ---------------------------------------------------------------------------

func seedAll(ctx context.Context, conn *pgx.Conn) error {
	fmt.Println()
	for _, s := range []struct {
		label string
		fn    func(context.Context, *pgx.Conn) error
	}{
		{"Batch 1 — Feb 13 decision state (sys_time=2026-02-13T21:30Z)", seedBatch1},
		{"Batch 2 — Model v1.4 comparison  (sys_time=2026-02-18T17:00Z)", seedBatch2},
		{"Batch 3 — Credit memo arrives     (sys_time=2026-03-03T15:00Z, valid_from=Feb 24)", seedBatch3},
	} {
		fmt.Printf("  %s ... ", s.label)
		if err := s.fn(ctx, conn); err != nil {
			fmt.Println("FAILED")
			return fmt.Errorf("seed: %w", err)
		}
		fmt.Println("ok")
	}
	return nil
}

func seedBatch1(ctx context.Context, conn *pgx.Conn) error {
	// Feb 13 4:30 PM EST = 21:30 UTC
	if err := execDML(ctx, conn, "BEGIN READ WRITE WITH (SYSTEM_TIME = TIMESTAMP '2026-02-13T21:30:00Z')"); err != nil {
		return err
	}

	for _, inv := range []map[string]any{
		{"_id": "I-7001", "merchant_id": "M-100", "entity_id": "ENTITY-US", "currency": "USD", "amount": 5000.00, "status": "CLOSED", "due_date": "2026-02-28", "issued_at": "2026-01-15T09:00:00Z"},
		{"_id": "I-7002", "merchant_id": "M-100", "entity_id": "ENTITY-US", "currency": "USD", "amount": 3200.00, "status": "CLOSED", "due_date": "2026-02-28", "issued_at": "2026-01-20T11:00:00Z"},
		{"_id": "I-7003", "merchant_id": "M-100", "entity_id": "ENTITY-US", "currency": "USD", "amount": 1800.00, "status": "CLOSED", "due_date": "2026-02-28", "issued_at": "2026-01-22T14:00:00Z"},
	} {
		if err := insertRecord(ctx, conn, "invoice", inv); err != nil {
			return err
		}
	}

	if err := insertRecord(ctx, conn, "payment", map[string]any{
		"_id": "P-9001", "payer_name": "Acme Corp", "amount": 10000.00, "currency": "USD",
		"value_date": "2026-02-13", "bank_ref": "BANKREF-441892", "channel": "ACH",
		"raw_ref_text": "PAYMENT REF ACME FEB INVOICES CONSOLIDATED PMT",
	}); err != nil {
		return err
	}

	if err := insertRecord(ctx, conn, "match_proposal", map[string]any{
		"_id": "MP-001", "proposal_type": "COMPOSITE", "payment_ids": "P-9001",
		"invoice_ids": "I-7001,I-7002,I-7003", "confidence": 0.8740, "model_version": "v1.2",
	}); err != nil {
		return err
	}

	if err := insertRecord(ctx, conn, "decision_journal", map[string]any{
		"_id": "D-5001", "proposal_id": "MP-001", "payment_id": "P-9001",
		"outcome": "APPROVED", "actor_user_id": "U-211", "actor_role": "AR_ANALYST",
		"reason_summary": "Composite match: I-7001 + I-7002 + I-7003 sums to $10,000. Approved per model recommendation.",
		"tolerance_used": 0.00,
	}); err != nil {
		return err
	}

	if err := insertRecord(ctx, conn, "erp_upload_batch", map[string]any{
		"_id": "B-20260213-01", "erp": "NetSuite", "batch_type": "manager_entry_upload",
		"submitted_by": "U-211", "status": "SUBMITTED",
	}); err != nil {
		return err
	}

	return execDML(ctx, conn, "COMMIT")
}

func seedBatch2(ctx context.Context, conn *pgx.Conn) error {
	if err := execDML(ctx, conn, "BEGIN READ WRITE WITH (SYSTEM_TIME = TIMESTAMP '2026-02-18T17:00:00Z')"); err != nil {
		return err
	}

	if err := insertRecord(ctx, conn, "match_proposal", map[string]any{
		"_id": "MP-003", "proposal_type": "TOLERANCE", "payment_ids": "P-9001",
		"invoice_ids": "I-7001,I-7002", "confidence": 0.5120, "model_version": "v1.4",
	}); err != nil {
		return err
	}

	if err := insertRecord(ctx, conn, "decision_journal", map[string]any{
		"_id": "D-5003", "proposal_id": "MP-003", "payment_id": "P-9001",
		"outcome": "QUEUED", "actor_user_id": "SYSTEM", "actor_role": "AI_ENGINE",
		"reason_summary": "v1.4: Confidence below 0.75 threshold. Dirty remittance, no invoice refs. Escalated to Controller.",
	}); err != nil {
		return err
	}

	return execDML(ctx, conn, "COMMIT")
}

func seedBatch3(ctx context.Context, conn *pgx.Conn) error {
	// System time: Mar 3 (when it was entered)
	// Valid time (_valid_from): Feb 24 (when the business fact was effective)
	if err := execDML(ctx, conn, "BEGIN READ WRITE WITH (SYSTEM_TIME = TIMESTAMP '2026-03-03T15:00:00Z')"); err != nil {
		return err
	}

	if err := insertRecord(ctx, conn, "credit_memo", map[string]any{
		"_id":         "CM-501",
		"invoice_id":  "I-7003",
		"amount":      200.00,
		"reason":      "Pricing adjustment — approved by sales Feb 24",
		"issued_date": "2026-02-24",
		"_valid_from": "2026-02-24T00:00:00Z",
	}); err != nil {
		return err
	}

	return execDML(ctx, conn, "COMMIT")
}

// ---------------------------------------------------------------------------
// Queries
// ---------------------------------------------------------------------------

// Query 1A: System-time snapshot at Feb 13 4:30 PM EST.
// Shows three closed invoices, the approved composite match, and model v1.2.
var query1A = `
SELECT
  inv._id             AS invoice_id,
  inv.status,
  inv.amount,
  dj._id              AS decision_id,
  dj.outcome,
  dj.actor_role,
  dj.reason_summary,
  mp.proposal_type,
  mp.confidence,
  mp.model_version
FROM invoice FOR SYSTEM_TIME AS OF TIMESTAMP '2026-02-13T21:30:00Z' inv
JOIN decision_journal FOR SYSTEM_TIME AS OF TIMESTAMP '2026-02-13T21:30:00Z' dj
  ON dj.payment_id = 'P-9001'
JOIN match_proposal FOR SYSTEM_TIME AS OF TIMESTAMP '2026-02-13T21:30:00Z' mp
  ON mp._id = dj.proposal_id
WHERE inv.merchant_id = 'M-100'
  AND dj.outcome = 'APPROVED'
`

// Query 1B: Same snapshot with payment details (payer, raw remittance text, channel).
var query1B = `
SELECT
  inv._id             AS invoice_id,
  inv.status,
  inv.amount,
  p.payer_name,
  p.raw_ref_text,
  p.channel,
  dj._id              AS decision_id,
  dj.outcome,
  dj.actor_role,
  mp.proposal_type,
  mp.confidence,
  mp.model_version
FROM decision_journal FOR SYSTEM_TIME AS OF TIMESTAMP '2026-02-13T21:30:00Z' dj
JOIN payment FOR SYSTEM_TIME AS OF TIMESTAMP '2026-02-13T21:30:00Z' p
  ON p._id = dj.payment_id
JOIN match_proposal FOR SYSTEM_TIME AS OF TIMESTAMP '2026-02-13T21:30:00Z' mp
  ON mp._id = dj.proposal_id
JOIN invoice FOR SYSTEM_TIME AS OF TIMESTAMP '2026-02-13T21:30:00Z' inv
  ON inv.merchant_id = 'M-100'
WHERE dj.payment_id = 'P-9001'
  AND dj.outcome = 'APPROVED'
`

// Query 1C: Audit chain closure — did the approved decision reach NetSuite?
var query1C = `
SELECT
  dj._id              AS decision_id,
  dj.outcome,
  dj.actor_role,
  eb._id              AS batch_id,
  eb.erp,
  eb.batch_type,
  eb.submitted_by,
  eb.status
FROM decision_journal FOR SYSTEM_TIME AS OF TIMESTAMP '2026-02-13T21:30:00Z' dj
JOIN erp_upload_batch FOR SYSTEM_TIME AS OF TIMESTAMP '2026-02-13T21:30:00Z' eb
  ON eb.submitted_by = dj.actor_user_id
WHERE dj.payment_id = 'P-9001'
  AND dj.outcome = 'APPROVED'
`

// Query 2: Model version comparison — v1.2 (approved) vs v1.4 (queued).
// No temporal qualifier needed; both versions exist in current state.
var query2 = `
SELECT
  mp.model_version,
  mp.confidence,
  mp.proposal_type,
  dj.outcome,
  dj.reason_summary
FROM match_proposal mp
JOIN decision_journal dj ON dj.proposal_id = mp._id
WHERE mp.payment_ids = 'P-9001'
ORDER BY mp.model_version
`

// Query 3A: Transaction-time query at Feb 25.
// Credit memo was entered on Mar 3 — the system didn't know about it yet.
// Expected: zero rows.
var query3A = `
SELECT
  _id          AS cm_id,
  invoice_id,
  amount,
  reason,
  issued_date
FROM credit_memo
  FOR SYSTEM_TIME AS OF TIMESTAMP '2026-02-25T16:00:00Z'
WHERE invoice_id = 'I-7003'
`

// Query 3B: Valid-time query at Feb 25.
// Credit memo effective date was Feb 24 — it existed as a business fact.
// Expected: one row (CM-501).
var query3B = `
SELECT
  _id          AS cm_id,
  invoice_id,
  amount,
  reason,
  issued_date
FROM credit_memo
  FOR VALID_TIME AS OF TIMESTAMP '2026-02-25T16:00:00Z'
WHERE invoice_id = 'I-7003'
`

// ---------------------------------------------------------------------------
// CLI query output
// ---------------------------------------------------------------------------

func runCLIQueries(ctx context.Context, conn *pgx.Conn) error {
	section("SCENARIO 1: Reconstruct Decision State When Models Evolve")

	fmt.Println("Query 1A — What did the system know and decide at Feb 13 4:30 PM EST?")
	fmt.Println("           Transaction-time snapshot: invoices + decision + match proposal")
	if err := queryPrint(ctx, conn, query1A); err != nil {
		return fmt.Errorf("query 1A: %w", err)
	}

	fmt.Println("\nQuery 1B — Same snapshot joined with payment details")
	if err := queryPrint(ctx, conn, query1B); err != nil {
		return fmt.Errorf("query 1B: %w", err)
	}

	fmt.Println("\nQuery 1C — Audit chain: did the approved decision reach NetSuite?")
	if err := queryPrint(ctx, conn, query1C); err != nil {
		return fmt.Errorf("query 1C: %w", err)
	}

	fmt.Println("\nQuery 2  — Model v1.2 vs v1.4 on the same payment")
	fmt.Println("           Answers: how do you reconstruct decision state when models evolve?")
	if err := queryPrint(ctx, conn, query2); err != nil {
		return fmt.Errorf("query 2: %w", err)
	}

	section("SCENARIO 2: Backdated Credit Memo")

	fmt.Println("Query 3A — Transaction time: what did the system have recorded by Feb 25?")
	fmt.Println("           Credit memo was entered Mar 3 — system didn't know about it yet")
	if err := queryPrint(ctx, conn, query3A); err != nil {
		return fmt.Errorf("query 3A: %w", err)
	}

	fmt.Println("\nQuery 3B — Valid time: what was true in the business world on Feb 25?")
	fmt.Println("           Credit memo effective Feb 24 — it existed as a business fact")
	if err := queryPrint(ctx, conn, query3B); err != nil {
		return fmt.Errorf("query 3B: %w", err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func insertRecord(ctx context.Context, conn *pgx.Conn, table string, doc map[string]any) error {
	data, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	sql := fmt.Sprintf("INSERT INTO %s RECORDS $1", table)
	res := conn.PgConn().ExecParams(ctx, sql,
		[][]byte{data}, []uint32{jsonOID}, []int16{0}, []int16{0})
	_, err = res.Close()
	if err != nil {
		return fmt.Errorf("insert into %s: %w", table, err)
	}
	return nil
}

func execDML(ctx context.Context, conn *pgx.Conn, sql string) error {
	res := conn.PgConn().ExecParams(ctx, sql, nil, nil, nil, nil)
	_, err := res.Close()
	return err
}

func queryPrint(ctx context.Context, conn *pgx.Conn, sql string) error {
	rows, err := conn.Query(ctx, sql)
	if err != nil {
		return fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	descs := rows.FieldDescriptions()
	headers := make([]string, len(descs))
	for i, d := range descs {
		headers[i] = string(d.Name)
	}

	var data [][]string
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return fmt.Errorf("scan: %w", err)
		}
		row := make([]string, len(vals))
		for i, v := range vals {
			if v == nil {
				row[i] = "NULL"
			} else {
				s := fmt.Sprintf("%v", v)
				if len(s) > 72 {
					s = s[:69] + "..."
				}
				row[i] = s
			}
		}
		data = append(data, row)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	fmt.Println()
	if len(data) == 0 {
		fmt.Println("  (no rows)")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  "+strings.Join(headers, "\t"))
	seps := make([]string, len(headers))
	for i, h := range headers {
		seps[i] = strings.Repeat("-", len(h))
	}
	fmt.Fprintln(w, "  "+strings.Join(seps, "\t"))
	for _, row := range data {
		fmt.Fprintln(w, "  "+strings.Join(row, "\t"))
	}
	w.Flush()
	fmt.Printf("  (%d rows)\n", len(data))
	return nil
}

func section(title string) {
	fmt.Printf("\n%s\n%s\n\n", title, strings.Repeat("=", len(title)))
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
