package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// Ingest request caps (security review, HIGH): the handler previously decoded an unbounded
// array straight into one transaction holding FOR UPDATE row locks, with no limit on body size
// or packet count. A single gateway batch is at most a few hundred tags; these are generous but
// finite.
const (
	maxIngestBodyBytes         = 2 << 20 // 2 MiB
	maxIngestPacketsPerRequest = 2000
	liveStreamHeartbeat        = 25 * time.Second
	// liveStreamRecomputeInterval bounds live-stream recomputes to one per (tenant, query) per
	// API instance per interval, however often ingest NOTIFYs.
	liveStreamRecomputeInterval = 5 * time.Second
	// herdSignalsReadTimeout bounds every herd-signals read. The deadline rides the request
	// context into pgx, which on expiry sends a Postgres cancel request, so the statement is
	// stopped server-side instead of running on after the client gave up.
	herdSignalsReadTimeout = 5 * time.Second
)

// readContext derives the bounded context for a herd-signals read route.
func readContext(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), herdSignalsReadTimeout)
}

func tenantID(r *http.Request) string {
	return httpmiddleware.TenantIDFromContext(r.Context())
}

func actorID(r *http.Request) string {
	return httpmiddleware.ActorIDFromContext(r.Context())
}

// AppService defines the interface the handler expects from the app service.
type AppService interface {
	IngestPackets(ctx context.Context, actor domain.Actor, req domain.IngestRequest) (domain.IngestResponse, error)
	ListLive(ctx context.Context, actor domain.Actor, parkID, shedID, movementState, liveState, mappingState, pattern, riskState, q *string, cursor string, limit int, sort domain.LiveSort) (domain.LiveResponse, error)
	GetTimeline(ctx context.Context, actor domain.Actor, tagID, from, to string, bucketSeconds int) (domain.TimelineResponse, error)
	ListGateways(ctx context.Context, actor domain.Actor) (domain.GatewaysResponse, error)
	GetInsights(ctx context.Context, actor domain.Actor) (domain.InsightsResponse, error)
	// The mapping WRITES (see mapping_http.go): the module's first write surface beyond ingest.
	BindTagMapping(ctx context.Context, actor domain.Actor, req domain.BindTagMappingRequest) (domain.TagMappingResponse, error)
	UnmapTagMapping(ctx context.Context, actor domain.Actor, req domain.UnmapTagMappingRequest) (domain.TagMappingResponse, error)
	ReplaceTagMapping(ctx context.Context, actor domain.Actor, req domain.ReplaceTagMappingRequest) (domain.TagMappingResponse, error)
	RecordGatewayHeartbeat(ctx context.Context, actor domain.Actor, req domain.GatewayHeartbeatRequest) (domain.GatewayHeartbeatResponse, error)
	ExportCSV(ctx context.Context, actor domain.Actor, parkID, shedID, movementState, liveState, mappingState, pattern, riskState, q *string, sort domain.LiveSort, w io.Writer) error
	GetTagActivity(ctx context.Context, actor domain.Actor, tagID, from, to string) (domain.ActivityResponse, error)
}

// Handler handles HTTP requests for herd signals.
type Handler struct {
	service AppService
	log     *slog.Logger
	liveHub *liveStreamHub
}

type LiveNotificationSource interface {
	Start(ctx context.Context, publish func(tenantID string), publishAll func())
}

// liveCacheInvalidator is implemented by the app service's per-instance live cohort cache.
type liveCacheInvalidator interface {
	InvalidateLive(tenantID string)
	InvalidateAllLive()
}

// NewHandler creates a new herd signals HTTP handler.
func NewHandler(service AppService, log ...*slog.Logger) *Handler {
	l := slog.Default()
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	}
	return &Handler{service: service, log: l, liveHub: newLiveStreamHub(liveStreamRecomputeInterval, l)}
}

func (h *Handler) WithLiveNotifications(ctx context.Context, source LiveNotificationSource) *Handler {
	if source == nil {
		return h
	}
	publish, publishAll := h.liveHub.publish, h.liveHub.publishAll
	if inv, ok := h.service.(liveCacheInvalidator); ok {
		publish = func(tenantID string) {
			inv.InvalidateLive(tenantID)
			h.liveHub.publish(tenantID)
		}
		publishAll = func() {
			inv.InvalidateAllLive()
			h.liveHub.publishAll()
		}
	}
	source.Start(ctx, publish, publishAll)
	return h
}

