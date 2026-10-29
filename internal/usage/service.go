package usage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"opencode2api/internal/config"
	modelcatalog "opencode2api/internal/models"
	"opencode2api/internal/store"
	"opencode2api/internal/telemetry"
)

// Pricer resolves the published per-million-token price of a model. The
// RuntimeController implements it from the OpenCode metadata registry.
type Pricer interface {
	Price(model string) (modelcatalog.Price, bool)
}

// Options configures the usage service.
type Options struct {
	Config      config.UsageConfig
	ConfigPath  string
	Logger      *slog.Logger
	Pricer      Pricer
	Root        context.Context
	WarpEnabled bool
}

// Service owns the usage store, async recorder and query methods.
type Service struct {
	store        *store.Store
	recorder     *Recorder
	logger       *slog.Logger
	pricer       Pricer
	warpEnabled  bool
	path         string
	queryTimeout time.Duration
	cancel       context.CancelFunc
	retentionWG  sync.WaitGroup
	closeOnce    sync.Once
	closeErr     error
}

// New opens the database, starts the writer and schedules retention.
func New(options Options) (*Service, error) {
	path, err := databasePath(options.ConfigPath, options.Config.Database)
	if err != nil {
		return nil, err
	}
	database, err := store.Open(path)
	if err != nil {
		return nil, err
	}
	database.SetRetentionDays(options.Config.RetentionDays)
	root := options.Root
	if root == nil {
		root = context.Background()
	}
	ctx, cancel := context.WithCancel(root)
	recorder := newRecorder(database, options.Logger,
		options.Config.BatchSize, time.Duration(options.Config.FlushIntervalMS)*time.Millisecond)
	recorder.Start(ctx)
	queryTimeout := time.Duration(options.Config.QueryTimeoutMS) * time.Millisecond
	if queryTimeout <= 0 {
		queryTimeout = 5 * time.Second
	}
	service := &Service{
		store: database, recorder: recorder, logger: options.Logger, pricer: options.Pricer,
		warpEnabled: options.WarpEnabled, path: path, queryTimeout: queryTimeout, cancel: cancel,
	}
	service.retentionWG.Add(1)
	go func() {
		defer service.retentionWG.Done()
		service.retentionLoop(ctx)
	}()
	return service, nil
}

// Close flushes the queue and closes the database.
func (s *Service) Close(ctx context.Context) error {
	s.closeOnce.Do(func() {
		if ctx == nil {
			ctx = context.Background()
		}
		s.cancel()
		s.retentionWG.Wait()
		if err := s.recorder.Close(ctx); err != nil {
			s.closeErr = err
			return
		}
		if err := s.store.Close(); err != nil {
			s.closeErr = err
		}
	})
	return s.closeErr
}

// Path returns the database file path.
func (s *Service) Path() string { return s.path }

// QueryTimeout is the configured read deadline for admin queries.
func (s *Service) QueryTimeout() time.Duration { return s.queryTimeout }

// Stats reports recorder health for diagnostics.
func (s *Service) Stats() map[string]uint64 {
	return map[string]uint64{"written": s.recorder.Written(), "dropped": s.recorder.Dropped()}
}

// Middleware records every inference request after the inner handler returns.
// It must sit inside telemetry.Middleware so the RequestMeta is present.
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isInferencePath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		started := time.Now()
		writer := &responseWriter{ResponseWriter: w}
		next.ServeHTTP(writer, r)
		status := writer.status
		if status == 0 {
			status = http.StatusOK
		}
		meta := telemetry.MetaFromRequest(r)
		event, ok := Normalize(meta, r.URL.Path, status, started, writer.firstWrite)
		if !ok {
			return
		}
		s.record(event)
	})
}

func (s *Service) record(event store.Event) {
	price, known := s.priceFor(event.Model)
	ApplyPrice(&event, price, known)
	if s.warpEnabled && event.Provider == "anonymous" && event.ProxyKind != nil && *event.ProxyKind == "direct" {
		kind := "warp"
		event.ProxyKind = &kind
	}
	s.recorder.Record(event)
}

func (s *Service) priceFor(model string) (Price, bool) {
	if s.pricer == nil {
		return Price{}, false
	}
	price, ok := s.pricer.Price(model)
	if !ok {
		return Price{}, false
	}
	return Price{
		Input: price.Input, Output: price.Output, CacheRead: price.CacheRead,
		CacheWrite: price.CacheWrite, Source: "opencode",
	}, true
}

// Summary aggregates usage with sane defaults.
func (s *Service) Summary(ctx context.Context, filter store.Filter) (store.Summary, error) {
	filter = withDefaults(filter, 7*24*time.Hour)
	return s.store.Summary(ctx, filter)
}

// Events returns raw usage events.
func (s *Service) Events(ctx context.Context, filter store.Filter, beforeID int64, limit int) ([]store.Event, error) {
	return s.store.Events(ctx, filter, beforeID, limit)
}

// Timeseries returns daily totals.
func (s *Service) Timeseries(ctx context.Context, filter store.Filter) ([]store.Point, error) {
	filter = withDefaults(filter, 30*24*time.Hour)
	return s.store.Timeseries(ctx, filter)
}

// Backup writes a consistent copy of the database beside it.
func (s *Service) Backup(ctx context.Context) (string, error) {
	return s.store.Backup(ctx)
}

// Retention rolls up and prunes expired raw events.
func (s *Service) Retention(ctx context.Context) (int64, int, error) {
	return s.store.Retention(ctx)
}

func (s *Service) retentionLoop(ctx context.Context) {
	timer := time.NewTimer(time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			runCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
			deleted, rolled, err := s.store.Retention(runCtx)
			cancel()
			if err != nil && s.logger != nil {
				s.logger.Warn("usage retention failed", "component", "usage", "event", "usage_retention_failed", "error", err)
			} else if s.logger != nil && (deleted > 0 || rolled > 0) {
				s.logger.Info("usage retention completed", "component", "usage", "event", "usage_retention",
					"deleted_events", deleted, "rolled_up_days", rolled)
			}
			timer.Reset(24 * time.Hour)
		}
	}
}

func withDefaults(filter store.Filter, span time.Duration) store.Filter {
	to := filter.To
	if to.IsZero() {
		to = time.Now().UTC()
	}
	if filter.From.IsZero() {
		filter.From = to.Add(-span)
	}
	filter.To = to
	if filter.GroupBy == "" {
		filter.GroupBy = "provider_model"
	}
	if filter.Sort == "" {
		filter.Sort = "total_tokens"
	}
	if filter.Order == "" {
		filter.Order = "desc"
	}
	return filter
}

func databasePath(configPath, configured string) (string, error) {
	configured = strings.TrimSpace(configured)
	if configPath != "" {
		absolute, err := filepath.Abs(configPath)
		if err != nil {
			return "", fmt.Errorf("resolve config path: %w", err)
		}
		configPath = absolute
	}
	if configured == "" {
		if configPath == "" {
			return "", errors.New("usage.database is empty and no config path is available")
		}
		configured = "usage.db"
	}
	if filepath.IsAbs(configured) {
		return filepath.Clean(configured), nil
	}
	if configPath == "" {
		return "", fmt.Errorf("usage.database %q is relative but no config path is available", configured)
	}
	return filepath.Clean(filepath.Join(filepath.Dir(configPath), configured)), nil
}

type responseWriter struct {
	http.ResponseWriter
	status     int
	firstWrite time.Time
}

func (w *responseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if w.firstWrite.IsZero() {
		w.firstWrite = time.Now()
	}
	return w.ResponseWriter.Write(data)
}

func (w *responseWriter) Flush() {
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
