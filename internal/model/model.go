package model

// 数据口径:
//   - 帧 frame 为非负整数, 帧号即索引; 采样以固定帧率导入, 每帧 10ms。
//   - 所有半开区间 [start,end) 均为“左闭右开”; 刚好落在分析窗右端的帧不属于当前窗。
//   - 缺帧不出现在候选集合中; 任何轨迹在缺帧处都没有值, 绝不补零频率。
//   - 频率单位 Hz; 同帧内同一轨迹的最终值必须满足 F1 < F2 < F3 < F4(严格递增)。
//   - 轨迹身份(rank 1..4 => F1..F4)在不可见帧(缺帧/隐藏段)上不交换;
//     轨迹在不可见帧处断开为独立可见段, 不跨缺口连线。

// Candidate 是某一帧频谱上的一个共振峰候选。
type Candidate struct {
	ID         string  `json:"id"`
	Frequency  float64 `json:"frequency"`
	Energy     float64 `json:"energy"`
	Confidence float64 `json:"confidence"`
}

// GenCandidates 是某次候选集合重算后, 某代(gen)候选在某帧上的候选集合。
type GenCandidates struct {
	Gen        int         `json:"gen"`
	Frame      int         `json:"frame"`
	Candidates []Candidate `json:"candidates"` // 同帧内按 frequency 严格升序
}

// AutoTrack 是原算法在某代候选上的自动追踪结果点。
// Missing=true 表示该帧缺帧(不存在任何候选, 不补零)。
type AutoTrack struct {
	Gen         int     `json:"gen"`
	Frame       int     `json:"frame"`
	Rank        int     `json:"rank"` // 1..4 => F1..F4
	CandidateID string  `json:"candidateId"`
	Frequency   float64 `json:"frequency"`
	Confidence  float64 `json:"confidence"`
	Visible     bool    `json:"visible"`
	Missing     bool    `json:"missing"`
}

// VowelRange 元音标注区间, 半开 [Start,End)。
type VowelRange struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

// 操作类型: 拖动单点 / 重选一段候选路径 / 锁定边界帧。
const (
	OpDrag     = "drag"
	OpReselect = "reselect_path"
	OpLock     = "lock_boundary"
)

// DragPayload 把 (frame,rank) 上的最终值改选到指定候选。
type DragPayload struct {
	FromCandidateID string `json:"fromCandidateId"`
	ToCandidateID   string `json:"toCandidateId"`
}

// ReselectPayload 重选 [StartFrame,EndFrame) 上某个 rank 的候选路径。
// Picked 为 frame -> 该帧上选中的候选 ID; 稀疏操作只记录受影响帧。
type ReselectPayload struct {
	StartFrame int            `json:"startFrame"`
	EndFrame   int            `json:"endFrame"`
	Picked     map[int]string `json:"picked"`
}

// LockPayload 锁定边界帧(元音区间起止); 重放时要求候选唯一映射, 否则进入冲突。
type LockPayload struct {
	Frame       int    `json:"frame"`
	VowelID     string `json:"vowelId"`
	Edge        string `json:"edge"` // start | end
	CandidateID string `json:"candidateId"`
}

// Operation 是一条稀疏修订, 基于某代候选; 重跑后映射到更新的代。
type Operation struct {
	ID           string           `json:"id"`
	UtteranceID  string           `json:"utteranceId"`
	Gen          int              `json:"gen"`
	Frame        int              `json:"frame,omitempty"`
	Rank         int              `json:"rank,omitempty"`
	Type         string           `json:"type"`
	Drag         *DragPayload     `json:"drag,omitempty"`
	Reselect     *ReselectPayload `json:"reselect,omitempty"`
	Lock         *LockPayload     `json:"lock,omitempty"`
	Note         string           `json:"note,omitempty"`
	CreatedAt    string           `json:"createdAt"`
	ResolvedFrom string           `json:"resolvedFrom,omitempty"`
}

// Conflict 是重放后无法唯一映射的一次触点。绝不按距离强行选一个。
type Conflict struct {
	ID             string   `json:"id"`
	UtteranceID    string   `json:"utteranceId"`
	FromOpID       string   `json:"fromOpId"`
	FromGen        int      `json:"fromGen"`
	ToGen          int      `json:"toGen"`
	Type           string   `json:"type"` // ambiguous | unmatched
	Frame          int      `json:"frame"`
	Rank           int      `json:"rank,omitempty"`
	OldCandidateID string   `json:"oldCandidateId"`
	OldFrequency   float64  `json:"oldFrequency"`
	CandidateIDs   []string `json:"candidateIds"`
	Resolved       bool     `json:"resolved"`
}

// RunRecord 一次可导出的运行记录。
type RunRecord struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"` // import | replay | resolve | reset | export
	Detail    string `json:"detail"`
	CreatedAt string `json:"createdAt"`
}

// Speaker 说话人。
type Speaker struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Utterance 发音版本。
type Utterance struct {
	ID        string  `json:"id"`
	SpeakerID string  `json:"speakerId"`
	Label     string  `json:"label"`
	FrameRate float64 `json:"frameRate"`
	FrameMs   int     `json:"frameMs"`
	NumFrames int     `json:"numFrames"`
}

// Dataset 导入后的完整固定数据(不含修订)。
type Dataset struct {
	Speakers   []Speaker                  `json:"speakers"`
	Utterances []Utterance                `json:"utterances"`
	Gens       map[string][]GenCandidates `json:"-"`
	Autos      map[string][]AutoTrack     `json:"-"`
	Vowels     map[string][]VowelRange    `json:"-"`
}