// liveStreamHub coalesces live-stream recomputes per (tenant, live query) key.
//
// The MQTT bridge ingests a batch about every 2s and every batch NOTIFYs. The hub used to fan
// each NOTIFY out to every open stream and each stream re-ran the full ListLive read, so N viewers
// meant N full reads every ~2s. Now all viewers with the same tenant + query share one feed; a
// feed recomputes at most once per interval (leading edge: the first NOTIFY after a quiet period
// computes immediately; trailing edge: NOTIFYs inside the window collapse into one recompute when
// it closes), never runs two computes at once (single-flight), and broadcasts the one encoded
// frame to every viewer on that feed.
type liveStreamHub struct {
	mu       sync.Mutex
	interval time.Duration
	log      *slog.Logger
	feeds    map[string]*liveFeed
	byTenant map[string]map[*liveFeed]struct{}
}

type liveFeed struct {
	key       string
	tenantID  string
	compute   func(ctx context.Context) (frame []byte, ok bool)
	subs      map[chan []byte]struct{}
	lastRun   time.Time
	lastFrame []byte
	timer     *time.Timer
	running   bool
	pending   bool
	closed    bool
}

func newLiveStreamHub(interval time.Duration, log *slog.Logger) *liveStreamHub {
	if log == nil {
		log = slog.Default()
	}
	return &liveStreamHub{
		interval: interval,
		log:      log,
		feeds:    make(map[string]*liveFeed),
		byTenant: make(map[string]map[*liveFeed]struct{}),
	}
}

// subscribe joins (or creates) the feed for key. compute is only used when the feed is created;
// every later viewer of the same key shares it. The returned channel carries complete SSE frames.
func (h *liveStreamHub) subscribe(tenantID, key string, compute func(ctx context.Context) (frame []byte, ok bool)) (<-chan []byte, func()) {
	ch := make(chan []byte, 1)
	h.mu.Lock()
	f := h.feeds[key]
	if f == nil {
		f = &liveFeed{key: key, tenantID: tenantID, compute: compute, subs: make(map[chan []byte]struct{})}
		h.feeds[key] = f
		if h.byTenant[tenantID] == nil {
			h.byTenant[tenantID] = make(map[*liveFeed]struct{})
		}
		h.byTenant[tenantID][f] = struct{}{}
	}
	f.subs[ch] = struct{}{}
	h.mu.Unlock()

	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(f.subs, ch)
		if len(f.subs) > 0 {
			return
		}
		f.closed = true
		if f.timer != nil {
			f.timer.Stop()
			f.timer = nil
		}
		if h.feeds[key] == f {
			delete(h.feeds, key)
		}
		delete(h.byTenant[tenantID], f)
		if len(h.byTenant[tenantID]) == 0 {
			delete(h.byTenant, tenantID)
		}
	}
}

// recentFrame returns the feed's last broadcast frame when it is younger than the coalescing
// interval, so a viewer joining a busy feed does not add its own full read.
func (h *liveStreamHub) recentFrame(key string) []byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	f := h.feeds[key]
	if f == nil || f.lastFrame == nil || time.Since(f.lastRun) >= h.interval {
		return nil
	}
	return f.lastFrame
}

func (h *liveStreamHub) publish(tenantID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for f := range h.byTenant[tenantID] {
		h.triggerLocked(f)
	}
}

func (h *liveStreamHub) publishAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, f := range h.feeds {
		h.triggerLocked(f)
	}
}

func (h *liveStreamHub) triggerLocked(f *liveFeed) {
	if f.closed {
		return
	}
	if f.running {
		f.pending = true
		return
	}
	if f.timer != nil {
		return // trailing edge already scheduled; it will see this change
	}
	wait := h.interval - time.Since(f.lastRun)
	if f.lastRun.IsZero() || wait <= 0 {
		h.startLocked(f)
		return
	}
	f.timer = time.AfterFunc(wait, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		f.timer = nil
		if f.closed {
			return
		}
		if f.running {
			f.pending = true
			return
		}
		h.startLocked(f)
	})
}

func (h *liveStreamHub) startLocked(f *liveFeed) {
	f.running = true
	f.lastRun = time.Now()
	go h.runFeed(f)
}

func (h *liveStreamHub) runFeed(f *liveFeed) {
	// Detached from any one viewer's request: the frame is shared by every viewer on the feed.
	ctx, cancel := context.WithTimeout(domain.WithFreshLiveRead(context.Background()), herdSignalsReadTimeout)
	frame, ok := f.compute(ctx)
	cancel()

	h.mu.Lock()
	defer h.mu.Unlock()
	f.running = false
	if frame != nil && !f.closed {
		// Only a successful snapshot is reusable for joining viewers; an error frame is
		// delivered to current viewers but never replayed.
		if ok {
			f.lastFrame = frame
		}
		for ch := range f.subs {
			deliverLatest(ch, frame)
		}
	}
	if f.pending {
		f.pending = false
		h.triggerLocked(f)
	}
}

