package core

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mahsang/internal/model"
)

// TestSampleDelayBodyGate covers the byte-limit / throttle check: a probe only
// passes if the validated response both contains the marker AND sustains a full
// payload past Iran's ~1–4KB cutoff. A connection that delivers the marker then
// stops short (a throttled/blocked config) must fail, not report OK on TTFB.
func TestSampleDelayBodyGate(t *testing.T) {
	full := strings.Repeat("x", probeBodyBytes*2) // more than the gate
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, "<html>"+measureMarker+full)
	})
	mux.HandleFunc("/short", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, measureMarker+" but cut off early") // marker, tiny body
	})
	mux.HandleFunc("/nomarker", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, full) // big enough, but injected page (no marker)
	})
	mux.HandleFunc("/bad", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	orig := measureURL
	defer func() { measureURL = orig }()

	cases := []struct {
		path   string
		want   model.Verdict
		wantOK bool // expects a non-negative latency
	}{
		{"/ok", model.VerdictOK, true},
		{"/short", model.VerdictTimeout, false},      // throttled: marker but short body
		{"/nomarker", model.VerdictBlockPage, false}, // injected page
		{"/bad", model.VerdictBlockPage, false},      // non-200
	}
	for _, c := range cases {
		measureURL = srv.URL + c.path
		ms, v, detail := sampleDelay(context.Background(), srv.Client(), true)
		if v != c.want {
			t.Errorf("%s: verdict = %v (%s), want %v", c.path, v, detail, c.want)
		}
		if c.wantOK && ms < 0 {
			t.Errorf("%s: latency = %d, want >= 0", c.path, ms)
		}
		if !c.wantOK && ms != -1 {
			t.Errorf("%s: latency = %d, want -1 on failure", c.path, ms)
		}
	}
}

// TestClassifyTransportErr pins the teardown-vs-drop distinction, including the
// "closed pipe" form that xray's pipe-backed dialer reports for a refused or
// reset upstream (a silent drop instead yields a deadline/timeout).
func TestClassifyTransportErr(t *testing.T) {
	cases := []struct {
		err  error
		want model.Verdict
	}{
		{errors.New(`Get "https://x/": io: read/write on closed pipe`), model.VerdictReset},
		{io.ErrClosedPipe, model.VerdictReset},
		{errors.New("unexpected EOF"), model.VerdictReset},
		{errors.New("read tcp: connection reset by peer"), model.VerdictReset},
		{context.DeadlineExceeded, model.VerdictTimeout},
		{errors.New("context deadline exceeded (Client.Timeout exceeded while awaiting headers)"), model.VerdictTimeout},
	}
	for _, c := range cases {
		if got := classifyTransportErr(c.err); got != c.want {
			t.Errorf("classifyTransportErr(%q) = %v, want %v", c.err, got, c.want)
		}
	}
}

func TestMedianMs(t *testing.T) {
	cases := []struct {
		in   []int64
		want int64
	}{
		{[]int64{100}, 100},
		{[]int64{300, 100, 200}, 200},   // unsorted, odd count
		{[]int64{40, 10, 30, 20}, 30},   // even count -> upper-middle
		{[]int64{5, 5, 5}, 5},           // all equal
		{[]int64{1000, 50, 60, 55}, 60}, // one outlier shouldn't win
	}
	for _, c := range cases {
		if got := medianMs(append([]int64(nil), c.in...)); got != c.want {
			t.Errorf("medianMs(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}
