package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"opencode-dashboard/internal/model"
	"opencode-dashboard/internal/ports"
)

// Compile-time proof that Hub satisfies the frozen Wave-1 seam contract.
// Caller: internal/collector after each poll tick.
var _ ports.SnapshotSink = (*Hub)(nil)

// ErrHubClosed is returned by Publish/Subscribe after the hub drained.
var ErrHubClosed = errors.New("hub: closed")

const (
	// DefaultTickInterval is the production fan-out cadence. It sits inside
	// the contract window (250–1000ms) so per-row sparklines can cap at 1Hz
	// and streaming series never animate per tick (research §2).
	DefaultTickInterval = 500 * time.Millisecond
	// DefaultKeepaliveInterval is the production SSE comment cadence. It
	// sits inside the contract window (15–30s) so proxies drop idle
	// connections and the UI can render STALE vs reconnecting distinctly.
	DefaultKeepaliveInterval = 20 * time.Second
	// DefaultViewerQueueCap bounds per-viewer memory. A viewer that falls
	// behind is evicted (slow-viewer drop) and counted — the poll loop is
	// never stalled by a slow consumer.
	DefaultViewerQueueCap = 8
	// DefaultRetryMs is the SSE `retry:` hint sent on connect so
	// EventSource backs off 3s before auto-reconnecting.
	DefaultRetryMs = 3000
)

// HubOption tunes a Hub. Zero value options keep production defaults.
type HubOption func(*Hub)

// WithTickInterval sets the fan-out cadence. Production values must stay in
// 250ms–1000ms; tests may pass smaller values for speed.
func WithTickInterval(d time.Duration) HubOption {
	return func(h *Hub) {
		if d > 0 {
			h.tickInterval = d
		}
	}
}

// WithKeepaliveInterval sets the SSE comment cadence. Production values
// must stay in 15s–30s; tests may pass smaller values for speed.
func WithKeepaliveInterval(d time.Duration) HubOption {
	return func(h *Hub) {
		if d > 0 {
			h.keepaliveInterval = d
		}
	}
}

// WithViewerQueueCap sets the per-viewer buffered frame cap.
func WithViewerQueueCap(n int) HubOption {
	return func(h *Hub) {
		if n > 0 {
			h.queueCap = n
		}
	}
}

// frame is one aggregated tick handed to a viewer.
type frame struct {
	seq     uint64
	payload []byte
}

// viewer is one SSE subscriber. ch is buffered to queueCap; when full the
// viewer is evicted (never blocks the poll loop).
type viewer struct {
	id uint64
	ch chan frame
}

// Subscription is a live viewer registration. Unsubscribe is idempotent.
type Subscription struct {
	ch   <-chan frame
	hub  *Hub
	v    *viewer
	once sync.Once
}

// Ch returns the receive-only frame channel. It closes when the viewer is
// evicted (slow consumer) or the hub drains on shutdown.
func (s *Subscription) Ch() <-chan frame {
	return s.ch
}

// Unsubscribe removes the viewer and closes its channel. Safe to call
// multiple times and after eviction.
func (s *Subscription) Unsubscribe() {
	s.once.Do(func() {
		s.hub.remove(s.v)
	})
}

// Hub is the SSE fan-out implementation of ports.SnapshotSink.
//
// Aggregation: Publish stores the latest tick; Run broadcasts at
// tickInterval, coalescing bursts so viewers see at most one frame per
// tick. Broadcasts are non-blocking per viewer: a full queue evicts the
// slow viewer and bumps the drop counters instead of stalling the poll
// loop. Reconnecting viewers re-sync from Latest (full-snapshot replay).
//
// Publish shares the snapshot's backing arrays with broadcast payloads;
// the collector must not mutate a Snapshot after Publish returns.
// Use through a pointer; the zero value is not usable — call NewHub.
type Hub struct {
	mu                sync.RWMutex
	latest            model.Snapshot
	hasSnapshot       bool
	lastBroadcastSeq  uint64
	broadcastDone     bool
	viewers           map[*viewer]struct{}
	tickInterval      time.Duration
	keepaliveInterval time.Duration
	queueCap          int
	closed            bool

	droppedFrames  atomic.Int64
	droppedViewers atomic.Int64
	viewerSeq      atomic.Uint64
}

