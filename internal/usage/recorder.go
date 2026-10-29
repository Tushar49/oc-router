package usage

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"opencode2api/internal/store"
)

// Recorder is a bounded, non-blocking queue and one batched SQLite writer. A
// full queue drops rows rather than delaying an inference response.
type Recorder struct {
	store         *store.Store
	logger        *slog.Logger
	queue         chan store.Event
	batchSize     int
	flushInterval time.Duration
	stop          chan struct{}
	done          chan struct{}
	startOnce     sync.Once
	started       atomic.Bool
	closed        atomic.Bool
	dropped       atomic.Uint64
	written       atomic.Uint64
}

func newRecorder(database *store.Store, logger *slog.Logger, batchSize int, flushInterval time.Duration) *Recorder {
	if batchSize < 1 {
		batchSize = 100
	}
	if flushInterval <= 0 {
		flushInterval = 100 * time.Millisecond
	}
	queueSize := batchSize * 16
	if queueSize < 1024 {
		queueSize = 1024
	}
	return &Recorder{
		store: database, logger: logger, queue: make(chan store.Event, queueSize),
		batchSize: batchSize, flushInterval: flushInterval,
		stop: make(chan struct{}), done: make(chan struct{}),
	}
}

// Start launches the writer. It stops on ctx cancellation or Close.
func (r *Recorder) Start(ctx context.Context) {
	r.startOnce.Do(func() {
		r.started.Store(true)
		go r.loop(ctx)
	})
}

// Record enqueues an event without blocking.
func (r *Recorder) Record(event store.Event) {
	if r.closed.Load() {
		return
	}
	select {
	case r.queue <- event:
	default:
		dropped := r.dropped.Add(1)
		if dropped == 1 || dropped%1000 == 0 {
			r.logf("usage queue full; dropping events", "dropped", dropped)
		}
	}
}

// Close stops the writer after draining the queue.
func (r *Recorder) Close(ctx context.Context) error {
	if !r.started.Load() {
		r.closed.Store(true)
		return nil
	}
	if r.closed.CompareAndSwap(false, true) {
		close(r.stop)
	}
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Dropped returns the number of events discarded because the queue was full.
func (r *Recorder) Dropped() uint64 { return r.dropped.Load() }

// Written returns the number of events committed by the writer.
func (r *Recorder) Written() uint64 { return r.written.Load() }

func (r *Recorder) loop(ctx context.Context) {
	ticker := time.NewTicker(r.flushInterval)
	defer ticker.Stop()
	batch := make([]store.Event, 0, r.batchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		writeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := r.store.InsertBatch(writeCtx, batch)
		cancel()
		if err != nil {
			r.dropped.Add(uint64(len(batch)))
			r.logf("usage batch write failed", "events", len(batch), "error", err)
		} else {
			r.written.Add(uint64(len(batch)))
		}
		batch = batch[:0]
	}
	drain := func() {
		for {
			select {
			case event := <-r.queue:
				batch = append(batch, event)
				if len(batch) >= r.batchSize {
					flush()
				}
			default:
				flush()
				return
			}
		}
	}
	for {
		select {
		case event := <-r.queue:
			batch = append(batch, event)
			if len(batch) >= r.batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-r.stop:
			drain()
			close(r.done)
			return
		case <-ctx.Done():
			drain()
			close(r.done)
			return
		}
	}
}

func (r *Recorder) logf(message string, args ...any) {
	if r.logger == nil {
		return
	}
	r.logger.Warn(message, append([]any{"component", "usage", "event", "usage_recorder"}, args...)...)
}
