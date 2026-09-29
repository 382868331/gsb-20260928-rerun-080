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
  confirmations: number;
  branch_id: string;
}

export interface TxRef {
  from_txid: string;
  to_txid: string;
  vout: number;
  resolved: boolean;
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
