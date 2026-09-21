package fixture

import "testing"

func TestFixtureShapes(t *testing.T) {
	d := Build()
	uid := "u01"

	// 缺帧: 两代候选集合均为空, 自动轨迹也无点。
	for gen := 1; gen <= 2; gen++ {
		for f := 42; f <= 46; f++ {
			var cs []struct{}
			_ = cs
			for _, gc := range d.Gens[uid] {
				if gc.Gen == gen && gc.Frame == f && len(gc.Candidates) != 0 {
					t.Fatalf("gen%d 缺帧 f%d 不应有候选", gen, f)
				}
			}
			for _, a := range d.Autos[uid] {
				if a.Gen == gen && a.Frame == f {
					t.Fatalf("gen%d 缺帧 f%d 不应有自动轨迹点", gen, f)
				}
			}
		}
	}

	// 每个存在候选的帧, 候选频率必须严格升序。
	for _, gc := range d.Gens[uid] {
		for i := 1; i < len(gc.Candidates); i++ {
			if gc.Candidates[i].Frequency <= gc.Candidates[i-1].Frequency {
				t.Fatalf("gen%d f%d 候选未严格升序: %v", gc.Gen, gc.Frame,
					gc.Candidates)
			}
		}
	}

	// 弱能量区两条峰流必须交叉(排序槽随帧发生 a/b 上下换位)。
	a23, b23 := streamA(23), streamB(23)
	a26, b26 := streamA(26), streamB(26)
	if !(a23 < b23 && a26 > b26) && !(a23 > b23 && a26 < b26) {
		t.Fatalf("峰流未在弱能量区交叉: f23 a=%.1f b=%.1f, f26 a=%.1f b=%.1f",
			a23, b23, a26, b26)
	}
}

func TestGen1AutoHasExpectedDefects(t *testing.T) {
	d := Build()
	uid := "u01"
	auto := map[[2]int]float64{}
	for _, a := range d.Autos[uid] {
		if a.Gen == 1 {
			auto[[2]int{a.Frame, a.Rank}] = a.Frequency
		}
	}
	// f22: F2/F3 选反 -> F2 > F3。
	if !(auto[[2]int{22, 2}] > auto[[2]int{22, 3}]) {
		t.Fatalf("gen1 f22 应选反 F2/F3: F2=%v F3=%v",
			auto[[2]int{22, 2}], auto[[2]int{22, 3}])
	}
	// f47: 跨缺口身份交换 -> F2 > F3。
	if !(auto[[2]int{47, 2}] > auto[[2]int{47, 3}]) {
		t.Fatalf("gen1 f47 应发生身份交换: F2=%v F3=%v",
			auto[[2]int{47, 2}], auto[[2]int{47, 3}])
	}
	// gen2 两处均修复为严格递增。
	a2 := map[[2]int]float64{}
	for _, a := range d.Autos[uid] {
		if a.Gen == 2 {
			a2[[2]int{a.Frame, a.Rank}] = a.Frequency
		}
	}
	for _, f := range []int{22, 47} {
		if !(a2[[2]int{f, 1}] < a2[[2]int{f, 2}] &&
			a2[[2]int{f, 2}] < a2[[2]int{f, 3}] &&
			a2[[2]int{f, 3}] < a2[[2]int{f, 4}]) {
			t.Fatalf("gen2 f%d 应严格递增", f)
		}
	}
}

func TestGen2AmbiguityCandidates(t *testing.T) {
	d := Build()
	uid := "u01"
	var f22g1, f22g2 []float64
	for _, gc := range d.Gens[uid] {
		if gc.Frame != 22 {
			continue
		}
		for _, c := range gc.Candidates {
			if gc.Gen == 1 {
				f22g1 = append(f22g1, c.Frequency)
			} else {
				f22g2 = append(f22g2, c.Frequency)
			}
		}
	}
	// 旧 F2 目标 1288 在 gen2 必须恰好有两个 45Hz 内容差候选: 1288 与 1318。
	hits := 0
	for _, q := range f22g2 {
		if abs(q-1288) <= MapTolHz {
			hits++
		}
	}
	if hits != 2 {
		t.Fatalf("gen2 f22 对旧 F2(1288) 应有恰好2个容差内候选, 实际 %d: g1=%v g2=%v",
			hits, f22g1, f22g2)
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
