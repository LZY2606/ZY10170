package store

import (
	"context"
	"path/filepath"
	"testing"

	"voiceprintbench/internal/fixture"
)

func TestImportAndReset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	ctx := context.Background()

	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ResetAndImport(ctx, fixture.Build()); err != nil {
		t.Fatal(err)
	}
	d, err := st.LoadDataset(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Speakers) != 2 || len(d.Utterances) != 2 {
		t.Fatalf("说话人/发音数量错误: %+v %+v", d.Speakers, d.Utterances)
	}
	// 48 帧中 42..46 为缺帧(无候选行): 每代 43 帧, 两代 86 行。
	if got := len(d.Gens["u01"]); got != 43*2 {
		t.Fatalf("u01 候选行数应为86(两代*43个有候选帧), 实际 %d", got)
	}

	runs, err := st.ListRuns(ctx, 10)
	if err != nil || len(runs) == 0 {
		t.Fatalf("运行记录缺失: %v %v", runs, err)
	}

	// 清空重导入后数据仍然齐全。
	if err := st.ResetAndImport(ctx, fixture.Build()); err != nil {
		t.Fatal(err)
	}
	d2, err := st.LoadDataset(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(d2.Gens["u01"]) != 43*2 {
		t.Fatalf("重导入后候选行数错误: %d", len(d2.Gens["u01"]))
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	// 重新打开同一文件, 数据持久化。
	st2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	ts, err := st2.Meta(ctx, "imported_at")
	if err != nil || ts == "" {
		t.Fatalf("重开后导入标记丢失: %q %v", ts, err)
	}
}
