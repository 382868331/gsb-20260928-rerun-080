import type { Block } from "./types";

const base = 1_700_000_000_000;
const at = (minutes: number) => new Date(base + minutes * 60_000).toISOString();

const tx = (hash: string, from: string, to: string, value: string) => ({ hash, from, to, value });

// Initial chain G-A1-A2. tx-shared deliberately appears on both branches so
// its position and confirmation state must move during the reorg.
export const initialChain: Block[] = [
  {
    hash: "G", parentHash: "", height: 0, weight: 1, timestamp: at(0), miner: "miner-0",
    transactions: [tx("tx-genesis", "0x0", "alice", "1000")],
  },
  {
    hash: "A1", parentHash: "G", height: 1, weight: 1, timestamp: at(1), miner: "alice",
    transactions: [tx("tx-a1", "alice", "bob", "12")],
  },
  {
    hash: "A2", parentHash: "A1", height: 2, weight: 1, timestamp: at(2), miner: "alice",
    transactions: [tx("tx-shared", "carol", "dave", "7")],
  },
];

// Heavier fork overtaking at height 1; it also contains an unconnected header
// (orphan C3) to demonstrate disconnected arrivals.
export const reorgBatch: Block[] = [
  {
    hash: "B1", parentHash: "G", height: 1, weight: 4, timestamp: at(3), miner: "bob",
    transactions: [
      tx("tx-shared", "carol", "dave", "7"),
      tx("tx-b1-only", "bob", "erin", "99"),
    ],
  },
  {
    hash: "B2", parentHash: "B1", height: 2, weight: 2, timestamp: at(4), miner: "bob",
    transactions: [tx("tx-b2", "erin", "frank", "3")],
  },
  {
    hash: "C3", parentHash: "C2-missing", height: 3, weight: 1, timestamp: at(5), miner: "?",
    transactions: [tx("tx-orphan", "nobody", "nobody", "0")],
  },
];

// Late-arriving extension of the old fork: old branch stays traceable.
export const oldForkExtend: Block[] = [
  {
    hash: "A3", parentHash: "A2", height: 3, weight: 1, timestamp: at(6), miner: "alice",
    transactions: [tx("tx-a3", "dave", "grace", "5")],
  },
];