// NewHub returns a Hub with production defaults overridden by opts.
func NewHub(opts ...HubOption) *Hub {
	h := &Hub{
		viewers:           make(map[*viewer]struct{}),
		tickInterval:      DefaultTickInterval,
		keepaliveInterval: DefaultKeepaliveInterval,
		queueCap:          DefaultViewerQueueCap,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Publish delivers one Snapshot tick. It never blocks on viewers: it stores
// the tick under a short mutex and returns, so the collector poll loop can
// never stall here. Stale ticks are kept (collector owns monotonic Seq).
func (h *Hub) Publish(ctx context.Context, snap model.Snapshot) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("hub: publish: %w", ctx.Err())
	default:
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return ErrHubClosed
	}
	h.latest = snap
	h.hasSnapshot = true
	return nil
}

// Run starts tick-aggregated fan-out and blocks until ctx is cancelled,
// then drains all viewers (closes their channels) and returns nil.
// Every goroutine this hub owns stops here — no leaks past shutdown.
func (h *Hub) Run(ctx context.Context) error {
	ticker := time.NewTicker(h.tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			h.closeAll()
			return nil
		case <-ticker.C:
			h.broadcastTick()
		}
	}
}

// Close drains all viewers immediately. Idempotent; Run also drains on
// context cancel.
func (h *Hub) Close() {
	h.closeAll()
}

// Subscribe registers a viewer and returns its subscription. The caller
// must Unsubscribe (the SSE handler defers it on disconnect).
func (h *Hub) Subscribe() (*Subscription, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, ErrHubClosed
	}
	v := &viewer{id: h.viewerSeq.Add(1), ch: make(chan frame, h.queueCap)}
	h.viewers[v] = struct{}{}
	return &Subscription{ch: v.ch, hub: h, v: v}, nil
}

// Latest returns the newest stored tick. The second return is false until
// the first Publish lands.
func (h *Hub) Latest() (model.Snapshot, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.latest, h.hasSnapshot
}

// ViewerCount reports live viewers. Tests use it to assert drain on
// shutdown (no leaked goroutines per viewer).
func (h *Hub) ViewerCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.viewers)
}

// DroppedFrames counts frames discarded because a viewer queue was full.
func (h *Hub) DroppedFrames() int64 {
	return h.droppedFrames.Load()
}

// DroppedViewers counts slow viewers evicted for falling behind.
func (h *Hub) DroppedViewers() int64 {
	return h.droppedViewers.Load()
}

// TickInterval reports the fan-out cadence (tests assert the 250–1000ms
// production default sits in the contract window).
func (h *Hub) TickInterval() time.Duration {
	return h.tickInterval
}

// KeepaliveInterval reports the SSE comment cadence (tests assert the
// 15–30s production default sits in the contract window).
func (h *Hub) KeepaliveInterval() time.Duration {
	return h.keepaliveInterval
}

// broadcastTick fans out the latest tick when it moved since the last
// broadcast. Slow viewers are evicted and counted; the tick is marshalled
// once and shared across viewers.
func (h *Hub) broadcastTick() {
	h.mu.RLock()
	if !h.hasSnapshot || h.closed {
		h.mu.RUnlock()
		return
	}
	if h.broadcastDone && h.latest.Seq == h.lastBroadcastSeq {
		h.mu.RUnlock()
		return
	}
	snap := h.latest
	viewers := make([]*viewer, 0, len(h.viewers))
	for v := range h.viewers {
		viewers = append(viewers, v)
	}
	h.mu.RUnlock()

	payload, err := json.Marshal(snap)
	if err != nil {
		// Snapshot is JSON-safe by construction (model tags only); a
		// failure drops this tick rather than stalling the loop.
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.lastBroadcastSeq = snap.Seq
	h.broadcastDone = true
	for _, v := range viewers {
		if _, ok := h.viewers[v]; !ok {
			continue
		}
		select {
		case v.ch <- frame{seq: snap.Seq, payload: payload}:
		default:
			delete(h.viewers, v)
			close(v.ch)
			h.droppedFrames.Add(1)
			h.droppedViewers.Add(1)
		}
	}
}

// remove unregisters one viewer. Idempotent: evicted or unknown viewers
// are ignored.
func (h *Hub) remove(v *viewer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.viewers[v]; ok {
		delete(h.viewers, v)
		close(v.ch)
	}
}

// closeAll evicts every viewer and seals the hub. Idempotent.
func (h *Hub) closeAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	for v := range h.viewers {
		delete(h.viewers, v)
		close(v.ch)
	}
}
