// Command mihomo-k8s-exporter exposes Prometheus metrics read from the mihomo
// REST API: subscription expiry and traffic, provider updates, node health.
//
// It runs as a sidecar next to the core, because the API listens on 127.0.0.1
// inside the pod. The API secret is taken from MIHOMO_CONTROLLER_SECRET.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func envOr(name, fallback string) string {
	if v, ok := os.LookupEnv(name); ok && v != "" {
		return v
	}
	return fallback
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	listen := flag.String("listen", envOr("LISTEN_ADDRESS", ":9101"), "address to serve metrics on")
	apiURL := flag.String("mihomo-url", envOr("MIHOMO_URL", "http://127.0.0.1:9090"), "mihomo external-controller URL")
	timeoutStr := flag.String("timeout", envOr("SCRAPE_TIMEOUT", "5s"), "time limit for reading the API on each scrape")
	flag.Parse()

	timeout, err := time.ParseDuration(*timeoutStr)
	if err != nil || timeout <= 0 {
		log.Error("invalid timeout", "value", *timeoutStr)
		os.Exit(2)
	}

	client := NewClient(*apiURL, os.Getenv("MIHOMO_CONTROLLER_SECRET"), timeout)
	reg := prometheus.NewRegistry()
	reg.MustRegister(NewCollector(client, timeout, log))

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("mihomo-k8s-exporter: metrics at /metrics\n"))
	})

	srv := &http.Server{
		Addr:              *listen,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      timeout + 10*time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Info("listening", "address", *listen, "mihomo", *apiURL)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("serve", "err", err)
		os.Exit(1)
	}
}
