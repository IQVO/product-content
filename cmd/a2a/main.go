// Command a2a is the composition root for the Product Content A2A agent: it
// wires env config into the Postgres repositories, the repositories into the
// SAME GetProductContent and ListCategories read use cases the REST adapter
// uses, then serves Agent2Agent JSON-RPC over HTTP with the Agent Card at the
// well-known path (ADR 0006). It is a second, independent deployable beside
// cmd/api (REST, the outbox relay and the offer-availability consumer).
//
// It reads the same Postgres as cmd/api: DATABASE_URL is mandatory and its
// absence is a boot error that names it; there is no in-memory store. It is
// READ-ONLY: it never writes, never enqueues an outbox row, and has no Kafka
// producer, consumer, relay or scheduler (only cmd/api drains outbox_events and
// consumes offer-availability events), so it needs no KAFKA_BROKERS. A missing
// or malformed mandatory variable is a boot error naming it, raised before
// telemetry, any connection and the listener.
//
// It calls no other bounded context and contains no outbound A2A client.
//
// Environment:
//
//	DATABASE_URL                 REQUIRED. Postgres DSN
//	A2A_AGENT_URL                REQUIRED. Absolute http(s) URL of this agent's
//	                             JSON-RPC endpoint, advertised on the Agent Card
//	MIGRATIONS_DATABASE_URL      direct DSN for the boot migrations (default DATABASE_URL)
//	A2A_ADDR                     listen address (default :8093)
//	LOG_LEVEL                    debug | info | warn | error (default info)
//	OTEL_SERVICE_NAME            service.name of spans and metrics (default product-content-a2a)
//	OTEL_EXPORTER_OTLP_ENDPOINT  Collector OTLP/gRPC endpoint (default telemetry.DefaultEndpoint)
//	SERVICE_VERSION              service.version (default dev)
//	ENVIRONMENT                  deployment.environment.name
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	inbounda2a "github.com/IQVO/product-content/internal/adapters/inbound/a2a"
	"github.com/IQVO/product-content/internal/adapters/outbound/postgres"
	"github.com/IQVO/product-content/internal/adapters/outbound/telemetry"
	"github.com/IQVO/product-content/internal/application/usecases"
	"github.com/IQVO/product-content/internal/bootretry"
)

// shutdownTimeout bounds the HTTP server's graceful drain.
const shutdownTimeout = 10 * time.Second

// defaultServiceName is the OTEL_SERVICE_NAME default of this binary.
const defaultServiceName = "product-content-a2a"

// Boot errors for the variables the agent cannot run without. Each message
// names the variable.
var (
	errMissingDatabaseURL = errors.New("DATABASE_URL is required: this context runs on real Postgres, there is no in-memory mode")
	errMissingAgentURL    = errors.New("A2A_AGENT_URL is required: the Agent Card must advertise the absolute URL of this agent's JSON-RPC endpoint (ADR 0006), e.g. http://<release>-a2a.<ns>.svc.cluster.local:8093/")
)

// bootConfig is the mandatory configuration of cmd/a2a: real Postgres and the
// URL the Agent Card advertises, with no default and no degraded mode behind
// either.
type bootConfig struct {
	databaseURL string
	agentURL    string
}

// checkAgentURL validates A2A_AGENT_URL: an absolute http(s) URL. The card is
// the discovery contract; a relative or schemeless URL is a misconfiguration
// every client would hit, so it is refused at boot, never silently served.
func checkAgentURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return errMissingAgentURL
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("A2A_AGENT_URL=%q is not an absolute http(s) URL: the Agent Card must advertise the endpoint clients dial (ADR 0006)", raw)
	}
	return nil
}

// loadBootConfig reads and validates the mandatory environment. Every problem
// is reported at once, each naming its variable, so the process fails fast at
// boot, before telemetry, any connection and the listener.
func loadBootConfig() (bootConfig, error) {
	cfg := bootConfig{
		databaseURL: os.Getenv("DATABASE_URL"),
		agentURL:    strings.TrimSpace(os.Getenv("A2A_AGENT_URL")),
	}
	var errs []error
	if cfg.databaseURL == "" {
		errs = append(errs, errMissingDatabaseURL)
	}
	if err := checkAgentURL(cfg.agentURL); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return bootConfig{}, errors.Join(errs...)
	}
	return cfg, nil
}

