package store

import (
	"path/filepath"
	"testing"

	"chainresearch/internal/sample"
)

func TestIdempotentImportAndResume(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	blocks := sample.Blocks()
	firstHalf := blocks[:4] // G,A1,A2,A3
	r1, err := s.AddBlocks(firstHalf)
	if err != nil || r1.Added != 4 || r1.Skipped != 0 {
		t.Fatalf("首次导入异常: %+v %v", r1, err)
	}
	// 模拟导入中断后从头重放全部数据：前 4 个跳过，只新增后 4 个。
	r2, err := s.AddBlocks(blocks)
	if err != nil || r2.Added != 4 || r2.Skipped != 4 {
		t.Fatalf("续传导入应跳过已索引区块: %+v %v", r2, err)
	}
	// 再重放一次：没有任何新增，不产生重复。
	r3, err := s.AddBlocks(blocks)
	if err != nil || r3.Added != 0 || r3.Skipped != 8 {
		t.Fatalf("完全重复导入应全部跳过: %+v %v", r3, err)
	}
	if len(s.AllBlocks()) != 8 {
		t.Fatalf("区块总数应保持 8，实际 %d", len(s.AllBlocks()))
	}
	// 重新打开存储（模拟进程重启续传），结果保持一致。
	s2, err := Open(dir)
	if err != nil || len(s2.AllBlocks()) != 8 {
		t.Fatalf("重启后应恢复 8 个区块: %d %v", len(s2.AllBlocks()), err)
	}
	r4, err := s2.AddBlocks(blocks)
	if err != nil || r4.Added != 0 {
		t.Fatalf("重启后重放不应产生新增: %+v %v", r4, err)
	}
}

func TestConflictingHashRejected(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	blocks := sample.Blocks()
	if _, err := s.AddBlocks(blocks[:2]); err != nil {
		t.Fatal(err)
	}
	conflict := blocks[:2]
	conflict[0].Miner = "篡改者"
	if _, err := s.AddBlocks(conflict); err == nil {
		t.Fatal("同 Hash 不同内容必须拒绝")
	}
	if len(s.AllBlocks()) != 2 {
		t.Fatalf("冲突拒绝必须保持原子性，区块数仍为 2，实际 %d", len(s.AllBlocks()))
	}
}
