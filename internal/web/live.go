package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ── Live shopping list (EF-33) ────────────────────────────────────────────
// The devices of a family showing the shopping list follow it as it changes:
// a server-sent event stream per page, fed by the changes the family posts.
// A tick moves the line in place; any other change asks the page to reload
// its list. Every event has an id; a stream opened again (EventSource
// reconnects by itself, the page passes the id it shows) catches up with a
// single "changed" when it missed anything.

const (
	liveHeartbeat   = 25 * time.Second
	liveMaxDuration = 30 * time.Minute // then the browser reconnects, the session checked again
	liveWriteWait   = 10 * time.Second
	liveRetry       = 5 * time.Second
	liveMaxStreams  = 32 // per family
	liveBuffer      = 16
)

// liveClientRe checks the id a page sends with its posts, so that it is not
// told of its own changes.
var liveClientRe = regexp.MustCompile(`^[0-9a-z]{8,32}$`)

// liveEvent is a change of the data of a family.
type liveEvent struct {
	id     string
	Line   string `json:"line,omitempty"`   // URL of the line ticked or unticked
	Ticked bool   `json:"ticked,omitempty"` // with Line
	Client string `json:"client,omitempty"` // the page that made the change
}

func (e liveEvent) name() string {
	if e.Line != "" {
		return "tick"
	}
	return "changed"
}

// hub hands the events of a family to its open streams.
type hub struct {
	boot string // tells the ids of this run from those of the previous ones

	mu      sync.Mutex
	seq     uint64
	closed  bool
	streams map[string]*familyStreams // by family file, kept for the run
}

type familyStreams struct {
	last uint64 // seq of the last event, 0 if none since the start
	subs map[chan liveEvent]struct{}
}

func newHub() *hub {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return &hub{boot: hex.EncodeToString(b), streams: map[string]*familyStreams{}}
}

func (h *hub) idOf(seq uint64) string {
	return h.boot + "-" + strconv.FormatUint(seq, 10)
}

// lastID is the id of the last event of a family: a page shows the data as
// of it.
func (h *hub) lastID(family string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var last uint64
	if fs := h.streams[family]; fs != nil {
		last = fs.last
	}
	return h.idOf(last)
}

// subscribe opens a stream on the events of a family; ok is false when the
// family has too many of them or the server is stopping. missed tells
// whether an event came after the one of id since.
func (h *hub) subscribe(family, since string) (ch chan liveEvent, missed, ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	fs := h.streams[family]
	if fs == nil {
		fs = &familyStreams{subs: map[chan liveEvent]struct{}{}}
		h.streams[family] = fs
	}
	if h.closed || len(fs.subs) >= liveMaxStreams {
		return nil, false, false
	}
	ch = make(chan liveEvent, liveBuffer)
	fs.subs[ch] = struct{}{}
	return ch, since != h.idOf(fs.last), true
}

// unsubscribe closes a stream, unless the hub already did.
func (h *hub) unsubscribe(family string, ch chan liveEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	fs := h.streams[family]
	if fs == nil {
		return
	}
	if _, ok := fs.subs[ch]; ok {
		delete(fs.subs, ch)
		close(ch)
	}
}

// publish hands an event to every stream of a family. A stream that cannot
// keep up is closed: the page reconnects and catches up.
func (h *hub) publish(family string, e liveEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seq++
	// Kept without stream too: a page shown before its stream opens
	// catches up.
	fs := h.streams[family]
	if fs == nil {
		fs = &familyStreams{subs: map[chan liveEvent]struct{}{}}
		h.streams[family] = fs
	}
	fs.last = h.seq
	e.id = h.idOf(h.seq)
	for ch := range fs.subs {
		select {
		case ch <- e:
		default:
			delete(fs.subs, ch)
			close(ch)
		}
	}
}

