export interface TxOutput {
  value: number;
  to?: string;
}

export interface TxInput {
  txid: string;
  vout: number;
}

export interface TxInBlock {
  txid: string;
  index: number;
  memo?: string;
}

export interface BlockView {
  hash: string;
  prev_hash: string;
  height: number;
  canonical: boolean;
  branch_id: string;
  scope?: Scope;
  confirmations: number;
  work: number;
  miner?: string;
  timestamp?: string;
  txs: TxInBlock[];
}

export interface TxPosition {
  txid: string;
  block_hash: string;
  index: number;
  height: number;
  canonical: boolean;
  scope?: Scope;
  confirmations: number;
  branch_id: string;
}

// 范围标记：confirmed=检查点确认范围，tentative=检查点后暂定范围，
// conflict=越过检查点的冲突范围，pending=缺少依据的待补充资料，reverted=普通被撤回分叉。
export type Scope = "confirmed" | "tentative" | "conflict" | "pending" | "reverted";

export const SCOPE_LABEL: Record<Scope, string> = {
  confirmed: "确认范围",
  tentative: "暂定范围",
  conflict: "冲突范围",
  pending: "待补充",
  reverted: "已撤回旧分叉",
};

export interface CheckpointView {
  hash: string;
  height: number;
  note?: string;
  confirmed_at: string;
}

export interface Conflict {
  id: string;
  rival_tip: string;
  rival_height: number;
  rival_work: number;
  status: "pending" | "independent";
}

export interface TxRef {
  from_txid: string;
  to_txid: string;
  vout: number;
  resolved: boolean;
  from_block_hash: string;
  from_branch_id: string;
  from_canonical: boolean;
  from_scope?: Scope;
  from_confirmations: number;
  to_block_hash?: string;
  to_branch_id?: string;
  to_canonical: boolean;
  to_scope?: Scope;
  to_confirmations: number;
  active: boolean;
  // 目标交易在当前快照中的出现位置数：撤回未确认资料后，
  // 大于 0 表示该引用仍有独立来源。
  independent_sources: number;
}

// 研究线：两位研究者各自确认检查点、各自导入资料的一条研究线索。
export interface LineMeta {
  id: string;
  name: string;
  created_at: string;
  parent_lines?: string[];
  base_comparison?: string;
  block_count: number;
  checkpoints: number;
  latest_version: number;
}

export interface Adoption {
  hash: string;
  comparison_id: string;
  source_line: string; // 来源线 ID；共有资料为 "both"
  adopted_at: string;
}

export interface Withdrawal {
  hash: string;
  withdrawn_at: string;
}

export interface Deferred {
  hash: string;
  comparison_id: string;
  reason: string;
}

export interface PendingGap {
  hash: string;
  source_line?: string;
  missing_parents?: string[];
  missing_refs?: string[];
}

// 研究线视图：采用审计、待补充范围、暂缓冲突与撤回记录。
export interface LineView {
  id: string;
  name: string;
  parent_lines?: string[];
  base_comparison?: string;
  adoptions: Record<string, Adoption>;
  pending_gaps: PendingGap[];
  deferred: Deferred[];
  withdrawals: Withdrawal[];
}

export interface LineConflict {
  hash: string;
  in_line: string;
  crosses_line: string;
  checkpoint: string;
}

export interface TxForkNote {
  txid: string;
  blocks_in_a: string[];
  blocks_in_b: string[];
}

// 两条研究线的一次比较结果（持久化，供采用决定溯源）。
export interface Comparison {
  id: string;
  line_a: string;
  line_b: string;
  created_at: string;
  shared: string[];
  only_a: string[];
  only_b: string[];
  compatible_a: string[];
  compatible_b: string[];
  conflicts: LineConflict[];
  tx_notes: TxForkNote[];
}

export type RefDiffKind =
  | "position_migrated"
  | "target_changed"
  | "confirmations_only"
  | "added"
  | "removed";

export interface RefDiffEntry {
  kind: RefDiffKind;
  before?: TxRef;
  after?: TxRef;
}

export interface RefDiff {
  from_version: number;
  to_version: number;
  entries: RefDiffEntry[];
}

export interface Snapshot {
  version: number;
  created_at: string;
  label?: string;
  line_id?: string;
  tip_hash: string;
  tip_height: number;
  blocks: BlockView[];
  tx_index: TxPosition[];
  tx_refs: TxRef[];
  checkpoint?: CheckpointView | null;
  conflicts: Conflict[];
  line?: LineView | null;
}

export interface VersionMeta {
  version: number;
  created_at: string;
  label?: string;
  tip_hash: string;
  tip_height: number;
}

export interface ImportResponse {
  added: number;
  skipped: number;
  state: Snapshot;
  line_state?: Snapshot | null;
}

export interface RawBlock {
  hash: string;
  prev_hash: string;
  miner?: string;
  work?: number;
  timestamp?: string;
  txs: Array<{
    txid: string;
    inputs?: TxInput[];
    outputs?: TxOutput[];
    memo?: string;
  }>;
}
