// SkinVerdict server: one static binary. Serves the API, scan files and the
// embedded Preact front end; runs the check-in scheduler in-process.
//
//	YOUCAM_API_KEY=...  go run ./cmd/server
//
// Flags/env: -addr (:8080), -data (./data-local), -catalog (./data/catalog.json),
// -check-gap (168h in prod; use e.g. 2m for demos), -dev (proxy / to Vite on :5173),
// -scans-per-hour (60). ADMIN_TOKEN enables /api/admin/* (Bearer auth); unset, they 404.
package main

import (
	"context"
	"database/sql"
	"flag"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/manasnikam/skinverdict/internal/agent"
	"github.com/manasnikam/skinverdict/internal/api"
	"github.com/manasnikam/skinverdict/internal/app"
	"github.com/manasnikam/skinverdict/internal/catalog"
	"github.com/manasnikam/skinverdict/internal/sched"
	"github.com/manasnikam/skinverdict/internal/store"
	"github.com/manasnikam/skinverdict/internal/verdict"
	"github.com/manasnikam/skinverdict/internal/youcam"
	"github.com/manasnikam/skinverdict/web"
	_ "github.com/ncruces/go-sqlite3/driver" // registers the "sqlite3" database/sql driver (pure Go, no cgo)
	_ "github.com/ncruces/go-sqlite3/embed"  // embedded SQLite build (wasm)
)

func main() {
	addr := flag.String("addr", env("ADDR", ":8080"), "listen address")
	dataDir := flag.String("data", env("DATA_DIR", "./data-local"), "writable dir for db, scans, masks, cache")
	catPath := flag.String("catalog", env("CATALOG", "./data/catalog.json"), "catalog json")
	gap := flag.Duration("check-gap", mustDur(env("CHECK_GAP", "168h")), "time between check-ins")
	dev := flag.String("dev", env("VITE_DEV", ""), "proxy non-API requests to this Vite dev server, e.g. http://localhost:5173")
	dbPath := flag.String("db", env("DB_PATH", ""), "sqlite file (default <data>/skinverdict.db); 'json' uses the JSON store")
	agentMode := flag.String("agent", env("AGENT", "claude"), "claude (Claude Code CLI, falls back to rules) | rules")
	claudeBin := flag.String("claude-bin", env("CLAUDE_BIN", "claude"), "path to the claude CLI")
	scansPerHour := flag.Int("scans-per-hour", mustInt(env("SCANS_PER_HOUR", "60")), "server-wide cap on YouCam scans (9 units each); 0 = unlimited")
	flag.Parse()

	key := os.Getenv("YOUCAM_API_KEY")
	if key == "" {
		log.Fatal("YOUCAM_API_KEY not set")
	}
	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		log.Fatal(err)
	}
	cat, err := catalog.Load(*catPath)
	if err != nil {
		log.Fatal(err)
	}
	st, err := openStore(*dataDir, *dbPath)
	if err != nil {
		log.Fatal(err)
	}
	yc := youcam.New(key)
	yc.CacheDir = filepath.Join(*dataDir, "youcam-cache")
	yc.Log = log.Printf

	var ag app.Agent = app.RuleAgent{}
	if *agentMode == "claude" {
		c := agent.NewCLI()
		c.Bin, c.Log = *claudeBin, log.Printf
		if extra := os.Getenv("CLAUDE_ARGS"); extra != "" { // e.g. "--model sonnet"
			c.Args = strings.Fields(extra)
		}
		ag = c
	}
	log.Printf("agent: %s", *agentMode)

	opts := verdict.DefaultOptions()
	opts.Label = cat.Label
	a := &app.App{
		Store: st, Catalog: cat, YouCam: yc, Agent: ag,
		DataDir: *dataDir, Options: opts, CheckGap: *gap,
	}

	mux := http.NewServeMux()
	(&api.Server{App: a, AdminToken: os.Getenv("ADMIN_TOKEN"), ScansPerHour: *scansPerHour}).Routes(mux)
	if *dev != "" {
		u, _ := url.Parse(*dev)
		mux.Handle("/", httputil.NewSingleHostReverseProxy(u))
	} else {
		mux.Handle("/", web.Handler())
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go sched.Run(ctx, a, time.Minute)

	srv := &http.Server{Addr: *addr, Handler: logging(mux), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	log.Printf("skinverdict listening on %s (data=%s, check-gap=%s)", *addr, *dataDir, *gap)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

// openStore opens SQLite at dbPath (default <dataDir>/skinverdict.db).
// Pass "json" to use the dependency-free JSON file store instead.
func openStore(dataDir, dbPath string) (store.Store, error) {
	if dbPath == "json" {
		log.Printf("store: local JSON")
		return store.OpenJSON(filepath.Join(dataDir, "db.json"))
	}
	if dbPath == "" {
		dbPath = filepath.Join(dataDir, "skinverdict.db")
	}
	db, err := sql.Open("sqlite3", "file:"+dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // single writer; SQLite serialises anyway and this avoids SQLITE_BUSY
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return nil, err
	}
	log.Printf("store: sqlite %s", dbPath)
	return store.OpenSQL(ctx, db)
}

func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t := time.Now()
		next.ServeHTTP(w, r)
		if len(r.URL.Path) < 7 || r.URL.Path[:7] != "/files/" {
			log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(t).Round(time.Millisecond))
		}
	})
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func mustInt(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		log.Fatalf("bad integer %q", s)
	}
	return n
}

func mustDur(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		log.Fatalf("bad duration %q", s)
	}
	return d
}
