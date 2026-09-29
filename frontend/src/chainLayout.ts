import type { BlockView } from "./types";

export interface PositionedBlock {
  block: BlockView;
  x: number;
  y: number;
}

// 按分支分列、按高度分行布局：主链在第 0 列，每个分叉独占一列，
// 直观呈现“不是简单列表”的链结构与主/分叉关系。
export function layoutBlocks(blocks: BlockView[]): { nodes: PositionedBlock[]; branches: string[] } {
  const branchCols = new Map<string, number>();
  branchCols.set("main", 0);
  let nextCol = 1;
  for (const b of blocks) {
    if (!b.canonical && !branchCols.has(b.branch_id)) {
      branchCols.set(b.branch_id, nextCol++);
    }
  }
  const nodes = blocks.map((block) => ({
    block,
    x: (branchCols.get(block.canonical ? "main" : block.branch_id) ?? 0) * 210,
    y: block.height * 110,
  }));
  const branches = Array.from(branchCols.keys()).sort((a, b) => branchCols.get(a)! - branchCols.get(b)!);
  return { nodes, branches };
}
