// Package fixture 生成固定的声谱实验数据。
//
// 口径:
//   - 固定帧率 100Hz, 每帧 10ms; 主发音 u01 共 48 帧 (0..47)。
//   - 帧 42..46 为缺帧: 两代候选集合中这些帧均无候选, 不补零、不插值。
//   - 弱能量区为半开区间 [18,29), 上升峰流 a 与下凹峰流 b 在帧 23 与帧 26
//     附近两度交叉(弱能量下的局部抖动), 每帧候选仍按频率严格升序。
//   - 原算法(gen1)在弱能量帧 22 把 F2/F3 候选选反(同帧不再严格递增);
//     并在缺帧缺口后的帧 47 发生身份交换(F2/F3 对调), 违背“跨不可见帧不换轨”。
//   - 候选集合重算(gen2): 峰流频率与 gen1 相同, 追踪修复; 仅帧 22 额外保留
//     杂峰 1318Hz, 使旧 F2 目标 1288Hz 在 45Hz 容差内出现两个候选
//     (1288 与 1318), 构成歧义映射, 两项都保留, 不按距离强行选一个。
package fixture

import (
	"math"
	"strings"

	"voiceprintbench/internal/model"
)

const (
	FrameMs   = 10
	NumFrames = 48

	// MapTolHz 旧候选映射到新候选的频率容差; 容差内候选数:
	// 1 => 唯一映射自动恢复; >1 => 歧义保留全部; 0 => unmatched。
	MapTolHz = 45.0
	// GapContinuityTolHz 跨缺帧缺口按线性外推核对身份时允许的偏差。
	GapContinuityTolHz = 60.0
)

// weakStart/weakEnd 为弱能量区半开区间 [18,29)。
const weakStart, weakEnd = 18, 29

// Missing 为缺帧集合 (42..46)。
func Missing(f int) bool { return f >= 42 && f <= 46 }

// MissingFrame 供外部按帧号判断 u01 的缺帧; 非 u01 发音无缺帧。
func MissingFrame(uid string, f int) bool {
	if uid == "u01" {
		return Missing(f)
	}
	return false
}

func round1(x float64) float64 { return math.Round(x*10) / 10 }

// streamA 上升峰流; streamB 在弱能量区有一处窄下凹, 与 a 两度交叉。
func streamA(f int) float64 { return 1200 + 4.0*float64(f) }
func streamB(f float64) float64 {
	dip := 560 * math.Exp(-math.Pow((f-24.0)/3.5, 2))
	return 2050 - 12*f - dip
}
func f1(f int) float64 { return 700 + 18*math.Sin(2*math.Pi*float64(f)/48) }
func f4(f int) float64 { return 3250 + 40*math.Sin(2*math.Pi*float64(f)/40) }

// orderedSlots 返回某帧 F1,F2,F3,F4 的有序频率槽(严格递增)。
func orderedSlots(f int) [4]float64 {
	a, b := streamA(f), streamB(float64(f))
	lo, hi := a, b
	if lo > hi {
		lo, hi = hi, lo
	}
	return [4]float64{round1(f1(f)), round1(lo), round1(hi), round1(f4(f))}
}

func weakEnergy(f int) bool { return f >= weakStart && f < weakEnd }

func confFor(f, rank int) float64 {
	switch {
	case weakEnergy(f):
		if rank == 4 {
			return 0.82
		}
		return round1(0.30 + 0.10*math.Abs(math.Sin(float64(f)*1.7+float64(rank))))
	case f == 41 || f == 47:
		return 0.62
	default:
		return round1(0.80 + 0.13*math.Abs(math.Cos(float64(f)+float64(rank)*0.7)))
	}
}

func energyFor(f, rank int) float64 {
	if weakEnergy(f) {
		return round1(0.25 + 0.15*math.Abs(math.Sin(float64(f*3+rank))))
	}
	return round1(0.6 + 0.3*math.Abs(math.Cos(float64(f*2+rank))))
}

type rawCand struct {
	freq, energy, conf float64
}

// frameCandidates 构造某代某帧的候选(升序)。缺帧返回 nil。
func frameCandidates(uid string, gen, f int) []model.Candidate {
	if Missing(f) {
		return nil
	}
	slots := orderedSlots(f)
	var raws []rawCand
	put := func(freq, energy, conf float64) {
		raws = append(raws, rawCand{round1(freq), energy, conf})
	}
	put(slots[0], energyFor(f, 1), confFor(f, 1))
	put(slots[3], energyFor(f, 4), confFor(f, 4))

	// 两代峰流频率相同, 保证未触碰帧的映射唯一(频率差 0)。
	a, b := streamA(f), streamB(float64(f))
	put(a, energyFor(f, 2), confFor(f, 2))
	put(b, energyFor(f, 3), confFor(f, 3))
	if gen == 1 && weakEnergy(f) {
		// gen1 弱能量区杂峰: 研究者必须看得到原算法面对的每个候选。
		z := round1((a+b)/2 + 30*math.Sin(float64(f)*2.2))
		put(z, 0.18, 0.22)
	}
	if gen == 2 && f == 22 {
		// 重算后唯一保留的杂峰: 与旧 F2 目标 1288 相距 30Hz, 构成两项歧义。
		put(1318, 0.20, 0.24)
	}

	sortRaws(raws)
	out := make([]model.Candidate, 0, len(raws))
	seen := map[float64]bool{}
	for _, r := range raws {
		if seen[r.freq] {
			continue
		}
		seen[r.freq] = true
		out = append(out, model.Candidate{
			ID:         candID(uid, gen, f, r.freq),
			Frequency:  r.freq,
			Energy:     r.energy,
			Confidence: r.conf,
		})
	}
	return out
}

