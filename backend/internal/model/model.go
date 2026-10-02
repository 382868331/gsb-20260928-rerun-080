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
	Version       int64           `json:"version"`
	CreatedAt     string          `json:"created_at"`
	Label         string          `json:"label,omitempty"`
	TipHash       string          `json:"tip_hash"`
	TipHeight     int64           `json:"tip_height"`
	Blocks        []BlockView     `json:"blocks"`
	TxIndex       []TxPosition    `json:"tx_index"`
	TxRefs        []TxRef         `json:"tx_refs"`
	Checkpoint    *CheckpointView `json:"checkpoint,omitempty"`
	ResearchLines []ResearchLine  `json:"research_lines,omitempty"`
}

type BlockView struct {
	Hash          string      `json:"hash"`
	PrevHash      string      `json:"prev_hash"`
	Height        int64       `json:"height"`
	Canonical     bool        `json:"canonical"`
	BranchID      string      `json:"branch_id"`
	Range         string      `json:"range,omitempty"`
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

// 链范围标记：confirmed=研究者确认范围（检查点及以前的主链），
// tentative=暂定范围（检查点之后的主链与常规分叉），
// conflict=冲突范围（越过检查点、试图改写已确认历史的分叉）。
// 未设置检查点时为空字符串。
const (
	RangeConfirmed = "confirmed"
	RangeTentative = "tentative"
	RangeConflict  = "conflict"
)

// Checkpoint 是研究者明确确认的链检查点：确认时刻的主链上
// 某一区块及其之前的全部历史被视为已确认范围，之后的新资料
// 若要改写这段历史，不会被自动采纳，而是作为冲突保留。
type Checkpoint struct {
	BlockHash   string          `json:"block_hash"`
	Height      int64           `json:"height"`
	ConfirmedAt string          `json:"confirmed_at"`
	Note        string          `json:"note,omitempty"`
	Basis       CheckpointBasis `json:"basis"`
}

// CheckpointBasis 记录确认时的依据（当时的版本、主链端点与
// 从链根到检查点的完整已确认链段），持久化后重开仍可追踪。
type CheckpointBasis struct {
	Version   int64    `json:"version"`
	TipHash   string   `json:"tip_hash"`
	TipHeight int64    `json:"tip_height"`
	Chain     []string `json:"chain"`
}

// CheckpointView 是某一索引版本中检查点的解释。
// Status：ok=当前主链包含检查点（或冲突已被登记为独立研究线）；
// conflict=出现了越过检查点的更重竞争链，等待研究者决定。
type CheckpointView struct {
	BlockHash   string              `json:"block_hash"`
	Height      int64               `json:"height"`
	ConfirmedAt string              `json:"confirmed_at"`
	Note        string              `json:"note,omitempty"`
	Basis       CheckpointBasis     `json:"basis"`
	Status      string              `json:"status"`
	Conflict    *CheckpointConflict `json:"conflict,omitempty"`
}

// CheckpointConflict 描述一条越过检查点的竞争链：
// 系统不自动撤销原确认，主链仍钉在包含检查点的链上，
// 同时保留竞争链信息与两种视角下的引用差异，供研究者裁决。
type CheckpointConflict struct {
	RivalTipHash   string   `json:"rival_tip_hash"`
	RivalTipHeight int64    `json:"rival_tip_height"`
	RivalWork      int64    `json:"rival_work"`
	DivergeHeight  int64    `json:"diverge_height"`
	RefChanges     *RefDiff `json:"ref_changes,omitempty"`
}

// ResearchLine 是研究者从冲突中建立的独立研究线：
// 竞争链被登记为独立研究对象，不再作为待决冲突，
// 原检查点确认保持不变。
type ResearchLine struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	TipHash   string `json:"tip_hash"`
	TipHeight int64  `json:"tip_height"`
	CreatedAt string `json:"created_at"`
}

// TxPosition 描述同一交易在某版本中的全部出现位置。
// 分叉链之间允许出现重复交易，因此一笔交易可对应多个位置。
type TxPosition struct {
	TxID          string `json:"txid"`
	BlockHash     string `json:"block_hash"`
	Index         int    `json:"index"`
	Height        int64  `json:"height"`
	Canonical     bool   `json:"canonical"`
	Range         string `json:"range,omitempty"`
	Confirmations int64  `json:"confirmations"`
	BranchID      string `json:"branch_id"`
}

// TxRef 是交易之间的引用关系（input 花费某笔前序交易）。
// 同一交易在多个分叉出现时，每个出现位置各产生一条引用记录，
// 各自携带所在区块、链视角（分支）与当前有效性；切换历史版本后
// 看到的即是该版本当时的解释。Active 表示当前主链视角下有效：
// 引用方位置在主链且引用目标也已在主链解析到。引用尚未到达
// （未解析）或只位于被撤回分叉时 Active=false，不提前认作有效。
type TxRef struct {
	FromTxID          string `json:"from_txid"`
	ToTxID            string `json:"to_txid"`
	Vout              int    `json:"vout"`
	Resolved          bool   `json:"resolved"`
	Scope             string `json:"scope,omitempty"`
	FromBlockHash     string `json:"from_block_hash"`
	FromBranchID      string `json:"from_branch_id"`
	FromCanonical     bool   `json:"from_canonical"`
	FromConfirmations int64  `json:"from_confirmations"`
	ToBlockHash       string `json:"to_block_hash,omitempty"`
	ToBranchID        string `json:"to_branch_id,omitempty"`
	ToCanonical       bool   `json:"to_canonical"`
	ToConfirmations   int64  `json:"to_confirmations"`
	Active            bool   `json:"active"`
}

// RefDiffEntry 描述两个索引版本之间一条引用关系的变化。
// Kind 取值：added / removed / position_migrated / target_changed /
// confirmations_only。
type RefDiffEntry struct {
	Kind   string `json:"kind"`
	Before *TxRef `json:"before,omitempty"`
	After  *TxRef `json:"after,omitempty"`
}

// RefDiff 是分叉切换前后两个版本的引用差异结果。
type RefDiff struct {
	FromVersion int64          `json:"from_version"`
	ToVersion   int64          `json:"to_version"`
	Entries     []RefDiffEntry `json:"entries"`
}
