// Package export 生成可独立重放的修订导出包。
//
// 导出内容三部分齐全:
//   - auto_tracks: 原算法轨迹(原始点, 任何修订都不会改写它们);
//   - operations: 稀疏操作序列(含所基于的候选代);
//   - final: 在每代候选上叠加操作后的最终值。
//
// 连同 candidates / utterances / vowels 一起即可在全新库中独立重放复核。
package export

import (
	"time"

	"voiceprintbench/internal/model"
	"voiceprintbench/internal/ops"
)

const SchemaVersion = "voiceprintbench.v1"

// Bundle 是一次导出。
type Bundle struct {
	Schema      string                                      `json:"schema"`
	GeneratedAt string                                      `json:"generatedAt"`
	Speakers    []model.Speaker                             `json:"speakers"`
	Utterances  []model.Utterance                           `json:"utterances"`
	Vowels      map[string][]model.VowelRange               `json:"vowels"`
	Candidates  map[string][]model.GenCandidates            `json:"candidates"`
	AutoTracks  map[string][]model.AutoTrack                `json:"autoTracks"`
	Operations  map[string][]model.Operation                `json:"operations"`
	Conflicts   map[string][]model.Conflict                 `json:"conflicts"`
	Finals      map[string]map[string][][]ops.FinalPoint    `json:"finals"`
	Issues      map[string]map[string][]ops.ValidationIssue `json:"issues"`
}

// Result 汇总每个发音每代的最终轨迹与校验结果。
type Result struct {
	Finals map[string]map[int][][]ops.FinalPoint
	Issues map[string]map[int][]ops.ValidationIssue
	Confs  map[string][]model.Conflict
}

// Build 构造导出包。
func Build(d *model.Dataset, opsByUtt map[string][]model.Operation, res Result) *Bundle {
	b := &Bundle{
		Schema:      SchemaVersion,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Speakers:    d.Speakers,
		Utterances:  d.Utterances,
		Vowels:      d.Vowels,
		Candidates:  d.Gens,
		AutoTracks:  d.Autos,
		Operations:  opsByUtt,
		Conflicts:   res.Confs,
		Finals:      map[string]map[string][][]ops.FinalPoint{},
		Issues:      map[string]map[string][]ops.ValidationIssue{},
	}
	for uid, byGen := range res.Finals {
		b.Finals[uid] = map[string][][]ops.FinalPoint{}
		b.Issues[uid] = map[string][]ops.ValidationIssue{}
		for gen, final := range byGen {
			b.Finals[uid][genLabel(gen)] = final
			b.Issues[uid][genLabel(gen)] = res.Issues[uid][gen]
		}
	}
	return b
}

func genLabel(gen int) string {
	switch gen {
	case 1:
		return "gen1"
	case 2:
		return "gen2"
	default:
		return "gen" + itoa(gen)
	}
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
