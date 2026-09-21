package fixture

import (
	"fmt"
	"sort"

	"voicebench/internal/model"
)

// Data holds every generated raw entity before persistence.
type Data struct {
	Speakers   []model.Speaker
	Utterances []model.Utterance
	Frames     []model.FrameMeta
	Candidates []model.Candidate
	Auto       []model.AutoPoint
	Vowels     []model.VowelInterval
	Spectrum   []model.SpectrumCell
}

type idGen struct{ c, a int64 }

// Build generates the fixed fixture for both candidate sets v1 and v2.
func Build() *Data {
	specs := Specs()
	d := &Data{}
	ig := &idGen{}
	seenSp := map[string]bool{}
	for _, spec := range specs {
		if !seenSp[spec.Speaker.ID] {
			d.Speakers = append(d.Speakers, spec.Speaker)
			seenSp[spec.Speaker.ID] = true
		}
		d.Utterances = append(d.Utterances, spec.Utter)
		for _, v := range spec.Vowels {
			vid := int64(len(d.Vowels) + 1)
			v.ID = vid
			d.Vowels = append(d.Vowels, v)
		}
		buildFrames(&d.Frames, spec)
		buildSet(d, ig, spec, SetV1)
		buildSet(d, ig, spec, SetV2)
		buildSpectrum(&d.Spectrum, spec)
	}
	return d
}

func inWindow(spec Spec, frame int) bool {
	return frame >= spec.Utter.WindowFrom && frame < spec.Utter.WindowTo
}

func buildFrames(out *[]model.FrameMeta, spec Spec) {
	for n := 0; n < spec.Utter.Frames; n++ {
		*out = append(*out, model.FrameMeta{
			SpeakerID: spec.Speaker.ID, UtterID: spec.Utter.ID,
			Frame:    n,
			T:        float64(n) / float64(spec.Utter.FPS),
			Present:  !isMissing(spec, n),
			InWindow: inWindow(spec, n),
		})
	}
}

// slot describes candidate peaks competing for one band at one frame.
type slot struct{ freqs []float64 }

func v1Slots(spec Spec, frame int) []slot {
	truth := branchFreq(spec, frame)
	slots := make([]slot, model.Bands)
	for b := 0; b < model.Bands; b++ {
		slots[b].freqs = []float64{round1(truth[b])}
	}
	if isWeak(spec, frame) {
		// In the low-energy region the analyser proposes a competing peak
		// INSIDE each band slot: for F1 it sees the F2 branch frequency,
		// and vice versa. The true branch peaks themselves remain strictly
		// F1<F2<F3<F4; it is the tracker's pick that jumps labels.
		slots[0].freqs = append(slots[0].freqs, round1(truth[1]))
		slots[1].freqs = append(slots[1].freqs, round1(truth[0]))
	}
	if spec.Spur != nil {
		for b, fs := range spec.Spur[frame] {
			slots[b].freqs = append(slots[b].freqs, fs...)
		}
	}
	return slots
}

// v2Shift is the deterministic recomputation perturbation of each true peak.
// It is a pure function of (utterance key, frame, band).
func v2Shift(spec Spec, frame, band int) float64 {
	base := []float64{10, 12, -8, 6}
	jitter := float64(((frame*7 + band*13) % 9) - 4) // -4..4 Hz
	switch spec.Utter.Key() {
	case "s1/ai-a":
		// f10 F2 has TWO recomputed peaks in tolerance (ambiguous remap).
		if frame == 10 && band == 1 {
			return 8
		}
		// f9 weak cross candidates all move far away -> unique restoration.
		if frame == 9 {
			return 20
		}
	case "s2/ai-a":
		return base[band] + jitter
	}
	return base[band] + jitter
}

func v2Slots(spec Spec, frame int) []slot {
	v1 := v1Slots(spec, frame)
	slots := make([]slot, model.Bands)
	for b := 0; b < model.Bands; b++ {
		truePeak := v1[b].freqs[0]
		shift := v2Shift(spec, frame, b)
		freqs := []float64{round1(truePeak + shift)}
		// Recomputation drops all weak-energy competitors except the single
		// designed ambiguous case, so remapping is unique there by absence.
		if spec.Utter.Key() == "s1/ai-a" && frame == 10 && b == 1 {
			// Second peak within tolerance of the old F2 peak: ambiguous.
			freqs = append(freqs, round1(truePeak+10))
		}
		slots[b].freqs = freqs
	}
	return slots
}

func round1(x float64) float64 { return float64(int(x*10+0.5)) / 10 }

