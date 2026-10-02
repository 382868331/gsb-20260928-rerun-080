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
  range?: ChainRange;
  confirmations: number;
  work: number;
  miner?: string;
  timestamp?: string;
  txs: TxInBlock[];
}

export type ChainRange = "confirmed" | "tentative" | "conflict";

export interface TxPosition {
  txid: string;
  block_hash: string;
  index: number;
  height: number;
  canonical: boolean;
  range?: ChainRange;
  confirmations: number;
  branch_id: string;
}

export interface TxRef {
  from_txid: string;
  to_txid: string;
  vout: number;
  resolved: boolean;
  scope?: ChainRange;
  from_block_hash: string;
  from_branch_id: string;
  from_canonical: boolean;
  from_confirmations: number;
  to_block_hash?: string;
  to_branch_id?: string;
  to_canonical: boolean;
  to_confirmations: number;
  active: boolean;
}

export interface CheckpointBasis {
  version: number;
  tip_hash: string;
  tip_height: number;
  chain: string[];
}

export interface CheckpointConflict {
  rival_tip_hash: string;
  rival_tip_height: number;
  rival_work: number;
  diverge_height: number;
  ref_changes?: RefDiff;
}

export interface CheckpointView {
  block_hash: string;
  height: number;
  confirmed_at: string;
  note?: string;
  basis: CheckpointBasis;
  status: "ok" | "conflict";
  conflict?: CheckpointConflict;
}

export interface ResearchLine {
  id: string;
  label: string;
  tip_hash: string;
  tip_height: number;
  created_at: string;
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
  research_lines?: ResearchLine[];
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
