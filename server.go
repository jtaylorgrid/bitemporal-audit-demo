package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
)

//go:embed templates
var templateFS embed.FS

//go:embed architecture.svg
var architectureSVG []byte

var connMu sync.Mutex

var queryMap = map[string]string{
	"1a": query1A,
	"1b": query1B,
	"1c": query1C,
	"2":  query2,
	"3a": query3A,
	"3b": query3B,
}

type QueryResult struct {
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
}

func startServer(ctx context.Context, conn *pgx.Conn, addr string) error {
	mux := http.NewServeMux()

	indexHTML, err := templateFS.ReadFile("templates/index.html")
	if err != nil {
		return fmt.Errorf("read template: %w", err)
	}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
	})

	mux.HandleFunc("/architecture.svg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Write(architectureSVG)
	})

	mux.HandleFunc("/api/query/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/api/query/")
		sql, ok := queryMap[id]
		if !ok {
			http.NotFound(w, r)
			return
		}
		result, err := runQueryJSON(ctx, conn, sql)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})

	mux.HandleFunc("/api/export", func(w http.ResponseWriter, r *http.Request) {
		allData := make(map[string]*QueryResult)
		for id, sql := range queryMap {
			result, err := runQueryJSON(ctx, conn, sql)
			if err != nil {
				http.Error(w, fmt.Sprintf("query %s: %v", id, err), http.StatusInternalServerError)
				return
			}
			allData[id] = result
		}
		dataJSON, _ := json.Marshal(allData)

		html := string(indexHTML)
		injection := fmt.Sprintf("<script>const PRELOADED_DATA = %s;</script>", dataJSON)
		html = strings.Replace(html, "</head>", injection+"\n</head>", 1)
		html = strings.Replace(html, "<title>", "<title>Exported — ", 1)
		html = strings.Replace(html,
			`<img src="/architecture.svg" alt="Architecture" class="arch-diagram">`,
			string(architectureSVG), 1)

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=bitemporal-audit-demo.html")
		fmt.Fprint(w, html)
	})

	fmt.Printf("\n  UI available at http://localhost%s\n", addr)
	fmt.Printf("  Static export at http://localhost%s/api/export\n\n", addr)
	return http.ListenAndServe(addr, mux)
}

func runQueryJSON(ctx context.Context, conn *pgx.Conn, sql string) (*QueryResult, error) {
	connMu.Lock()
	defer connMu.Unlock()

	rows, err := conn.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	descs := rows.FieldDescriptions()
	result := &QueryResult{
		Columns: make([]string, len(descs)),
		Rows:    [][]string{},
	}
	for i, d := range descs {
		result.Columns[i] = string(d.Name)
	}

	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		row := make([]string, len(vals))
		for i, v := range vals {
			if v == nil {
				row[i] = ""
			} else {
				row[i] = fmt.Sprintf("%v", v)
			}
		}
		result.Rows = append(result.Rows, row)
	}

	return result, rows.Err()
}