func sortRaws(rs []rawCand) {
	for i := 1; i < len(rs); i++ {
		for j := i; j > 0 && rs[j-1].freq > rs[j].freq; j-- {
			rs[j-1], rs[j] = rs[j], rs[j-1]
		}
	}
}

func candID(uid string, gen, f int, freq float64) string {
	return "c-" + uid + "-g" + itoa(gen) + "-f" + itoa(f) + "-" + formatFreq(freq)
}

func formatFreq(v float64) string {
	n := int(math.Round(math.Abs(v) * 10))
	return itoa(n/10) + "." + itoa(n%10)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// pickByFreq 在候选集合中找到频率最接近目标的候选。
func pickByFreq(cs []model.Candidate, freq float64) model.Candidate {
	best := cs[0]
	for _, c := range cs[1:] {
		if math.Abs(c.Frequency-freq) < math.Abs(best.Frequency-freq) {
			best = c
		}
	}
	return best
}

// autoTrack 生成原算法自动轨迹: gen1 在 f22 选反 F2/F3, 并在缺口后 f47 换轨;
// gen2 追踪修复, 全部按有序频率槽就近选取。
func autoTrack(gen, f int, cs []model.Candidate) []model.AutoTrack {
	if Missing(f) {
		return nil
	}
	slots := orderedSlots(f)
	pick := func(rank int) model.Candidate {
		if gen == 1 && f == 22 && (rank == 2 || rank == 3) {
			// 弱能量区交叉杂峰导致的跳轨: F2/F3 选反。
			if rank == 2 {
				return pickByFreq(cs, slots[2])
			}
			return pickByFreq(cs, slots[1])
		}
		if gen == 1 && f == 47 && (rank == 2 || rank == 3) {
			// 跨缺帧缺口的身份交换: 到达帧仍按缺口前身份接续, 形成同帧逆序。
			if rank == 2 {
				return pickByFreq(cs, slots[2])
			}
			return pickByFreq(cs, slots[1])
		}
		return pickByFreq(cs, slots[rank-1])
	}
	out := make([]model.AutoTrack, 0, 4)
	for rank := 1; rank <= 4; rank++ {
		c := pick(rank)
		out = append(out, model.AutoTrack{
			Gen:         gen,
			Frame:       framePtr(f),
			Rank:        rank,
			CandidateID: c.ID,
			Frequency:   c.Frequency,
			Confidence:  c.Confidence,
			Visible:     true,
		})
	}
	return out
}

func framePtr(f int) int { return f }

// Build 构造固定数据集。
func Build() *model.Dataset {
	d := &model.Dataset{
		Speakers: []model.Speaker{
			{ID: "s1", Name: "说话人 甲"},
			{ID: "s2", Name: "说话人 乙"},
		},
		Utterances: []model.Utterance{
			{ID: "u01", SpeakerID: "s1", Label: "发音版本 take1 /a/", FrameRate: 100, FrameMs: FrameMs, NumFrames: NumFrames},
			{ID: "u02", SpeakerID: "s2", Label: "发音版本 take1 /i/", FrameRate: 100, FrameMs: FrameMs, NumFrames: 30},
		},
		Gens:   map[string][]model.GenCandidates{},
		Autos:  map[string][]model.AutoTrack{},
		Vowels: map[string][]model.VowelRange{},
	}

	buildU01(d)
	buildU02(d)
	return d
}

func buildU01(d *model.Dataset) {
	uid := "u01"
	d.Vowels[uid] = []model.VowelRange{
		{ID: "v1", Label: "元音 i", Start: 6, End: 18},
		{ID: "v2", Label: "元音 a", Start: 24, End: 36},
	}
	for gen := 1; gen <= 2; gen++ {
		for f := 0; f < NumFrames; f++ {
			cs := frameCandidates(uid, gen, f)
			d.Gens[uid] = append(d.Gens[uid], model.GenCandidates{Gen: gen, Frame: f, Candidates: cs})
			d.Autos[uid] = append(d.Autos[uid], autoTrack(gen, f, cs)...)
		}
	}
}

// buildU02 乙的简单发音: 30 帧, 仅一代候选, 无交叉/缺帧, 供多说话人与
// 清空重导入复核使用。
func buildU02(d *model.Dataset) {
	uid := "u02"
	d.Vowels[uid] = []model.VowelRange{{ID: "v1", Label: "元音 i", Start: 4, End: 26}}
	for f := 0; f < 30; f++ {
		freqs := [4]float64{
			round1(320 + 10*math.Sin(float64(f)/3)),
			round1(2200 + 30*math.Sin(float64(f)/5)),
			round1(2900 + 20*math.Cos(float64(f)/4)),
			round1(3400 + 15*math.Sin(float64(f)/6)),
		}
		cs := make([]model.Candidate, 0, 4)
		for _, q := range freqs {
			cs = append(cs, model.Candidate{
				ID:         candID(uid, 1, f, q),
				Frequency:  q,
				Energy:     0.8,
				Confidence: 0.9,
			})
		}
		d.Gens[uid] = append(d.Gens[uid], model.GenCandidates{Gen: 1, Frame: f, Candidates: cs})
		for rank := 1; rank <= 4; rank++ {
			d.Autos[uid] = append(d.Autos[uid], model.AutoTrack{
				Gen: 1, Frame: f, Rank: rank,
				CandidateID: cs[rank-1].ID, Frequency: cs[rank-1].Frequency,
				Confidence: 0.9, Visible: true,
			})
		}
	}
}

// GenCount 返回某发音的候选代数。
func GenCount(uid string) int {
	if strings.HasPrefix(uid, "u01") {
		return 2
	}
	return 1
}
