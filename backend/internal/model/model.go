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
	Version   int64  `json:"version"`
	CreatedAt string `json:"created_at"`
	Label     string `json:"label,omitempty"`
	// Study 是该快照所属的研究线 ID；同一研究线内版本各自递增。
	Study     string       `json:"study"`
	TipHash   string       `json:"tip_hash"`
	TipHeight int64        `json:"tip_height"`
	Blocks    []BlockView  `json:"blocks"`
	TxIndex   []TxPosition `json:"tx_index"`
	TxRefs    []TxRef      `json:"tx_refs"`
	// PendingGaps 记录本研究线中父区块缺失的待补充范围
	// （只接纳部分后续资料时，缺少的中间资料保留为缺口，
	// 不拼接出从未存在的链）。
	PendingGaps []string `json:"pending_gaps"`
	// Checkpoint 是该版本生效的研究者确认检查点（nil 表示尚未确认）。
	Checkpoint *CheckpointView `json:"checkpoint,omitempty"`
	// Conflicts 记录越过检查点的替代链冲突，保留待研究者裁决。
	Conflicts []Conflict `json:"conflicts"`
}

// Study 是一条独立的研究线：两位研究者可以从相同资料出发，
// 各自确认检查点、各自导入后续区块与交易引用。研究线之间可以
// 比较与合并，原研究线及其确认依据始终保留可回看。
type Study struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Parents   []string   `json:"parents,omitempty"`
	Origin    string     `json:"origin,omitempty"`
	CreatedAt string     `json:"created_at"`
	Decisions []Decision `json:"decisions"`
}

// Decision 记录一次影响研究线内容的采用决定（合并接纳 / 撤回），
// 保存重开后可复核每个采用决定来自哪次比较。
type Decision struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"` // merge | retract
	Summary     string   `json:"summary"`
	FromStudies []string `json:"from_studies,omitempty"`
	Accepted    []string `json:"accepted,omitempty"`
	// Deferred 是比较中发现但暂不采用的相互冲突区块：
	// 冲突不被标成资料无效，仅保留待后续决定。
	Deferred []string `json:"deferred,omitempty"`
	// PendingGaps 是接纳后仍缺少的中间资料（父区块缺失）。
	PendingGaps    []string `json:"pending_gaps,omitempty"`
	KeptCheckpoint string   `json:"kept_checkpoint,omitempty"`
	Retracted      string   `json:"retracted,omitempty"`
	Version        int64    `json:"version"`
	CreatedAt      string   `json:"created_at"`
}

// 比较时一个区块相对于另一研究线的关系：
// shared=两线共有（按实际区块身份 Hash 判断），
// compatible=兼容的后续资料（延续对方已有历史，不跨越确认边界），
// conflict=跨越对方确认边界的冲突，
// disconnected=缺少中间资料，暂时无法接到对方链上。
const (
	RelShared       = "shared"
	RelCompatible   = "compatible"
	RelConflict     = "conflict"
	RelDisconnected = "disconnected"
)

// ComparedBlock 是比较结果中的一个区块及其相对另一研究线的关系。
type ComparedBlock struct {
	Hash      string `json:"hash"`
	PrevHash  string `json:"prev_hash"`
	Height    int64  `json:"height"`
	Scope     string `json:"scope"`
	Relation  string `json:"relation"`
	Canonical bool   `json:"canonical"`
}

// CompareResult 是两条研究线的对比结果：共有事实、各自独有资料
// （按兼容/冲突/缺中间资料分类）以及各自独有的交易引用。
type CompareResult struct {
	StudyA      string          `json:"study_a"`
	StudyB      string          `json:"study_b"`
	CheckpointA *CheckpointView `json:"checkpoint_a,omitempty"`
	CheckpointB *CheckpointView `json:"checkpoint_b,omitempty"`
	Shared      []ComparedBlock `json:"shared"`
	OnlyA       []ComparedBlock `json:"only_a"`
	OnlyB       []ComparedBlock `json:"only_b"`
	RefsOnlyA   []TxRef         `json:"refs_only_a"`
	RefsOnlyB   []TxRef         `json:"refs_only_b"`
}

// Checkpoint 是研究者明确确认的链检查点：对应区块及其之前
// 的历史范围视为已确认，后续分叉变化不得自动撤销。
type Checkpoint struct {
	Hash        string `json:"hash"`
	Note        string `json:"note,omitempty"`
	ConfirmedAt string `json:"confirmed_at"`
}

// CheckpointView 是快照中生效检查点的视图，附带由链接关系计算的高度。
type CheckpointView struct {
	Hash        string `json:"hash"`
	Height      int64  `json:"height"`
	Note        string `json:"note,omitempty"`
	ConfirmedAt string `json:"confirmed_at"`
}

// Conflict 描述一条越过检查点的替代链：它不从检查点区块延伸，
// 与已确认历史矛盾。系统不自动撤销原确认，仅保留冲突及其引用差异，
// 由研究者决定是否建立独立研究（Status=independent）。
type Conflict struct {
	ID          string `json:"id"`
	RivalTip    string `json:"rival_tip"`
	RivalHeight int64  `json:"rival_height"`
	RivalWork   int64  `json:"rival_work"`
	Status      string `json:"status"` // pending | independent
}

// 范围标记：confirmed=检查点确认范围，tentative=检查点之后的暂定范围，
// conflict=越过检查点的冲突范围，reverted=普通被撤回分叉。
const (
	ScopeConfirmed = "confirmed"
	ScopeTentative = "tentative"
	ScopeConflict  = "conflict"
	ScopeReverted  = "reverted"
)

type BlockView struct {
	Hash          string      `json:"hash"`
	PrevHash      string      `json:"prev_hash"`
	Height        int64       `json:"height"`
	Canonical     bool        `json:"canonical"`
	BranchID      string      `json:"branch_id"`
	Scope         string      `json:"scope"`
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
	Scope         string `json:"scope"`
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
	FromBlockHash     string `json:"from_block_hash"`
	FromBranchID      string `json:"from_branch_id"`
	FromCanonical     bool   `json:"from_canonical"`
	FromScope         string `json:"from_scope"`
	FromConfirmations int64  `json:"from_confirmations"`
	ToBlockHash       string `json:"to_block_hash,omitempty"`
	ToBranchID        string `json:"to_branch_id,omitempty"`
	ToCanonical       bool   `json:"to_canonical"`
	ToScope           string `json:"to_scope,omitempty"`
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
