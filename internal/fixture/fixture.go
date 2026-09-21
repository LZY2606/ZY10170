// Package fixture builds the fixed, deterministic data set used by every
// fresh database and by every replay check. Nothing here depends on wall
// clocks, random sources or external files.
package fixture

import "voicebench/internal/model"

const (
	SetV1 = "v1" // original spectrum candidate set
	SetV2 = "v2" // one recomputed candidate set
	// ToleranceHz is the remap window: a recomputed candidate within +/- this
	// of an old frequency may represent the same peak.
	ToleranceHz = 40.0
	BinHz       = 100.0
	BinCount    = 40 // 0..4000 Hz
)

// Spec describes one synthetic utterance and the deliberate defects of its
// original automatic tracking.
type Spec struct {
	Speaker model.Speaker
	Utter   model.Utterance
	Vowels  []model.VowelInterval
	// Missing are present=false raw frames inside the window.
	Missing []int
	// Weak frames carry competing peaks from the neighbouring formant branch.
	Weak []int
	// AutoSwap[frame][band] gives a forced wrong pick index (within that
	// frame/band's candidate list), reproducing track jumping.
	AutoSwap map[int]map[int]int
	// Spur adds extra non-branch candidate peaks (frame -> band -> freqs).
	Spur map[int]map[int][]float64
	// nodes per branch band: (frame, freq) piecewise-linear true trajectory.
	nodes [4][][2]float64
}

// Specs returns the fixed pronunciation versions under test.
func Specs() []Spec {
	s1 := model.Speaker{ID: "s1", Name: "说话人一 / Lin"}
	s2 := model.Speaker{ID: "s2", Name: "说话人二 / Aoi"}

	u1 := model.Utterance{ID: "ai-a", SpeakerID: "s1", PronVer: "pron-v1",
		WindowFrom: 0, WindowTo: 20, FPS: 100, Frames: 21}
	u2 := model.Utterance{ID: "ai-a", SpeakerID: "s2", PronVer: "pron-v2",
		WindowFrom: 0, WindowTo: 20, FPS: 100, Frames: 21}
	u3 := model.Utterance{ID: "ai-b", SpeakerID: "s1", PronVer: "pron-v3-fast",
		WindowFrom: 2, WindowTo: 18, FPS: 100, Frames: 19}

	// u1 is the acceptance case: close crossing in weak energy, missing
	// frames, and a partial identity swap across the gap.
	u1Spec := Spec{Speaker: s1, Utter: u1,
		Missing: []int{13, 14}, Weak: []int{9, 10, 11},
		// Index 0 is the lowest-frequency candidate of that slot. In weak
		// frames the competing branch sits below the true F2 peak; the
		// tracker jumps onto it. After the missing-frame run a spurious
		// F2 and F3 peaks exist in both slots after the missing run; the
		// tracker exchanges their labels across the invisible frames. The
		// frame itself is out of order and the continuity permutation also
		// disagrees with the locked boundary.
		AutoSwap: map[int]map[int]int{
			9:  {1: 0},
			10: {1: 0},
			11: {1: 0},
			15: {1: 1, 2: 0}, // F2/F3 labels exchange across the gap
		},
		Spur: map[int]map[int][]float64{
			15: {1: {2750}, 2: {1900}},
		},
		nodes: [4][][2]float64{
			{{0, 700}, {5, 800}, {10, 860}, {15, 500}, {20, 300}},
			{{0, 1200}, {5, 1100}, {10, 900}, {15, 1900}, {20, 2200}},
			{{0, 2500}, {10, 2600}, {15, 2750}, {20, 2900}},
			{{0, 3400}, {10, 3500}, {15, 3450}, {20, 3400}},
		},
		Vowels: []model.VowelInterval{
			{SpeakerID: "s1", UtterID: "ai-a", StartFrame: 1, EndFrame: 10, Label: "/a/"},
			{SpeakerID: "s1", UtterID: "ai-a", StartFrame: 10, EndFrame: 19, Label: "/i/"},
		},
	}

	// u2 keeps a weak-energy close approach but has no missing frames.
	u2Spec := Spec{Speaker: s2, Utter: u2, Weak: []int{9, 10, 11},
		AutoSwap: map[int]map[int]int{10: {1: 0}},
		nodes: [4][][2]float64{
			{{0, 720}, {10, 800}, {20, 320}},
			{{0, 1180}, {10, 1000}, {20, 2100}},
			{{0, 2550}, {10, 2620}, {20, 2680}},
			{{0, 3420}, {10, 3480}, {20, 3440}},
		},
		Vowels: []model.VowelInterval{
			{SpeakerID: "s2", UtterID: "ai-a", StartFrame: 1, EndFrame: 10, Label: "/a/"},
			{SpeakerID: "s2", UtterID: "ai-a", StartFrame: 10, EndFrame: 19, Label: "/i/"},
		},
	}

	// u3 starts at frame 2, so raw frames 0,1 are outside the window, and
	// frame 18 lands exactly on the right edge and is excluded.
	u3Spec := Spec{Speaker: s1, Utter: u3, Weak: []int{8},
		AutoSwap: map[int]map[int]int{8: {0: 1}},
		nodes: [4][][2]float64{
			{{2, 690}, {8, 790}, {10, 830}, {17, 420}, {18, 400}},
			{{2, 1220}, {8, 1120}, {10, 1000}, {17, 2000}, {18, 2050}},
			{{2, 2480}, {10, 2580}, {18, 2660}},
			{{2, 3380}, {10, 3470}, {18, 3430}},
		},
		Vowels: []model.VowelInterval{
			{SpeakerID: "s1", UtterID: "ai-b", StartFrame: 3, EndFrame: 10, Label: "/a/"},
			{SpeakerID: "s1", UtterID: "ai-b", StartFrame: 10, EndFrame: 17, Label: "/i/"},
		},
	}
	return []Spec{u1Spec, u2Spec, u3Spec}
}

// interp evaluates the piecewise-linear node list at frame n.
func interp(nodes [][2]float64, n int) float64 {
	x := float64(n)
	if x <= nodes[0][0] {
		return nodes[0][1]
	}
	for i := 1; i < len(nodes); i++ {
		if x <= nodes[i][0] {
			x0, y0 := nodes[i-1][0], nodes[i-1][1]
			x1, y1 := nodes[i][0], nodes[i][1]
			return y0 + (y1-y0)*(x-x0)/(x1-x0)
		}
	}
	return nodes[len(nodes)-1][1]
}

func isMissing(spec Spec, frame int) bool {
	for _, m := range spec.Missing {
		if m == frame {
			return true
		}
	}
	return false
}

func isWeak(spec Spec, frame int) bool {
	for _, m := range spec.Weak {
		if m == frame {
			return true
		}
	}
	return false
}

// branchFreq returns the true peak frequencies of the four branches at frame.
func branchFreq(spec Spec, frame int) [4]float64 {
	var out [4]float64
	for band := 0; band < model.Bands; band++ {
		out[band] = interp(spec.nodes[band], frame)
	}
	return out
}
