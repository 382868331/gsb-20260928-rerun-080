import type { BlockView, CheckpointView } from "../types";
import { SCOPE_LABEL } from "../types";

interface Props {
  block: BlockView | null;
  checkpoint: CheckpointView | null;
  onSelectTx: (txid: string) => void;
  onConfirmCheckpoint: (hash: string) => void;
  // onWithdraw 仅在研究线视图下提供：撤回该线中未确认的资料。
  onWithdraw?: (hash: string) => void;
}

export function BlockDetail({ block, checkpoint, onSelectTx, onConfirmCheckpoint, onWithdraw }: Props) {
  if (!block) {
    return <div className="panel-empty">点击拓扑图中的区块查看详情。</div>;
  }
  const scope = block.scope ?? (block.canonical ? "tentative" : "reverted");
  const isCheckpoint = checkpoint?.hash === block.hash;
  return (
    <div>
      <div className={`status-badge ${block.canonical ? "ok" : scope === "conflict" ? "conflict" : scope === "pending" ? "pending" : "reverted"}`}>
        {block.canonical
          ? `${SCOPE_LABEL[scope]} · ${block.confirmations} 个确认`
          : `${SCOPE_LABEL[scope]}（${block.branch_id}）· 确认数 0`}
      </div>
      {isCheckpoint && <div className="status-badge checkpoint">研究者确认的检查点</div>}
      <dl className="kv">
        <dt>区块 Hash</dt><dd>{block.hash}</dd>
        <dt>高度</dt><dd>{block.height}</dd>
        <dt>前一块</dt><dd>{block.prev_hash || "（链根）"}</dd>
        <dt>矿工</dt><dd>{block.miner || "-"}</dd>
        <dt>工作量</dt><dd>{block.work}</dd>
        <dt>时间</dt><dd>{block.timestamp || "-"}</dd>
      </dl>
      {block.canonical && !isCheckpoint && (
        <div className="row">
          <button onClick={() => onConfirmCheckpoint(block.hash)}>
            确认为链检查点（此前历史进入确认范围）
          </button>
        </div>
      )}
      {onWithdraw && scope !== "confirmed" && !isCheckpoint && (
        <div className="row">
          <button onClick={() => onWithdraw(block.hash)}>
            从当前研究线撤回该未确认资料
          </button>
        </div>
      )}
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
