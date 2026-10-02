import type { BlockView } from "../types";

export const RANGE_LABEL: Record<string, string> = {
  confirmed: "确认范围",
  tentative: "暂定范围",
  conflict: "冲突范围",
};

interface Props {
  block: BlockView | null;
  onSelectTx: (txid: string) => void;
}

export function BlockDetail({ block, onSelectTx }: Props) {
  if (!block) {
    return <div className="panel-empty">点击拓扑图中的区块查看详情。</div>;
  }
  return (
    <div>
      <div className={`status-badge ${block.canonical ? "ok" : "reverted"}`}>
        {block.canonical ? `主链 · ${block.confirmations} 个确认` : `已撤回（${block.branch_id}）· 确认数 0`}
      </div>
      {block.range && (
        <div className={`status-badge range-${block.range}`}>{RANGE_LABEL[block.range]}</div>
      )}
      <dl className="kv">
        <dt>区块 Hash</dt><dd>{block.hash}</dd>
        <dt>高度</dt><dd>{block.height}</dd>
        <dt>前一块</dt><dd>{block.prev_hash || "（链根）"}</dd>
        <dt>矿工</dt><dd>{block.miner || "-"}</dd>
        <dt>工作量</dt><dd>{block.work}</dd>
        <dt>时间</dt><dd>{block.timestamp || "-"}</dd>
      </dl>
      <h4>块内交易（{block.txs.length}）</h4>
      <table className="data-table">
        <thead><tr><th>#</th><th>交易 ID</th><th>备注</th></tr></thead>
        <tbody>
          {block.txs.map((tx) => (
            <tr key={`${tx.index}-${tx.txid}`}>
              <td>{tx.index}</td>
              <td><button className="link-btn" onClick={() => onSelectTx(tx.txid)}>{tx.txid}</button></td>
              <td>{tx.memo || ""}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