// deliverLatest replaces an undelivered older frame: a slow viewer only ever needs the newest.
func deliverLatest(ch chan []byte, frame []byte) {
	select {
	case ch <- frame:
		return
	default:
	}
	select {
	case <-ch:
	default:
	}
	select {
	case ch <- frame:
	default:
	}
}

// Register registers herd signals routes.
func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("POST /herd-signals/packets", h.IngestPackets)
	mux.HandleFunc("GET /herd-signals/live", h.ListLive)
	mux.HandleFunc("GET /herd-signals/live/stream", h.StreamLive)
	mux.HandleFunc("GET /herd-signals/tags/{tag_id}/timeline", h.GetTimeline)
	mux.HandleFunc("GET /herd-signals/gateways", h.ListGateways)
	mux.HandleFunc("GET /herd-signals/insights", h.GetInsights)
	// Mapping writes. Permission-gated separately from the reads above (herd_signals.map).
	mux.HandleFunc("POST /herd-signals/tag-mappings", h.BindTagMapping)
	mux.HandleFunc("POST /herd-signals/tag-mappings/replace", h.ReplaceTagMapping)
	mux.HandleFunc("POST /herd-signals/tag-mappings/unmap", h.UnmapTagMapping)
	// Gateway heartbeat ingest: a device write, gated with the ingest permission.
	mux.HandleFunc("POST /herd-signals/heartbeats", h.RecordGatewayHeartbeat)
	mux.HandleFunc("GET /herd-signals/export.csv", h.ExportCSV)
	mux.HandleFunc("GET /herd-signals/tags/{tag_id}/activity", h.GetTagActivity)
}

// IngestPackets handles POST /herd-signals/packets.
func (h *Handler) IngestPackets(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract actor from context (set by middleware)
	actor := domain.Actor{
		TenantID: tenantID(r),
		UserID:   actorID(r),
	}
	if actor.TenantID == "" || actor.UserID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized,
			map[string]interface{}{"code": "unauthorized", "message": "authentication required"},
			nil)
		return
	}

	// Bound the request body BEFORE decoding (security review, HIGH): the handler previously
	// decoded an unbounded array straight into one transaction holding FOR UPDATE row locks, so
	// an oversized or absurdly long-array payload could hold locks and memory indefinitely.
	// MaxBytesReader caps total bytes read; the packet-count cap below catches a payload that
	// stays under the byte cap by using short/repeated field values but still carries an
	// unreasonable number of packets.
	r.Body = http.MaxBytesReader(w, r.Body, maxIngestBodyBytes)

	// Strict JSON decode
	var req domain.IngestRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			httpresponse.WriteError(w, r, h.log, http.StatusBadRequest,
				map[string]interface{}{"code": "request_too_large", "message": fmt.Sprintf("request body exceeds %d bytes", maxIngestBodyBytes)},
				err)
			return
		}
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest,
			map[string]interface{}{"code": "invalid_request", "message": "invalid request body"},
			err)
		return
	}

	if len(req.Packets) > maxIngestPacketsPerRequest {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest,
			map[string]interface{}{"code": "too_many_packets", "message": fmt.Sprintf("request carries %d packets, max %d per request", len(req.Packets), maxIngestPacketsPerRequest)},
			nil)
		return
	}

	// Call service
	resp, err := h.service.IngestPackets(ctx, actor, req)
	if err != nil {
		h.log.Error("ingest_packets_failed", "error", err.Error())
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError,
			map[string]interface{}{"code": "ingest_failed", "message": "failed to ingest packets"},
			err)
		return
	}

	httpresponse.WriteJSON(w, http.StatusOK, resp)
}

// ListLive handles GET /herd-signals/live.
func (h *Handler) ListLive(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := readContext(r)
	defer cancel()

	// Extract actor from context
	actor := domain.Actor{
		TenantID: tenantID(r),
		UserID:   actorID(r),
	}
	if actor.TenantID == "" || actor.UserID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized,
			map[string]interface{}{"code": "unauthorized", "message": "authentication required"},
			nil)
		return
	}

	query := parseLiveQuery(r)

	// Call service
	resp, err := h.service.ListLive(ctx, actor, query.parkID, query.shedID, query.movementState, query.liveState, query.mappingState, query.pattern, query.riskState, query.q, query.cursor, query.limit, query.sort)
	if err != nil {
		h.log.Error("list_live_failed", "error", err.Error())
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError,
			map[string]interface{}{"code": "list_failed", "message": "failed to list live tags"},
			err)
		return
	}

	httpresponse.WriteJSON(w, http.StatusOK, resp)
}

