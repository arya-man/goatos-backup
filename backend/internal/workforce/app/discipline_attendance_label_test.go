package app

import "testing"

func TestLateByLabelReadsHoursFromAnHourOn(t *testing.T) {
	for in, want := range map[int]string{16: "16 min", 59: "59 min", 60: "1 h 0 min", 497: "8 h 17 min"} {
		if got := lateByLabel(in); got != want {
			t.Errorf("lateByLabel(%d) = %q, want %q", in, got, want)
		}
	}
}
