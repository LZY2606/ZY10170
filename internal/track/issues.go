package track

import "voicebench/internal/model"

// Issues validates the corrected grid:
//
//   - frequencies must be strictly increasing F1<F2<F3<F4 within every
//     visible in-window frame;
//   - band identities must not be exchanged silently across a run of
//     invisible frames. Detection uses locked boundary frames when available,
//     otherwise the minimum-jump permutation across the gap.
func Issues(d *Data, grid map[int]map[int]Choice,
	locks map[[2]int]int64) []model.Issue {
	var issues []model.Issue
	visible := visibleFrames(d)
	for _, f := range visible {
		issues = append(issues, orderIssues(f, grid)...)
	}
	issues = append(issues, gapIssues(d, visible, grid, locks)...)
	return issues
}

func visibleFrames(d *Data) []int {
	var out []int
	for _, fm := range d.Frames {
		if fm.InWindow && fm.Present {
			out = append(out, fm.Frame)
		}
	}
	return out
}

func orderIssues(f int, grid map[int]map[int]Choice) []model.Issue {
	row := grid[f]
	if len(row) < model.Bands {
		return nil
	}
	var issues []model.Issue
	for b := 0; b < model.Bands-1; b++ {
		lo, ok1 := row[b]
		hi, ok2 := row[b+1]
		if !ok1 || !ok2 {
			continue
		}
		if lo.Candidate.Freq >= hi.Candidate.Freq {
			issues = append(issues, model.Issue{
				Code:    model.IssueOrder,
				Frame:   f,
				Band:    b,
				Message: "帧内频率非严格递增：F" + itoa(b+1) + " >= F" + itoa(b+2),
			})
		}
	}
	return issues
}

// gapIssues scans consecutive visible frames. If frames between them are
// invisible (missing), the permutation of identities across the run is tested.
func gapIssues(d *Data, visible []int,
	grid map[int]map[int]Choice, locks map[[2]int]int64) []model.Issue {
	var issues []model.Issue
	for i := 0; i+1 < len(visible); i++ {
		f0, f1 := visible[i], visible[i+1]
		if f1 == f0+1 {
			continue // adjacent visible frames: nothing invisible to hide in
		}
		if !fullRow(grid, f0) || !fullRow(grid, f1) {
			continue
		}
		want := minJumpPerm(grid, f0, f1)
		if permutationLocked(d, grid, locks, f0, f1, want) {
			continue
		}
		for b := 0; b < model.Bands; b++ {
			if want[b] != b {
				issues = append(issues, model.Issue{
					Code:   model.IssueSwap,
					Frame:  f0,
					Frame2: f1,
					Band:   b,
					Message: "F" + itoa(b+1) + " 身份疑似在缺帧 " +
						itoo(f0+1) + ".." + itoo(f1-1) + " 中被交换",
				})
			}
		}
	}
	return issues
}

func fullRow(grid map[int]map[int]Choice, f int) bool {
	return len(grid[f]) == model.Bands
}

// minJumpPerm returns the right-side band each left-side band most cheaply
// continues to (assignment minimising total |freq difference|), i.e. the
// natural identity continuation across the invisible run.
func minJumpPerm(grid map[int]map[int]Choice, f0, f1 int) [model.Bands]int {
	var cost [model.Bands][model.Bands]float64
	for a := 0; a < model.Bands; a++ {
		for b := 0; b < model.Bands; b++ {
			x := grid[f0][a].Candidate.Freq
			y := grid[f1][b].Candidate.Freq
			cost[a][b] = absf(x - y)
		}
	}
	best := [model.Bands]int{0, 1, 2, 3}
	bestSum := 1e18
	var perm [model.Bands]int
	used := [model.Bands]bool{}
	var rec func(a int, sum float64)
	rec = func(a int, sum float64) {
		if sum >= bestSum {
			return
		}
		if a == model.Bands {
			bestSum = sum
			best = perm
			return
		}
		for b := 0; b < model.Bands; b++ {
			if !used[b] {
				used[b] = true
				perm[a] = b
				rec(a+1, sum+cost[a][b])
				used[b] = false
			}
		}
	}
	rec(0, 0)
	return best
}

// permutationLocked currently accepts the minimum-jump continuation. Explicit
// boundary locks pin identity through the gap, but identity preservation is
// required unconditionally: locks are anchors for remapping, not a relaxation
// of the no-swap rule.
func permutationLocked(_ *Data, _ map[int]map[int]Choice,
	_ map[[2]int]int64, _, _ int, want [model.Bands]int) bool {
	for b := 0; b < model.Bands; b++ {
		if want[b] != b {
			return false
		}
	}
	return true
}

func absf(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// itoo formats a frame index the same way as itoa; distinct name documents the
// "other frame" argument.
func itoo(n int) string { return itoa(n) }