func buildSet(d *Data, ig *idGen, spec Spec, version string) {
	var idByPick []map[string]int64
	for n := 0; n < spec.Utter.Frames; n++ {
		row := map[string]int64{}
		idByPick = append(idByPick, row)
		if !inWindow(spec, n) || isMissing(spec, n) {
			continue
		}
		var slots []slot
		if version == SetV1 {
			slots = v1Slots(spec, n)
		} else {
			slots = v2Slots(spec, n)
		}
		for b := 0; b < model.Bands; b++ {
			freqs := append([]float64(nil), slots[b].freqs...)
			sort.Float64s(freqs)
			for _, f := range freqs {
				ig.c++
				c := model.Candidate{
					ID:        ig.c,
					SpeakerID: spec.Speaker.ID,
					UtterID:   spec.Utter.ID,
					Version:   version,
					Frame:     n,
					Band:      b,
					Freq:      f,
					Energy:    peakEnergy(spec, n, b, f, version),
				}
				d.Candidates = append(d.Candidates, c)
				row[keyPick(n, b, f)] = c.ID
			}
		}
	}
	// Automatic tracking: greedy within each slot (deterministic), with the
	// fixture's forced swaps on v1 only.
	for n := 0; n < spec.Utter.Frames; n++ {
		if !inWindow(spec, n) || isMissing(spec, n) {
			continue
		}
		var slots []slot
		if version == SetV1 {
			slots = v1Slots(spec, n)
		} else {
			slots = v2Slots(spec, n)
		}
		for b := 0; b < model.Bands; b++ {
			freqs := append([]float64(nil), slots[b].freqs...)
			sort.Float64s(freqs)
			pick := 0
			if version == SetV1 {
				if forced, ok := spec.AutoSwap[n][b]; ok {
					pick = forced
				}
			}
			chosen := freqs[pick]
			ig.a++
			d.Auto = append(d.Auto, model.AutoPoint{
				ID:          ig.a,
				SpeakerID:   spec.Speaker.ID,
				UtterID:     spec.Utter.ID,
				Version:     version,
				Frame:       n,
				Band:        b,
				CandidateID: idByPick[n][keyPick(n, b, chosen)],
				Confidence:  autoConfidence(spec, n, b, pick, len(freqs)),
			})
		}
	}
}

func keyPick(frame, band int, freq float64) string {
	return fmt.Sprintf("%d:%d:%.1f", frame, band, freq)
}

// peakEnergy gives competitors in weak frames slightly higher energy, which is
// why a naive greedy tracker jumps onto them.
func peakEnergy(spec Spec, frame, band int, freq float64, version string) float64 {
	if version == SetV1 && isWeak(spec, frame) {
		truth := branchFreq(spec, frame)
		if abs(freq-truth[band]) > 0.6 {
			return 0.78
		}
	}
	if version == SetV1 && spec.Spur != nil {
		for _, f := range spec.Spur[frame][band] {
			if f == freq {
				return 0.8
			}
		}
	}
	return 0.6
}

func autoConfidence(spec Spec, frame, band, pick, nCand int) float64 {
	if spec.AutoSwap != nil {
		if _, forced := spec.AutoSwap[frame][band]; forced {
			return 0.41
		}
	}
	if nCand > 1 {
		return 0.55
	}
	return 0.9
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func buildSpectrum(out *[]model.SpectrumCell, spec Spec) {
	for n := 0; n < spec.Utter.Frames; n++ {
		if !inWindow(spec, n) {
			continue
		}
		present := !isMissing(spec, n)
		weak := isWeak(spec, n)
		for bin := 0; bin < BinCount; bin++ {
			lo := float64(bin) * BinHz
			hi := lo + BinHz
			e := 0.03
			if present {
				truth := branchFreq(spec, n)
				for b := 0; b < model.Bands; b++ {
					d := distance(lo+BinHz/2, truth[b])
					w := 45.0
					if weak {
						w = 75.0
					}
					if d < w {
						v := 0.9 * (1 - d/w)
						if v > e {
							e = v
						}
					}
				}
				if weak {
					e *= 0.7 // weak energy region
				}
			}
			*out = append(*out, model.SpectrumCell{
				SpeakerID: spec.Speaker.ID, UtterID: spec.Utter.ID,
				Frame: n, Bin: bin, FreqLo: lo, FreqHi: hi, Energy: round2(e),
			})
		}
	}
}

func distance(a, b float64) float64 {
	if a > b {
		return a - b
	}
	return b - a
}

func round2(x float64) float64 { return float64(int(x*100+0.5)) / 100 }
