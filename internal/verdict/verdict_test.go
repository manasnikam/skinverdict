package verdict

import (
	"strings"
	"testing"
	"time"
)

func day(n int) time.Time { return time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC).AddDate(0, 0, n) }

func series(target string, pts ...[2]float64) []Scan { // {dayOffset, score}
	var out []Scan
	for _, p := range pts {
		out = append(out, Scan{At: day(int(p[0])), Scores: map[string]float64{target: p[1], "redness": 90}})
	}
	return out
}

func TestWorkingAfterTwoWeeks(t *testing.T) {
	v, err := Evaluate("pore", series("pore", [2]float64{0, 22}, [2]float64{7, 24}, [2]float64{14, 27}), DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != Working || v.Delta != 5 {
		t.Fatalf("got %+v", v)
	}
	if v.Confidence != "medium" {
		t.Fatalf("confidence %s", v.Confidence)
	}
}

func TestNoiseIsNotYet(t *testing.T) {
	v, _ := Evaluate("pore", series("pore", [2]float64{0, 22}, [2]float64{14, 24.5}), DefaultOptions())
	if v.Status != NotYet {
		t.Fatalf("got %s", v.Status)
	}
}

func TestFlatAtHorizonIsNotWorking(t *testing.T) {
	v, _ := Evaluate("pore", series("pore", [2]float64{0, 22}, [2]float64{14, 23}, [2]float64{28, 22.5}), DefaultOptions())
	if v.Status != NotWorking {
		t.Fatalf("got %s", v.Status)
	}
}

func TestWorseIsNotWorking(t *testing.T) {
	v, _ := Evaluate("acne", series("acne", [2]float64{0, 80}, [2]float64{14, 70}), DefaultOptions())
	if v.Status != NotWorking {
		t.Fatalf("got %s", v.Status)
	}
}

func TestEarlyBigWinCounts(t *testing.T) {
	v, _ := Evaluate("pore", series("pore", [2]float64{0, 22}, [2]float64{7, 31}), DefaultOptions())
	if v.Status != Working {
		t.Fatalf("got %s", v.Status)
	}
}

func TestSideEffectFlagged(t *testing.T) {
	s := series("pore", [2]float64{0, 22}, [2]float64{14, 30})
	s[1].Scores["redness"] = 80 // dropped 10
	v, _ := Evaluate("pore", s, DefaultOptions())
	if v.Status != Working || v.Worsened["redness"] != -10 {
		t.Fatalf("got %+v", v)
	}
}

func TestSingleScanNotYet(t *testing.T) {
	v, _ := Evaluate("pore", series("pore", [2]float64{0, 22}), DefaultOptions())
	if v.Status != NotYet || v.Scans != 1 {
		t.Fatalf("got %+v", v)
	}
}

func TestUnorderedInput(t *testing.T) {
	s := series("pore", [2]float64{14, 30}, [2]float64{0, 22})
	v, _ := Evaluate("pore", s, DefaultOptions())
	if v.Baseline != 22 || v.Latest != 30 {
		t.Fatalf("got %+v", v)
	}
}

// Real scans of one unchanged face (only crop/exposure varied): a single dip
// to 81 must not decide the verdict.
func TestOutlierScanDoesNotDecide(t *testing.T) {
	s := series("pore", [2]float64{0, 91}, [2]float64{7, 93}, [2]float64{14, 92}, [2]float64{21, 93}, [2]float64{28, 81})
	v, _ := Evaluate("pore", s, DefaultOptions())
	if v.Status != NotWorking || v.Delta <= -4 || v.Delta >= 4 {
		t.Fatalf("one bad last scan swung the verdict: %+v", v)
	}
	if want := "trend across all scans confirms"; !strings.Contains(v.Explanation, want) {
		t.Fatalf("explanation %q", v.Explanation)
	}
}
