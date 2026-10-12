// Command testapp is a minimal Compulyst test workload.
//
// Behaviour is driven by release.json baked into the image, so each Git commit
// produces a distinct, reproducible release (healthy v1, healthy v2, broken, ...).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type releaseConfig struct {
	Version string `json:"version"`
	// Healthy=false makes /healthz return 503 so the Docker HEALTHCHECK fails.
	Healthy bool `json:"healthy"`
	// CrashAfterSeconds > 0 makes the process exit(1) after that many seconds.
	CrashAfterSeconds int `json:"crash_after_seconds"`
}

func loadConfig() releaseConfig {
	cfg := releaseConfig{Version: "unknown", Healthy: true}
	b, err := os.ReadFile("/app/release.json")
	if err != nil {
		log.Printf("release.json: %v (using defaults)", err)
		return cfg
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		log.Printf("release.json: %v (using defaults)", err)
	}
	return cfg
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		resp, err := (&http.Client{Timeout: 2 * time.Second}).Get("http://127.0.0.1:8080/healthz")
		if err != nil || resp.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		os.Exit(0)
	}

	cfg := loadConfig()
	host, _ := os.Hostname()
	var (
		draining atomic.Bool
		inflight sync.WaitGroup
		requests atomic.Int64
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		if !cfg.Healthy {
			http.Error(w, "unhealthy by config", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if draining.Load() {
			http.Error(w, "draining", http.StatusServiceUnavailable)
			return
		}
		inflight.Add(1)
		defer inflight.Done()
		n := requests.Add(1)
		log.Printf("request #%d %s %s", n, r.Method, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"version":        cfg.Version,
			"hostname":       host,
			"path":           r.URL.Path,
			"secret_present": os.Getenv("APP_SECRET") != "",
			"greeting":       os.Getenv("GREETING"),
		})
	})
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		if draining.Load() {
			http.Error(w, "draining", http.StatusServiceUnavailable)
			return
		}
		inflight.Add(1)
		defer inflight.Done()
		ms, _ := strconv.Atoi(r.URL.Query().Get("ms"))
		if ms <= 0 {
			ms = 5000
		}
		log.Printf("slow request start %dms", ms)
		time.Sleep(time.Duration(ms) * time.Millisecond)
		log.Printf("slow request done %dms", ms)
		fmt.Fprintf(w, "slow done after %dms version=%s\n", ms, cfg.Version)
	})
	mux.HandleFunc("/_platform/drain", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		draining.Store(true)
		log.Printf("drain requested by %s; waiting for in-flight requests", r.RemoteAddr)
		inflight.Wait()
		log.Printf("drain complete")
		fmt.Fprintln(w, "drained")
	})

	srv := &http.Server{Addr: ":8080", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("testapp version=%s healthy=%v crash_after=%ds listening on :8080",
		cfg.Version, cfg.Healthy, cfg.CrashAfterSeconds)

	if cfg.CrashAfterSeconds > 0 {
		go func() {
			time.Sleep(time.Duration(cfg.CrashAfterSeconds) * time.Second)
			log.Printf("crashing by config")
			os.Exit(1)
		}()
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	log.Printf("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}