func main() {
	if err := run(); err != nil {
		slog.Error("a2a agent exited with error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	logger := newLogger(getenv("LOG_LEVEL", "info"))
	slog.SetDefault(logger)

	// Fail fast, before telemetry, any connection and the listener.
	cfg, err := loadBootConfig()
	if err != nil {
		return err
	}

	// Telemetry comes up before the router is built (otelchimetric binds the
	// global MeterProvider at construction) and is flushed LAST.
	flushTelemetry, serviceName, err := startTelemetry(logger)
	if err != nil {
		return err
	}
	defer flushTelemetry()

	ctx := context.Background()
	deps, closeAdapters, err := buildDeps(ctx, logger, cfg.databaseURL, getenv("MIGRATIONS_DATABASE_URL", cfg.databaseURL))
	if err != nil {
		return err
	}
	defer closeAdapters()

	addr := getenv("A2A_ADDR", ":8093")
	srv := &http.Server{
		Addr:              addr,
		Handler:           newAgent(deps, cfg.agentURL, getenv("SERVICE_VERSION", telemetry.DefaultServiceVersion), serviceName),
		ReadHeaderTimeout: 5 * time.Second,
	}
	return serveUntilSignal(logger, srv)
}

// buildDeps wires the Postgres repositories into the two read use cases, running
// the embedded migrations first (idempotent, like cmd/api, so the agent also
// boots against a database the API has not migrated yet). The migrations use
// migrationsURL, the pooled application traffic uses databaseURL: a direct DSN
// is needed for the session-scoped advisory lock of golang-migrate.
//
// Only the read side is built: no outbox, inbox, unit of work, catalogue-item
// repository, clock or id generator, because no skill writes.
func buildDeps(ctx context.Context, logger *slog.Logger, databaseURL, migrationsURL string) (inbounda2a.Deps, func(), error) {
	noop := func() {}
	// The first outbound dial of an injected pod can be reset (Istio native
	// sidecars), so migrations and the first ping retry with backoff; on
	// exhaustion the LAST error is returned and the process refuses to boot.
	if err := bootretry.Retry(ctx, logger, "run migrations", func() error { return postgres.RunMigrations(migrationsURL) }); err != nil {
		return inbounda2a.Deps{}, noop, err
	}
	pool, err := postgres.NewPool(ctx, databaseURL)
	if err != nil {
		return inbounda2a.Deps{}, noop, err
	}
	if err := bootretry.Retry(ctx, logger, "ping postgres", func() error { return pool.Ping(ctx) }); err != nil {
		pool.Close()
		return inbounda2a.Deps{}, noop, err
	}
	logger.Info("postgres read side configured")
	return readDeps(pool), pool.Close, nil
}

// readDeps builds the A2A skill dependencies over pool.
func readDeps(pool *pgxpool.Pool) inbounda2a.Deps {
	d := usecases.Deps{Contents: postgres.NewContentRepository(pool), Categories: postgres.NewTreeRepository(pool)}
	return inbounda2a.Deps{
		GetProductContent: &usecases.GetProductContent{Deps: d},
		ListCategories:    &usecases.ListCategories{Deps: d},
	}
}

// newAgent composes the A2A surface: the executor over deps, the Agent Card
// advertising agentURL, and the HTTP router around them.
func newAgent(deps inbounda2a.Deps, agentURL, version, serviceName string) http.Handler {
	card := inbounda2a.Card(inbounda2a.Config{AgentURL: agentURL, Version: version})
	return newRouter(inbounda2a.Handler(inbounda2a.NewExecutor(deps)), card, serviceName)
}

// serveUntilSignal runs srv until SIGINT/SIGTERM (or a listen error), then
// drains it.
func serveUntilSignal(logger *slog.Logger, srv *http.Server) error {
	errCh := make(chan error, 1)
	go func() {
		logger.Info("a2a agent listening (JSON-RPC over HTTP)", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	stopCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-errCh:
		return err
	case <-stopCtx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// startTelemetry brings OpenTelemetry up and returns the flush func to defer
// and the service name. Export is non-blocking: an unreachable Collector costs
// telemetry, never availability.
func startTelemetry(logger *slog.Logger) (func(), string, error) {
	serviceName := getenv("OTEL_SERVICE_NAME", defaultServiceName)
	endpoint := getenv("OTEL_EXPORTER_OTLP_ENDPOINT", telemetry.DefaultEndpoint)
	shutdown, err := telemetry.Setup(context.Background(), serviceName, getenv("SERVICE_VERSION", telemetry.DefaultServiceVersion), endpoint)
	if err != nil {
		return nil, "", err
	}
	logger.Info("telemetry configured", "service_name", serviceName, "otlp_endpoint", endpoint)
	return func() {
		flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdown(flushCtx); err != nil {
			logger.Warn("telemetry flush failed on shutdown", "error", err)
		}
	}, serviceName, nil
}

// newLogger builds the JSON logger; LOG_LEVEL debug|info|warn|warning|error
// (case-insensitive). The handler is wrapped so a record written with a
// span-carrying context also carries trace_id and span_id.
func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(telemetry.WithTraceContext(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})))
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
