import { useMemo } from "react";
import { ReactFlow, Background, Controls } from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import type { BlockView } from "../types";
import { layoutBlocks } from "../chainLayout";

interface Props {
  blocks: BlockView[];
  selectedBlock: string | null;
  onSelect: (hash: string) => void;
}

const NODE_W = 184;
const NODE_H = 104;

export function ChainGraph({ blocks, selectedBlock, onSelect }: Props) {
  const { nodes, edges } = useMemo(() => {
    const { nodes: positioned } = layoutBlocks(blocks);
    const flowNodes = positioned.map(({ block, x, y }) => ({
      id: block.hash,
      position: { x, y },
      data: { label: blockLabel(block, selectedBlock === block.hash) },
      style: nodeStyle(block, selectedBlock === block.hash),
    }));
    const flowEdges = positioned
      .filter(({ block }) => block.prev_hash)
      .map(({ block }) => ({
        id: `${block.prev_hash}->${block.hash}`,
        source: block.prev_hash,
        target: block.hash,
        animated: block.canonical && blocks.find((b) => b.hash === block.prev_hash)?.canonical,
        style: {
          stroke: block.canonical && blocks.find((b) => b.hash === block.prev_hash)?.canonical ? "#2563eb" : "#f59e0b",
          strokeWidth: 2,
        },
      }));
    return { nodes: flowNodes, edges: flowEdges };
  }, [blocks, selectedBlock]);

  return (
    <div style={{ height: 460, border: "1px solid #d4d4d8", borderRadius: 8, background: "#fafafa" }}>
      <ReactFlow nodes={nodes} edges={edges} onNodeClick={(_, n) => onSelect(n.id)} fitView proOptions={{ hideAttribution: true }}>
        <Background />
        <Controls />
      </ReactFlow>
    </div>
  );
}

function blockLabel(b: BlockView, selected: boolean) {
  return (
    <div style={{ padding: 4, fontSize: 12, textAlign: "left", width: NODE_W - 16, outline: selected ? "2px solid #0f172a" : "none", borderRadius: 6 }}>
      <div style={{ fontWeight: 700 }}>{b.hash} #{b.height}</div>
      <div style={{ color: b.canonical ? "#1d4ed8" : "#b45309", fontWeight: 600 }}>
        {b.canonical ? `主链 · ${b.confirmations} 确认` : `已撤回旧分叉 · ${b.branch_id}`}
      </div>
      {b.range && (
        <div style={{ color: rangeColor(b.range), fontWeight: 600 }}>{rangeLabel(b.range)}</div>
      )}
      <div style={{ color: "#52525b" }}>{b.txs.length} 笔交易 · work {b.work}</div>
    </div>
  );
}

function rangeLabel(range: string) {
  return range === "confirmed" ? "确认范围" : range === "conflict" ? "冲突范围" : "暂定范围";
}

function rangeColor(range: string) {
  return range === "confirmed" ? "#15803d" : range === "conflict" ? "#b91c1c" : "#1d4ed8";
}

function nodeStyle(b: BlockView, selected: boolean): React.CSSProperties {
  const bg = b.range === "confirmed" ? "#dcfce7" : b.range === "conflict" ? "#fee2e2" : b.canonical ? "#dbeafe" : "#fef3c7";
  return {
    width: NODE_W,
    height: NODE_H,
    borderRadius: 8,
    border: selected ? "2px solid #0f172a" : "1px solid #cbd5e1",
    background: bg,
    padding: 0,
  };
}
