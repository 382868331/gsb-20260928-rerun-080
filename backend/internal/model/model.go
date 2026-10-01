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
// 每条引用同时记录花费方所在位置（区块、高度、链视角），
// 以及在该版本下是否当前有效：
//   - Resolved：被引用的前序交易已出现在索引中；
//   - ToCanonical：被引用交易在主链上有有效位置；
//   - Valid：花费方位于主链、引用已解析且目标也在主链。
// 引用尚未到达（未解析）或花费方/目标位于被撤回分叉时 Valid=false。
type TxRef struct {
	FromTxID      string `json:"from_txid"`
	ToTxID        string `json:"to_txid"`
	Vout          int    `json:"vout"`
	FromBlockHash string `json:"from_block_hash"`
	FromHeight    int64  `json:"from_height"`
	BranchID      string `json:"branch_id"`
	Canonical     bool   `json:"canonical"`
	Confirmations int64  `json:"confirmations"`
	Resolved      bool   `json:"resolved"`
	ToCanonical   bool   `json:"to_canonical"`
	Valid         bool   `json:"valid"`
}

// RefChange 描述两个索引版本之间一条引用关系的变化。
// Kind 取值：
//   - added / removed：引用出现或消失；
//   - moved：同一引用（from->to:vout）的花费方位置迁移到另一区块/高度；
//   - retargeted：同一花费位置（from_txid@区块:vout）的引用目标发生变化；
//   - confirmations：位置与目标未变，仅确认数变化。
type RefChange struct {
	Kind     string `json:"kind"`
	FromTxID string `json:"from_txid"`
	ToTxID   string `json:"to_txid"`
	Vout     int    `json:"vout"`
	Before   *TxRef `json:"before,omitempty"`
	After    *TxRef `json:"after,omitempty"`
}

// RefDiff 是两个索引版本之间引用关系的完整差异。
type RefDiff struct {
	FromVersion int64       `json:"from_version"`
	ToVersion   int64       `json:"to_version"`
	Changes     []RefChange `json:"changes"`
}
