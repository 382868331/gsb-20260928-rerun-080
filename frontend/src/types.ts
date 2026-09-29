export type BlockStatus = "canonical" | "forked" | "orphan";
export type TxStatus = "confirmed" | "forked" | "orphan";

export interface Transaction {
  hash: string;
  from: string;
  to: string;
  value: string;
}

export interface Block {
  hash: string;
  parentHash: string;
  height: number;
  weight: number;
  timestamp: string;
  miner: string;
  transactions: Transaction[];
}

export interface OccurrenceView {
  blockHash: string;
  height: number;
  index: number;
  status: BlockStatus;
}

export interface BlockView {
  hash: string;
  parentHash: string;
  height: number;
  weight: number;
  chainWork: number;
  timestamp: string;
  miner: string;
  status: BlockStatus;
  confirmations: number;
  transactionHashes: string[];
}

export interface TransactionView {
  hash: string;
  from: string;
  to: string;
  value: string;
  status: TxStatus;
  duplicate: boolean;
  confirmations: number;
  blockHash: string;
  blockHeight: number;
  occurrences: OccurrenceView[];
}

export interface Snapshot {
  version: number;
  previousVersion: number;
  createdAt: string;
  reason: string;
  tipHash: string;
  height: number;
  blocks: BlockView[];
  transactions: TransactionView[];
}

export interface ImportResult {
  imported: string[];
  skipped: string[];
  invalid: string[];
  previousVersion: number;
  currentVersion: number;
  tipHash: string;
  reason: string;
  changed: boolean;
}
