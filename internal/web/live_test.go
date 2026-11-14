package web

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

// stream is an open live stream of the shopping list.
type stream struct {
	t      *testing.T
	events chan [2]string // event name, data
}

// openStream opens the live stream of c, as of the change since.
func (c *client) openStream(since string) *stream {
	c.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	c.t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.srv.URL+"/shopping/events?since="+url.QueryEscape(since), nil)
	req.Header.Set("Accept", "text/event-stream")
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "text/event-stream" {
		c.t.Fatalf("stream: status %d, %q", res.StatusCode, res.Header.Get("Content-Type"))
	}
	s := &stream{t: c.t, events: make(chan [2]string, 16)}
	go func() {
		defer close(s.events)
		defer res.Body.Close()
		r := bufio.NewReader(res.Body)
		var name, data string
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\n")
			switch {
			case strings.HasPrefix(line, "event: "):
				name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				data = strings.TrimPrefix(line, "data: ")
			case line == "" && name != "":
				s.events <- [2]string{name, data}
				name, data = "", ""
			}
		}
	}()
	return s
}

// next returns the next event; ok is false when the stream ended.
func (s *stream) next() (name, data string, ok bool) {
	s.t.Helper()
	select {
	case e, open := <-s.events:
		return e[0], e[1], open
	case <-time.After(5 * time.Second):
		s.t.Fatal("no event")
		return "", "", false
	}
}

// quiet checks that no event comes for a while.
func (s *stream) quiet() {
	s.t.Helper()
	select {
	case e := <-s.events:
		s.t.Fatalf("unexpected event %v", e)
	case <-time.After(100 * time.Millisecond):
	}
}

// postAs posts a form with the id of the page that sends it.
func (c *client) postAs(page, path string, form url.Values) *http.Response {
	c.t.Helper()
	form.Set("csrf_token", c.token)
	req, _ := http.NewRequest(http.MethodPost, c.srv.URL+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Live-Client", page)
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, res.Body)
	res.Body.Close()
	return res
}

var liveSinceRe = regexp.MustCompile(`data-live-since="([^"]+)"`)

func TestLiveShoppingList(t *testing.T) {
	c := newClient(t)
	other := anonymous(t, c.app, c.srv)
	other.logIn(testLogin, testPassword)

	c.post("/shopping", url.Values{"from": {"2026-10-12"}, "to": {"2026-10-18"}})
	c.post("/shopping/extras", url.Values{"name": {"Pain"}})
	articles, _ := c.store.Articles(context.Background())
	line := "/shopping/articles/" + itoa(articles[0].ID)

	// The page tells the change it shows; the stream opened as of it has
	// nothing to catch up.
	body, _ := other.get("/shopping")
	m := liveSinceRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("EF-33: no live id on the list")
	}
	s := other.openStream(m[1])
	s.quiet()

	// A tick on one device reaches the other, with the page that made it.
	if res := c.postAs("abcdefgh", line, url.Values{"action": {"tick"}, "needs": {"extra"}}); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("tick: status %d", res.StatusCode)
	}
	if name, data, _ := s.next(); name != "tick" || data != `{"line":"`+line+`","ticked":true,"client":"abcdefgh"}` {
		t.Errorf("EF-33: tick event %s %s", name, data)
	}
	c.post(line, url.Values{"action": {"untick"}})
	if name, data, _ := s.next(); name != "tick" || data != `{"line":"`+line+`"}` {
		t.Errorf("EF-33: untick event %s %s", name, data)
	}

	// Any other change of the family asks for a reload; a refused one does
	// not.
	c.post("/shopping/extras", url.Values{"name": {"pain"}})
	s.quiet()
	c.post("/shopping/extras", url.Values{"name": {"Beurre"}})
	if name, _, _ := s.next(); name != "changed" {
		t.Errorf("EF-33: extra added, event %s", name)
	}

	// A stream opened as of an older change catches up at once.
	if name, _, _ := other.openStream(m[1]).next(); name != "changed" {
		t.Errorf("EF-33: missed changes, event %s", name)
	}

	// Another family hears nothing.
	createFamily(t, c.app, "voisins", testPassword)
	neighbour := anonymous(t, c.app, c.srv)
	neighbour.logIn("voisins", testPassword)
	ns := neighbour.openStream("")
	if name, _, _ := ns.next(); name != "changed" {
		t.Fatalf("a stream without id catches up: %s", name)
	}
	c.post(line, url.Values{"action": {"tick"}, "needs": {"extra"}})
	if name, _, _ := s.next(); name != "tick" {
		t.Fatalf("event %s", name)
	}
	ns.quiet()

	// Logging out every device ends the streams.
	c.post("/account/logout-all", url.Values{})
	if _, _, ok := s.next(); ok {
		t.Error("the stream outlived the sessions")
	}
	if _, res := other.get("/shopping/events"); res.StatusCode != http.StatusSeeOther {
		t.Errorf("stream without session: status %d", res.StatusCode)
	}
}

func TestHubSlowStream(t *testing.T) {
	h := newHub()
	ch, missed, ok := h.subscribe("f", h.lastID("f"))
	if !ok || missed {
		t.Fatalf("subscribe: ok %v, missed %v", ok, missed)
	}
	for range liveBuffer + 1 {
		h.publish("f", liveEvent{})
	}
	n := 0
	for range ch {
		n++
	}
	if n != liveBuffer {
		t.Errorf("%d events before the stream closed", n)
	}
	h.unsubscribe("f", ch) // already closed: no panic

	h.close()
	if _, _, ok := h.subscribe("f", ""); ok {
		t.Error("a stream opened after close")
	}
}
