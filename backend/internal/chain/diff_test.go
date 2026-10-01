package chain

import (
	"testing"

	"chainresearch/internal/model"
	"chainresearch/internal/sample"
)

func refByKey(snap model.VersionSnapshot, from, block, to string) *model.TxRef {
	for i := range snap.TxRefs {
		r := &snap.TxRefs[i]
		if r.FromTxID == from && r.FromBlockHash == block && r.ToTxID == to {
			return r
		}
	}
	return nil
}

func changesByKind(diff model.RefDiff, kind string) []model.RefChange {
	var out []model.RefChange
	for _, c := range diff.Changes {
		if c.Kind == kind {
			out = append(out, c)
		}
	}
	return out
}

func TestDiffRefsAcrossReorg(t *testing.T) {
	blocks := sample.Blocks()
	var old []model.Block
	for _, b := range blocks {
		if b.Hash == "G" || b.Hash[0] == 'A' {
			old = append(old, b)
		}
	}
	snap1 := BuildSnapshot(old, 1, "旧链")
	snap2 := BuildSnapshot(blocks, 2, "分叉切换")

	diff := DiffRefs(snap1, snap2)
	if diff.FromVersion != 1 || diff.ToVersion != 2 {
		t.Fatalf("差异版本号错误: %+v", diff)
	}
	// 旧链上的引用位置未变，仅确认数与有效性变化。
	confs := changesByKind(diff, "confirmations")
	if len(confs) != 3 {
		t.Fatalf("旧链 3 条引用应仅确认数变化，实际 %d: %+v", len(confs), confs)
	}
	for _, c := range confs {
		if c.Before == nil || c.After == nil || c.Before.FromBlockHash != c.After.FromBlockHash {
			t.Fatalf("仅确认数变化不应改变位置: %+v", c)
		}
		if c.Before.Confirmations == c.After.Confirmations && c.Before.Valid == c.After.Valid {
			t.Fatalf("确认数或有效性应发生变化: %+v", c)
		}
	}
	// 新主链带来的引用为新增。
	added := changesByKind(diff, "added")
	if len(added) != 3 {
		t.Fatalf("应新增 3 条引用（txA@B2、txB3、txB4），实际 %d: %+v", len(added), added)
	}
	if len(changesByKind(diff, "removed")) != 0 {
		t.Fatal("追加式导入不应有引用消失")
	}
}

func TestDiffRefsMovedAndRetargeted(t *testing.T) {
	mk := func(from, block, to string, conf int64) model.TxRef {
		return model.TxRef{
			FromTxID: from, ToTxID: to, FromBlockHash: block,
			Canonical: true, Confirmations: conf, Resolved: true, ToCanonical: true, Valid: true,
		}
	}
	snap1 := model.VersionSnapshot{Version: 1, TxRefs: []model.TxRef{
		mk("tx1", "X", "tx0", 3),
		mk("tx2", "X", "txA", 3),
	}}
	snap2 := model.VersionSnapshot{Version: 2, TxRefs: []model.TxRef{
		mk("tx1", "Y", "tx0", 1),
		mk("tx2", "X", "txB", 1),
	}}
	diff := DiffRefs(snap1, snap2)

	moved := changesByKind(diff, "moved")
	if len(moved) != 1 || moved[0].Before.FromBlockHash != "X" || moved[0].After.FromBlockHash != "Y" {
		t.Fatalf("tx1 应识别为位置迁移 X->Y: %+v", moved)
	}
	retargeted := changesByKind(diff, "retargeted")
	if len(retargeted) != 1 || retargeted[0].Before.ToTxID != "txA" || retargeted[0].After.ToTxID != "txB" {
		t.Fatalf("tx2 应识别为引用目标变化 txA->txB: %+v", retargeted)
	}
	if len(changesByKind(diff, "added")) != 0 || len(changesByKind(diff, "removed")) != 0 {
		t.Fatalf("迁移与目标变化不应再计为增删: %+v", diff.Changes)
	}
}
