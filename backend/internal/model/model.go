package model

// Block 是人工导入的区块数据。Height 不直接提供，
// 由索引依据 PrevHash 链接计算，避免伪造高度造成链结构错误。
type Block struct {
	Hash      string        `json:"hash"`
	PrevHash  string        `json:"prev_hash"`
	Miner     string        `json:"miner,omitempty"`
	Work      int64         `json:"work,omitempty"`
	Timestamp string        `json:"timestamp,omitempty"`
	Txs       []Transaction `json:"txs"`
}

// Transaction 是区块内的一笔交易。Inputs 中每项通过 TxID 引用
// 被花费的前序交易（Coinbase 交易 Inputs 为空）。
type Transaction struct {
	TxID    string     `json:"txid"`
	Inputs  []TxInput  `json:"inputs,omitempty"`
	Outputs []TxOutput `json:"outputs,omitempty"`
	Memo    string     `json:"memo,omitempty"`
}

type TxInput struct {
	TxID string `json:"txid"`
	Vout int    `json:"vout"`
}

type TxOutput struct {
	Value int64  `json:"value"`
	To    string `json:"to,omitempty"`
}

// StoredBlock 是去重后持久化的区块，附带交易在块内的位置。
type StoredBlock struct {
	Block Block `json:"block"`
}

// VersionSnapshot 是某一索引版本的完整不可变状态，
// 前端在同一版本内看到的所有数据都来自该快照。
type VersionSnapshot struct {
	Version   int64        `json:"version"`
	CreatedAt string       `json:"created_at"`
	Label     string       `json:"label,omitempty"`
	TipHash   string       `json:"tip_hash"`
	TipHeight int64        `json:"tip_height"`
	Blocks    []BlockView  `json:"blocks"`
	TxIndex   []TxPosition `json:"tx_index"`
	TxRefs    []TxRef      `json:"tx_refs"`
}

type BlockView struct {
	Hash          string      `json:"hash"`
	PrevHash      string      `json:"prev_hash"`
	Height        int64       `json:"height"`
	Canonical     bool        `json:"canonical"`
	BranchID      string      `json:"branch_id"`
	Confirmations int64       `json:"confirmations"`
	Work          int64       `json:"work"`
	Miner         string      `json:"miner,omitempty"`
	Timestamp     string      `json:"timestamp,omitempty"`
	Txs           []TxInBlock `json:"txs"`
}

type TxInBlock struct {
	TxID  string `json:"txid"`
	Index int    `json:"index"`
	Memo  string `json:"memo,omitempty"`
}

// TxPosition 描述同一交易在某版本中的全部出现位置。
// 分叉链之间允许出现重复交易，因此一笔交易可对应多个位置。
type TxPosition struct {
	TxID          string `json:"txid"`
	BlockHash     string `json:"block_hash"`
	Index         int    `json:"index"`
	Height        int64  `json:"height"`
	Canonical     bool   `json:"canonical"`
	Confirmations int64  `json:"confirmations"`
	BranchID      string `json:"branch_id"`
}

// TxRef 是交易之间的引用关系（input 花费某笔前序交易）。
type TxRef struct {
	FromTxID string `json:"from_txid"`
	ToTxID   string `json:"to_txid"`
	Vout     int    `json:"vout"`
	Resolved bool   `json:"resolved"`
}
