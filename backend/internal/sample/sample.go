package sample

import "chainresearch/internal/model"

// Blocks 构造一个带分叉、重复交易与交易互引的数据集：
// G -> A1 -> A2 -> A3  （旧链，累计工作量 30）
//
//	\-> B1 -> B2 -> B3 -> B4 （新主链，累计工作量 40）
//
// txA 在旧链 A2 与新链 B2 中重复出现（同一交易跨分叉各有一份）。
func Blocks() []model.Block {
	coinG := model.Transaction{TxID: "coin-G", Memo: "创世 coinbase", Outputs: []model.TxOutput{{Value: 100, To: "addr"}}}
	coinA1 := model.Transaction{TxID: "coin-A1", Outputs: []model.TxOutput{{Value: 50, To: "addr"}}}
	txA := model.Transaction{
		TxID:    "txA",
		Memo:    "重复交易：同时存在于旧链 A2 与新链 B2",
		Inputs:  []model.TxInput{{TxID: "coin-G", Vout: 0}},
		Outputs: []model.TxOutput{{Value: 40, To: "addr"}},
	}
	txA2 := model.Transaction{TxID: "txA2", Inputs: []model.TxInput{{TxID: "coin-A1", Vout: 0}}, Outputs: []model.TxOutput{{Value: 20, To: "addr"}}}
	txA3 := model.Transaction{TxID: "txA3", Inputs: []model.TxInput{{TxID: "txA2", Vout: 0}}, Outputs: []model.TxOutput{{Value: 10, To: "addr"}}}

	coinB1 := model.Transaction{TxID: "coin-B1", Outputs: []model.TxOutput{{Value: 60, To: "addr"}}}
	txB3 := model.Transaction{TxID: "txB3", Inputs: []model.TxInput{{TxID: "coin-B1", Vout: 0}}, Outputs: []model.TxOutput{{Value: 30, To: "addr"}}}
	txB4 := model.Transaction{TxID: "txB4", Inputs: []model.TxInput{{TxID: "txB3", Vout: 0}}, Outputs: []model.TxOutput{{Value: 15, To: "addr"}}}

	return []model.Block{
		{Hash: "G", PrevHash: "", Work: 100, Miner: "genesis", Timestamp: "2026-09-01T00:00:00Z", Txs: []model.Transaction{coinG}},
		{Hash: "A1", PrevHash: "G", Work: 10, Miner: "m1", Timestamp: "2026-09-02T00:00:00Z", Txs: []model.Transaction{coinA1}},
		{Hash: "A2", PrevHash: "A1", Work: 10, Miner: "m2", Timestamp: "2026-09-03T00:00:00Z", Txs: []model.Transaction{txA, txA2}},
		{Hash: "A3", PrevHash: "A2", Work: 10, Miner: "m2", Timestamp: "2026-09-04T00:00:00Z", Txs: []model.Transaction{txA3}},
		{Hash: "B1", PrevHash: "G", Work: 10, Miner: "m3", Timestamp: "2026-09-03T00:00:00Z", Txs: []model.Transaction{coinB1}},
		{Hash: "B2", PrevHash: "B1", Work: 10, Miner: "m3", Timestamp: "2026-09-04T00:00:00Z", Txs: []model.Transaction{txA}},
		{Hash: "B3", PrevHash: "B2", Work: 10, Miner: "m4", Timestamp: "2026-09-05T00:00:00Z", Txs: []model.Transaction{txB3}},
		{Hash: "B4", PrevHash: "B3", Work: 10, Miner: "m4", Timestamp: "2026-09-06T00:00:00Z", Txs: []model.Transaction{txB4}},
	}
}