type liveQuery struct {
	parkID        *string
	shedID        *string
	movementState *string
	liveState     *string
	mappingState  *string
	pattern       *string
	riskState     *string
	q             *string
	cursor        string
	limit         int
	sort          domain.LiveSort
}

func parseLiveQuery(r *http.Request) liveQuery {
	parkID := r.URL.Query().Get("park_id")
	shedID := r.URL.Query().Get("shed_id")
	movementState := r.URL.Query().Get("movement_state")
	liveState := r.URL.Query().Get("live_state")
	mappingState := r.URL.Query().Get("mapping_state")
	pattern := r.URL.Query().Get("pattern")
	riskState := r.URL.Query().Get("risk_state")
	q := r.URL.Query().Get("q")

	cursor := r.URL.Query().Get("cursor")
	sort := domain.LiveSort{Key: r.URL.Query().Get("sort"), Dir: r.URL.Query().Get("dir")}
	limit := 25 // default per contract; max 200
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 200 {
			limit = l
		}
	}

	// Convert empty strings to nil pointers
	var parkIDPtr, shedIDPtr, movementStatePtr, liveStatePtr, mappingStatePtr, patternPtr, riskStatePtr, qPtr *string
	if parkID != "" {
		parkIDPtr = &parkID
	}
	if shedID != "" {
		shedIDPtr = &shedID
	}
	if movementState != "" {
		movementStatePtr = &movementState
	}
	if liveState == "moving_now" || liveState == "active_1m" {
		liveStatePtr = &liveState
	}
	if mappingState != "" {
		mappingStatePtr = &mappingState
	}
	if pattern != "" {
		patternPtr = &pattern
	}
	if riskState != "" {
		riskStatePtr = &riskState
	}
	if q != "" {
		qPtr = &q
	}
	return liveQuery{
		parkID:        parkIDPtr,
		shedID:        shedIDPtr,
		movementState: movementStatePtr,
		liveState:     liveStatePtr,
		mappingState:  mappingStatePtr,
		pattern:       patternPtr,
		riskState:     riskStatePtr,
		q:             qPtr,
		cursor:        cursor,
		limit:         limit,
		sort:          sort,
	}
}

// StreamLive handles GET /herd-signals/live/stream.
func (h *Handler) StreamLive(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	controller := http.NewResponseController(w)

	actor := domain.Actor{
		TenantID: tenantID(r),
		UserID:   actorID(r),
	}
	if actor.TenantID == "" || actor.UserID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized,
			map[string]interface{}{"code": "unauthorized", "message": "authentication required"},
			nil)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	query := parseLiveQuery(r)
	write := func(event string, payload interface{}) bool {
		return writeFrame(w, controller, encodeEvent(h.log, event, payload))
	}
	listSnapshot := func(ctx context.Context) (domain.LiveResponse, error) {
		return h.service.ListLive(ctx, actor, query.parkID, query.shedID, query.movementState, query.liveState, query.mappingState, query.pattern, query.riskState, query.q, query.cursor, query.limit, query.sort)
	}
	writeSnapshot := func() bool {
		// Fresh read: a joining viewer must not be handed a stale-while-revalidate snapshot,
		// because the refreshed result would never be pushed to it.
		readCtx, cancel := context.WithTimeout(domain.WithFreshLiveRead(ctx), herdSignalsReadTimeout)
		defer cancel()
		resp, err := listSnapshot(readCtx)
		if err != nil {
			h.log.Warn("herd_signals_stream_snapshot_failed", "error", err.Error())
			return write("snapshot_error", map[string]interface{}{"code": "snapshot_failed", "message": "failed to list live tags"})
		}
		return write("snapshot", resp)
	}
	// The shared feed's compute: one read per coalescing window for every viewer of this key.
	computeFrame := func(ctx context.Context) ([]byte, bool) {
		resp, err := listSnapshot(ctx)
		if err != nil {
			h.log.Warn("herd_signals_stream_snapshot_failed", "error", err.Error())
			return encodeEvent(h.log, "snapshot_error", map[string]interface{}{"code": "snapshot_failed", "message": "failed to list live tags"}), false
		}
		return encodeEvent(h.log, "snapshot", resp), true
	}

	key := actor.TenantID + "\x00" + r.URL.Query().Encode()
	updates, unsubscribe := h.liveHub.subscribe(actor.TenantID, key, computeFrame)
	defer unsubscribe()
	if frame := h.liveHub.recentFrame(key); frame != nil {
		if !writeFrame(w, controller, frame) {
			return
		}
	} else if !writeSnapshot() {
		return
	}
	heartbeat := time.NewTicker(liveStreamHeartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			if !write("tick", map[string]interface{}{"at": time.Now().UTC().Format(time.RFC3339Nano)}) {
				return
			}
		case frame := <-updates:
			if !writeFrame(w, controller, frame) {
				return
			}
		}
	}
}

