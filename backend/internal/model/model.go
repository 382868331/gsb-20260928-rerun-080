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
	// LineID 非空表示该快照属于某条研究线（研究线有独立的版本序列）。
	LineID    string       `json:"line_id,omitempty"`
	TipHash   string       `json:"tip_hash"`
	TipHeight int64        `json:"tip_height"`
	Blocks    []BlockView  `json:"blocks"`
	TxIndex   []TxPosition `json:"tx_index"`
	TxRefs    []TxRef      `json:"tx_refs"`
	// Checkpoint 是该版本生效的研究者确认检查点（nil 表示尚未确认）。
	Checkpoint *CheckpointView `json:"checkpoint,omitempty"`
	// Conflicts 记录越过检查点的替代链冲突，保留待研究者裁决。
	Conflicts []Conflict `json:"conflicts"`
	// Line 是研究线视图（采用审计、待补充范围、暂缓冲突、撤回记录）。
	Line *LineView `json:"line,omitempty"`
}

// Checkpoint 是研究者明确确认的链检查点：对应区块及其之前
// 的历史范围视为已确认，后续分叉变化不得自动撤销。
type Checkpoint struct {
	Hash        string `json:"hash"`
	Note        string `json:"note,omitempty"`
	ConfirmedAt string `json:"confirmed_at"`
}

// ResearchLine 是一条研究线：两位研究者可以从相同资料出发，
// 各自确认不同的检查点、各自导入新的区块与交易引用。
// 研究线只记录“属于该线的区块身份（按 Hash）”与该线自己的
// 确认/采用/撤回决定；区块内容仍全局按 Hash 去重存储。
type ResearchLine struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	// ParentLines 记录合并来源的两条研究线；原线及其确认依据保持可回看。
	ParentLines []string `json:"parent_lines,omitempty"`
	// BaseComparison 是创建该线的合并所依据的比较 ID（可审计采用决定来源）。
	BaseComparison string       `json:"base_comparison,omitempty"`
	Blocks         []string     `json:"blocks"`
	Checkpoints    []Checkpoint `json:"checkpoints,omitempty"`
	// Adoptions 记录该线每个区块的采用决定来自哪次比较、哪条来源线。
	Adoptions []Adoption `json:"adoptions,omitempty"`
	// Withdrawals 记录后来被撤回的未确认资料。
	Withdrawals []Withdrawal `json:"withdrawals,omitempty"`
	// Deferred 记录合并时暂不采用的相互冲突部分（不是判为无效）。
	Deferred []Deferred `json:"deferred,omitempty"`
}

// Adoption 是一条采用决定：某区块因哪次比较、从哪条研究线被采纳。
type Adoption struct {
	Hash         string `json:"hash"`
	ComparisonID string `json:"comparison_id"`
	SourceLine   string `json:"source_line"` // 来源线 ID；共有资料为 "both"
	AdoptedAt    string `json:"adopted_at"`
}

// Withdrawal 记录一份未确认资料被撤回（已确认范围不允许撤回）。
type Withdrawal struct {
	Hash        string `json:"hash"`
	WithdrawnAt string `json:"withdrawn_at"`
}

// Deferred 记录合并时暂不采用的冲突区块：保留待研究者裁决，
// 不拼接进新链，也不标记为无效资料。
type Deferred struct {
	Hash         string `json:"hash"`
	ComparisonID string `json:"comparison_id"`
	Reason       string `json:"reason"`
}

// PendingGap 是研究线视图中的待补充范围：已采用的区块缺少
// 父区块或引用目标所需的中间资料。缺口随迟到资料到达自动消失，
// 系统不会为了补齐而拼接出从未存在的链。
type PendingGap struct {
	Hash           string   `json:"hash"`
	SourceLine     string   `json:"source_line,omitempty"`
	MissingParents []string `json:"missing_parents,omitempty"`
	MissingRefs    []string `json:"missing_refs,omitempty"`
}

// Comparison 是两条研究线的一次比较结果，持久化保存以便
// 保存重开后复核每个采用决定来自哪次比较。
type Comparison struct {
	ID        string `json:"id"`
	LineA     string `json:"line_a"`
	LineB     string `json:"line_b"`
	CreatedAt string `json:"created_at"`
	// Shared 是两条线共有的事实（按实际区块 Hash 判断，不用高度或交易名代替）。
	Shared []string `json:"shared"`
	OnlyA  []string `json:"only_a"`
	OnlyB  []string `json:"only_b"`
	// CompatibleA/CompatibleB 是各自独有且未跨越对方确认边界的后续资料，
	// 可以在合并时被选择接纳。
	CompatibleA []string `json:"compatible_a"`
	CompatibleB []string `json:"compatible_b"`
	// Conflicts 是跨越确认边界的冲突：一方独有区块与另一方的
	// 已确认检查点矛盾。冲突只被保留，不自动撤销任何确认。
	Conflicts []LineConflict `json:"conflicts"`
	// TxNotes 分别说明同一交易在两条线不同分叉里的出现位置。
	TxNotes []TxForkNote `json:"tx_notes"`
}

// LineConflict 描述一个跨越确认边界的区块。
type LineConflict struct {
	Hash        string `json:"hash"`
	InLine      string `json:"in_line"`      // 区块所在研究线
	CrossesLine string `json:"crosses_line"` // 被跨越确认边界的研究线
	Checkpoint  string `json:"checkpoint"`   // 被跨越的检查点区块 Hash
}

// TxForkNote 说明同一交易在两条研究线中的各自出现位置
// （同一交易在不同分叉里的引用分别列出，不合并成一条）。
type TxForkNote struct {
	TxID      string   `json:"txid"`
	BlocksInA []string `json:"blocks_in_a"`
	BlocksInB []string `json:"blocks_in_b"`
}

// LineView 是快照附带的研究线视图：采用审计、待补充范围、
// 暂缓的冲突与撤回记录都随同一版本快照返回。
type LineView struct {
	ID             string              `json:"id"`
	Name           string              `json:"name"`
	ParentLines    []string            `json:"parent_lines,omitempty"`
	BaseComparison string              `json:"base_comparison,omitempty"`
	Adoptions      map[string]Adoption `json:"adoptions"`
	PendingGaps    []PendingGap        `json:"pending_gaps"`
	Deferred       []Deferred          `json:"deferred"`
	Withdrawals    []Withdrawal        `json:"withdrawals"`
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
// conflict=越过检查点的冲突范围，pending=缺少父块/引用的待补充资料，
// reverted=普通被撤回分叉。
const (
	ScopeConfirmed = "confirmed"
	ScopeTentative = "tentative"
	ScopeConflict  = "conflict"
	ScopePending   = "pending"
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
	// IndependentSources 是目标交易在当前快照中仍存在的出现位置数：
	// 撤回某份未确认资料后，该值大于 0 的引用仍有独立来源。
	IndependentSources int `json:"independent_sources"`
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
