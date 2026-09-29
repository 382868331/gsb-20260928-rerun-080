package main

import (
	"testing"
	"time"
)

func mkBlock(hash, parent string, height int64, weight uint64, txs ...string) Block {
	ts := []Transaction{}
	for i, h := range txs {
		ts = append(ts, Transaction{Hash: h, From: "a", To: "b", Value: "10"})
		_ = i
	}
	return Block{Hash: hash, ParentHash: parent, Height: height, Weight: weight,
		Timestamp: time.Unix(1_700_000_000+height*60, 0), Txs: ts}
}

func genesis(txs ...string) Block { return mkBlock("G", "", 0, 1, txs...) }

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func blockStatus(snap *Snapshot, hash string) string {
	return snap.blockIndex[hash].Status
}

func TestForkReorgWithdrawsOldResults(t *testing.T) {
	s := newTestStore(t)
	// G - A1 - A2 (old main)
	s.Import([]Block{genesis("tx0"), mkBlock("A1", "G", 1, 1, "tx1"), mkBlock("A2", "A1", 2, 1, "tx2")})
	old := s.Current()
	if old.TipHash != "A2" {
		t.Fatalf("tip = %s, want A2", old.TipHash)
	}
	if got := old.txIndex["tx2"]; got.Status != TxConfirmed || got.BlockHash != "A2" {
		t.Fatalf("tx2 pre-reorg: %+v", got)
	}

	// Heavier fork G - B1(work 3) overtakes A2.
	s.Import([]Block{mkBlock("B1", "G", 1, 3, "tx2", "tx3")})
	cur := s.Current()
	if cur.TipHash != "B1" || cur.Reason != "reorg" {
		t.Fatalf("tip=%s reason=%s, want B1/reorg", cur.TipHash, cur.Reason)
	}
	if got := blockStatus(cur, "A1"); got != StatusForked {
		t.Fatalf("A1 status = %s, want forked", got)
	}
	if got := cur.txIndex["tx1"]; got.Status != TxForked || got.Confirmations != 0 {
		t.Fatalf("tx1 not withdrawn: %+v", got)
	}
	// tx2 exists on both branches: position follows the new main chain.
	tx2 := cur.txIndex["tx2"]
	if tx2.Status != TxConfirmed || tx2.BlockHash != "B1" || !tx2.Duplicate {
		t.Fatalf("tx2 after reorg: %+v", tx2)
	}
	if len(tx2.Occurrences) != 2 {
		t.Fatalf("tx2 occurrences = %d, want 2", len(tx2.Occurrences))
	}
	// Old version remains queryable for traceability.
	oldAgain := s.Version(old.Version)
	if oldAgain.TipHash != "A2" || oldAgain.txIndex["tx1"].Status != TxConfirmed {
		t.Fatal("old version was mutated by reorg")
	}
}

func TestInterruptAndDuplicateImportIdempotent(t *testing.T) {
	dir := t.TempDir()
	s1, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	s1.Import([]Block{genesis("shared"), mkBlock("A1", "G", 1, 1)})
	// Simulate an interrupted/retried client request: same hashes again.
	res := s1.Import([]Block{genesis("shared"), mkBlock("A1", "G", 1, 1), mkBlock("A2", "A1", 2, 1)})
	if len(res.Imported) != 1 || res.Imported[0] != "A2" || len(res.Skipped) != 2 || !res.Changed {
		t.Fatalf("unexpected import result: %+v", res)
	}
	v1 := s1.Current().Version

	// Restart by reopening the same data directory.
	s2, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(s2.blocks) != 3 {
		t.Fatalf("reloaded blocks = %d, want 3", len(s2.blocks))
	}
	if s2.Current().Version != v1+1 {
		t.Fatalf("version after reload: %d, want %d", s2.Current().Version, v1+1)
	}
	res2 := s2.Import([]Block{mkBlock("A2", "A1", 2, 1)})
	if len(res2.Skipped) != 1 || res2.Changed {
		t.Fatalf("restart created duplicate index: %+v", res2)
	}
}

func TestOrphanArrivalThenConnected(t *testing.T) {
	s := newTestStore(t)
	s.Import([]Block{mkBlock("X2", "X1", 2, 1, "o1")})
	cur := s.Current()
	if got := blockStatus(cur, "X2"); got != StatusOrphan {
		t.Fatalf("unconnected block status = %s", got)
	}
	if got := cur.txIndex["o1"]; got.Status != TxOrphan || got.Confirmations != 0 {
		t.Fatalf("orphan tx: %+v", got)
	}
	s.Import([]Block{genesis(), mkBlock("X1", "G", 1, 1)})
	cur = s.Current()
	if cur.TipHash != "X2" {
		t.Fatalf("tip after connecting = %s", cur.TipHash)
	}
	if got := cur.txIndex["o1"]; got.Status != TxConfirmed {
		t.Fatalf("connected tx status = %s", got.Status)
	}
}