func encodeEvent(log *slog.Logger, event string, payload interface{}) []byte {
	body, err := json.Marshal(payload)
	if err != nil {
		log.Warn("herd_signals_stream_marshal_failed", "error", err)
		return nil
	}
	return []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", event, body))
}

func writeFrame(w io.Writer, controller *http.ResponseController, frame []byte) bool {
	if frame == nil {
		return true
	}
	if _, err := w.Write(frame); err != nil {
		return false
	}
	return controller.Flush() == nil
}

// GetTimeline handles GET /herd-signals/tags/{tag_id}/timeline.
func (h *Handler) GetTimeline(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := readContext(r)
	defer cancel()

	// Extract actor from context
	actor := domain.Actor{
		TenantID: tenantID(r),
		UserID:   actorID(r),
	}
	if actor.TenantID == "" || actor.UserID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized,
			map[string]interface{}{"code": "unauthorized", "message": "authentication required"},
			nil)
		return
	}

	// Extract tag_id from path
	tagID := r.PathValue("tag_id")
	if tagID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest,
			map[string]interface{}{"code": "missing_tag_id", "message": "tag_id is required"},
			nil)
		return
	}

	// Parse query parameters
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	if from == "" || to == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest,
			map[string]interface{}{"code": "missing_from_to", "message": "from and to timestamps required"},
			nil)
		return
	}

	bucketSeconds := 0 // 0 = let the service select the tier from the requested range
	if bucketStr := r.URL.Query().Get("bucket_seconds"); bucketStr != "" {
		if b, err := strconv.Atoi(bucketStr); err == nil && b > 0 {
			bucketSeconds = b
		}
	}

	// Call service
	resp, err := h.service.GetTimeline(ctx, actor, tagID, from, to, bucketSeconds)
	if err != nil {
		// Defect 7 fix: a caller-input validation failure (bad range, unsupported
		// bucket_seconds, too many buckets) previously mapped to 500 like a real backend
		// failure. domain.ErrValidation distinguishes the two.
		if errors.Is(err, domain.ErrValidation) {
			h.log.Warn("get_timeline_invalid_request", "tag_id", tagID, "error", err.Error())
			httpresponse.WriteError(w, r, h.log, http.StatusBadRequest,
				map[string]interface{}{"code": "invalid_request", "message": err.Error()},
				err)
			return
		}
		h.log.Error("get_timeline_failed", "tag_id", tagID, "error", err.Error())
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError,
			map[string]interface{}{"code": "timeline_failed", "message": "failed to get timeline"},
			err)
		return
	}

	httpresponse.WriteJSON(w, http.StatusOK, resp)
}

// ListGateways handles GET /herd-signals/gateways.
func (h *Handler) ListGateways(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := readContext(r)
	defer cancel()

	// Extract actor from context
	actor := domain.Actor{
		TenantID: tenantID(r),
		UserID:   actorID(r),
	}
	if actor.TenantID == "" || actor.UserID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized,
			map[string]interface{}{"code": "unauthorized", "message": "authentication required"},
			nil)
		return
	}

	// Call service
	resp, err := h.service.ListGateways(ctx, actor)
	if err != nil {
		h.log.Error("list_gateways_failed", "error", err.Error())
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError,
			map[string]interface{}{"code": "gateways_failed", "message": "failed to list gateways"},
			err)
		return
	}

	httpresponse.WriteJSON(w, http.StatusOK, resp)
}

// GetInsights handles GET /herd-signals/insights.
func (h *Handler) GetInsights(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := readContext(r)
	defer cancel()

	actor := domain.Actor{
		TenantID: tenantID(r),
		UserID:   actorID(r),
	}
	if actor.TenantID == "" || actor.UserID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized,
			map[string]interface{}{"code": "unauthorized", "message": "authentication required"},
			nil)
		return
	}

	resp, err := h.service.GetInsights(ctx, actor)
	if err != nil {
		h.log.Error("get_insights_failed", "error", err.Error())
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError,
			map[string]interface{}{"code": "insights_failed", "message": "failed to compute insights"},
			err)
		return
	}

	httpresponse.WriteJSON(w, http.StatusOK, resp)
}