// drop closes the streams of a family, whose sessions may be gone: each page
// reconnects, with its session checked.
func (h *hub) drop(family string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if fs := h.streams[family]; fs != nil {
		for ch := range fs.subs {
			delete(fs.subs, ch)
			close(ch)
		}
	}
}

// close ends every stream, for the server to stop.
func (h *hub) close() {
	h.mu.Lock()
	families := make([]string, 0, len(h.streams))
	for f := range h.streams {
		families = append(families, f)
	}
	h.closed = true
	h.mu.Unlock()
	for _, f := range families {
		h.drop(f)
	}
}

// CloseStreams ends the live streams: give it to http.Server.RegisterOnShutdown,
// they would otherwise hold the shutdown.
func (a *App) CloseStreams() {
	a.live.close()
}

// liveNote is filled while a request of a family runs: protect publishes it
// once the change succeeded (see redirect).
type liveNote struct {
	done bool
	skip bool // the streams were closed instead
	ev   liveEvent
}

// noteDone marks the change of a post as made.
func noteDone(r *http.Request) {
	if n, ok := r.Context().Value(ctxLive).(*liveNote); ok && r.Method == http.MethodPost {
		n.done = true
	}
}

// noteTick makes the change of a post a tick of a line.
func noteTick(ctx context.Context, line string, ticked bool) {
	if n, ok := ctx.Value(ctxLive).(*liveNote); ok {
		n.ev.Line, n.ev.Ticked = line, ticked
	}
}

// endStreams closes the streams of the family of the request, instead of
// telling them of its change: they reconnect, if their session still holds.
func (a *App) endStreams(ctx context.Context) {
	if n, ok := ctx.Value(ctxLive).(*liveNote); ok {
		n.skip = true
	}
	a.live.drop(familyOf(ctx).File)
}

func (a *App) publishNote(r *http.Request, family string, n *liveNote) {
	if !n.done || n.skip {
		return
	}
	if c := r.Header.Get("X-Live-Client"); liveClientRe.MatchString(c) {
		n.ev.Client = c
	}
	a.live.publish(family, n.ev)
}

// shoppingEvents streams the changes of the family to a page of the
// shopping list.
func (a *App) shoppingEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// The stream does not need the database: it must not hold it open.
	ctx.Value(ctxRelease).(func())()
	family := familyOf(ctx).File

	since := r.Header.Get("Last-Event-ID")
	if since == "" {
		since = r.URL.Query().Get("since")
	}
	ch, missed, ok := a.live.subscribe(family, since)
	if !ok {
		w.Header().Set("Retry-After", "60")
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return
	}
	defer a.live.unsubscribe(family, ch)

	rc := http.NewResponseController(w)
	// The server timeouts are for requests, not for a stream.
	_ = rc.SetReadDeadline(time.Time{})
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no") // nginx would hold the events back
	w.WriteHeader(http.StatusOK)

	var b strings.Builder
	write := func() bool {
		_ = rc.SetWriteDeadline(time.Now().Add(liveWriteWait))
		_, err := fmt.Fprint(w, b.String())
		b.Reset()
		return err == nil && rc.Flush() == nil
	}
	fmt.Fprintf(&b, "retry: %d\n\n", liveRetry.Milliseconds())
	if missed {
		fmt.Fprintf(&b, "id: %s\nevent: changed\ndata: {}\n\n", a.live.lastID(family))
	}
	if !write() {
		return
	}

	heartbeat := time.NewTicker(liveHeartbeat)
	defer heartbeat.Stop()
	end := time.NewTimer(liveMaxDuration)
	defer end.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-end.C:
			return
		case <-heartbeat.C:
			b.WriteString(": ping\n\n")
		case e, open := <-ch:
			if !open {
				return
			}
			data, err := json.Marshal(e)
			if err != nil {
				return
			}
			fmt.Fprintf(&b, "id: %s\nevent: %s\ndata: %s\n\n", e.id, e.name(), data)
		}
		if !write() {
			return
		}
	}
}
