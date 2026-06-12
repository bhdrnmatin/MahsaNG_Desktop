package core

import "testing"

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
