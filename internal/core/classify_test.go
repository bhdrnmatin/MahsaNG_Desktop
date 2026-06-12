package core

import (
	"context"
	"errors"
	"io"
	"testing"

	"mahsang/internal/model"
)

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
