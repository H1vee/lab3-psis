package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "github.com/lib/pq"
)

var version = "dev"

type Brewing struct {
	Tea        string `json:"tea"`
	Title      string `json:"title"`
	Oxidation  string `json:"oxidation"`
	WaterMinC  int    `json:"water_min_c"`
	WaterMaxC  int    `json:"water_max_c"`
	SecondsMin int    `json:"seconds_min"`
	SecondsMax int    `json:"seconds_max"`
	NeedsRinse bool   `json:"needs_rinse"`
}

type server struct {
	db *sql.DB
}

const brewingQuery = `
SELECT k.code, k.title, k.oxidation,
       b.water_min_c, b.water_max_c, b.seconds_min, b.seconds_max, b.needs_rinse
FROM brewing_params b
JOIN tea_kinds k ON k.id = b.tea_kind_id
WHERE ($1 = '' OR k.code = $1)
ORDER BY k.sort_order`

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// buildDSN assembles the connection string from the environment. The password
// is never written to the log, only the host and database name are reported.
func buildDSN() (dsn string, safe string) {
	host := env("POSTGRES_HOST", "db")
	port := env("POSTGRES_PORT", "5432")
	user := env("POSTGRES_USER", "postgres")
	password := env("POSTGRES_PASSWORD", "")
	database := env("POSTGRES_DB", "postgres")
	sslMode := env("POSTGRES_SSLMODE", "disable")

	dsn = fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		host, port, user, password, database, sslMode)
	safe = fmt.Sprintf("%s@%s:%s/%s", user, host, port, database)
	return dsn, safe
}

func openDatabase(dsn string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)

	deadline := time.Now().Add(60 * time.Second)
	for attempt := 1; ; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err = db.PingContext(ctx)
		cancel()

		if err == nil {
			return db, nil
		}
		if time.Now().After(deadline) {
			db.Close()
			return nil, fmt.Errorf("database is unreachable after %d attempts: %w", attempt, err)
		}

		log.Printf("database is not ready yet (attempt %d), retrying", attempt)
		time.Sleep(2 * time.Second)
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("failed to write response: %v", err)
	}
}

func (s *server) handleBrewing(w http.ResponseWriter, r *http.Request) {
	requested := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("tea")))

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// The filter is passed as a query parameter, never concatenated into SQL.
	rows, err := s.db.QueryContext(ctx, brewingQuery, requested)
	if err != nil {
		log.Printf("query failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "query failed"})
		return
	}
	defer rows.Close()

	result := make([]Brewing, 0, 8)
	for rows.Next() {
		var item Brewing
		if err := rows.Scan(&item.Tea, &item.Title, &item.Oxidation,
			&item.WaterMinC, &item.WaterMaxC, &item.SecondsMin, &item.SecondsMax,
			&item.NeedsRinse); err != nil {
			log.Printf("failed to scan row: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "scan failed"})
			return
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		log.Printf("failed to read rows: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "read failed"})
		return
	}

	if requested != "" && len(result) == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown tea type"})
		return
	}

	w.Header().Set("X-App-Version", version)
	writeJSON(w, http.StatusOK, result)
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := s.db.PingContext(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status":  "degraded",
			"version": version,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"version": version,
	})
}

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"service": "teapot-api",
		"version": version,
		"endpoints": []string{
			"/api/brewing",
			"/api/brewing?tea=oolong",
			"/healthz",
		},
	})
}

func healthURL(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "http://127.0.0.1" + addr + "/healthz"
	}
	return "http://" + addr + "/healthz"
}

func runHealthCheck(addr string) int {
	client := &http.Client{Timeout: 2 * time.Second}

	resp, err := client.Get(healthURL(addr))
	if err != nil {
		log.Printf("health check failed: %v", err)
		return 1
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("health check returned status %d", resp.StatusCode)
		return 1
	}
	return 0
}

func main() {
	addr := flag.String("addr", env("API_ADDR", ":8080"), "address the HTTP server listens on")
	healthcheck := flag.Bool("healthcheck", false, "probe the running server and exit")
	flag.Parse()

	if *healthcheck {
		os.Exit(runHealthCheck(*addr))
	}

	dsn, safe := buildDSN()
	log.Printf("teapot-api %s connecting to %s", version, safe)

	db, err := openDatabase(dsn)
	if err != nil {
		log.Fatalf("startup failed: %v", err)
	}
	defer db.Close()

	app := &server{db: db}

	mux := http.NewServeMux()
	mux.HandleFunc("/", app.handleIndex)
	mux.HandleFunc("/api/brewing", app.handleBrewing)
	mux.HandleFunc("/healthz", app.handleHealth)

	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("teapot-api %s listening on %s", version, *addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("server stopped with error: %v", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Print("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
		os.Exit(1)
	}
	log.Print("server stopped cleanly")
}
