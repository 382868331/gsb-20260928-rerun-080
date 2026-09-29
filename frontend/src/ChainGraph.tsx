import { useMemo } from "react";
import { ReactFlow, Background, Controls, type Node, type Edge } from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import type { BlockView } from "./types";

interface Props {
  blocks: BlockView[];
  selected: string | null;
  onSelect: (hash: string) => void;
}

const colorByStatus: Record<string, string> = {
  canonical: "#2fbf71",
  forked: "#f5a623",
  orphan: "#7c88b3",
};

export default function ChainGraph({ blocks, selected, onSelect }: Props) {
  const { nodes, edges } = useMemo(() => {
    // Lay out connected blocks by height; forks spread on the x axis.
    const colCount: Record<number, number> = {};
    const connected = blocks.filter((b) => b.status !== "orphan");
    const orphans = blocks.filter((b) => b.status === "orphan");

    const assignCols = (list: BlockView[], gapX: number) => {
      for (const b of list) {
        const col = colCount[b.height] ?? 0;
        colCount[b.height] = col + 1;
        nodes.push({
          id: b.hash,
          position: { x: col * gapX, y: b.height * 70 },
          data: { label: `${b.hash} · h${b.height}` },
          style: {
            background: "#131a2e",
            color: "#e6ecff",
            border: `2px ${selected === b.hash ? "#7db4ff" : colorByStatus[b.status]}`,
            borderRadius: 8,
            padding: "6px 10px",
            fontSize: 12,
            width: 130,
          },
        });
      }
    };

    const nodes: Node[] = [];
    assignCols(connected, 170);
    for (const b of orphans) {
      const col = colCount[b.height] ?? 0;
      colCount[b.height] = col + 1;
      nodes.push({
        id: b.hash,
        position: { x: col * 170 + 560, y: b.height * 70 },
        data: { label: `${b.hash} · h${b.height} (孤)` },
        style: { background: "#131a2e", color: "#cdd5f5", border: "2px #7c88b3", borderRadius: 8, padding: "6px 10px", fontSize: 12, width: 130 },
      });
    }

    const edges: Edge[] = [];
    for (const b of blocks) {
      if (!b.parentHash || !blocks.find((x) => x.hash === b.parentHash)) continue;
      const parentStatus = blocks.find((x) => x.hash === b.parentHash)?.status;
      const strong = b.status === "canonical" && parentStatus === "canonical";
      edges.push({
        id: `${b.parentHash}->${b.hash}`,
        source: b.parentHash,
        target: b.hash,
        animated: strong,
        style: { stroke: strong ? "#2fbf71" : "#5a6aa0", strokeWidth: strong ? 2.5 : 1.5 },
      });
    }
    return { nodes, edges };
  }, [blocks, selected]);

  return (
    <div className="graph">
      <ReactFlow
        nodes={nodes}
        edges={edges}
        fitView
        nodesDraggable
        onNodeClick={(_, node) => onSelect(node.id)}
      >
        <Background color="#1c2648" gap={24} />
        <Controls showInteractive={false} />
      </ReactFlow>
    </div>
  )
}
