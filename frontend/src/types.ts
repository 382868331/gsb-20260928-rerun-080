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
// conflict=越过检查点的冲突范围，reverted=普通被撤回分叉。
export type Scope = "confirmed" | "tentative" | "conflict" | "reverted";

export const SCOPE_LABEL: Record<Scope, string> = {
  confirmed: "确认范围",
  tentative: "暂定范围",
  conflict: "冲突范围",
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
  tip_hash: string;
  tip_height: number;
  blocks: BlockView[];
  tx_index: TxPosition[];
  tx_refs: TxRef[];
  checkpoint?: CheckpointView | null;
  conflicts: Conflict[];
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
