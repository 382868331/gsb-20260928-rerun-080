import { useMemo, useState } from "react";
import type { Snapshot, TxPosition } from "../types";
import { SCOPE_LABEL } from "../types";

interface Props {
  snapshot: Snapshot;
  selectedTx: string | null;
  onSelectTx: (txid: string) => void;
  onSelectBlock: (hash: string) => void;
}

// 交易索引按 TxID 分组：分叉间的重复交易会显示多个位置，
// 每个位置各自携带高度、块 Hash、确认状态与所属分支。
export function TxExplorer({ snapshot, selectedTx, onSelectTx, onSelectBlock }: Props) {
  const [query, setQuery] = useState(selectedTx ?? "");

  const groups = useMemo(() => {
    const map = new Map<string, TxPosition[]>();
    for (const p of snapshot.tx_index) {
      const list = map.get(p.txid) ?? [];
      list.push(p);
      map.set(p.txid, list);
    }
    const q = query.trim().toLowerCase();
    return Array.from(map.entries())
      .filter(([id]) => !q || id.toLowerCase().includes(q))
      .sort(([a], [b]) => a.localeCompare(b));
  }, [snapshot, query]);

  const selectedPositions = selectedTx ? groups.find(([id]) => id === selectedTx)?.[1] ?? snapshot.tx_index.filter((p) => p.txid === selectedTx) : null;
  const refs = selectedTx
    ? snapshot.tx_refs.filter((r) => r.from_txid === selectedTx || r.to_txid === selectedTx)
    : [];

  return (
    <div className="tx-explorer">
      <input
        className="search"
        placeholder="按交易 ID 查询（如 txA）"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
      />
      <div className="tx-columns">
        <div>
          <h4>交易索引（{groups.length} 笔）</h4>
          {groups.length === 0 && (
            <div className="panel-empty">
              {query.trim() ? `未找到匹配“${query.trim()}”的交易。` : "当前索引版本中没有交易。"}
            </div>
          )}
          <ul className="tx-list">
            {groups.map(([id, positions]) => (
              <li key={id}>
                <button className={`link-btn ${selectedTx === id ? "active" : ""}`} onClick={() => onSelectTx(id)}>
                  {id}
                </button>
                {positions.length > 1 && <span className="dup-tag">重复 ×{positions.length}（跨分叉）</span>}
              </li>
            ))}
          </ul>
        </div>
        <div>
          {!selectedTx && <div className="panel-empty">选择交易查看位置与引用。</div>}
          {selectedTx && (
            <div>
              <h4>{selectedTx} 的全部出现位置</h4>
              {(selectedPositions ?? []).length === 0 && (
                <div className="panel-empty">交易 {selectedTx} 不在当前索引版本（v{snapshot.version}）中。</div>
              )}
              <table className="data-table">
                <thead><tr><th>高度</th><th>区块</th><th>块内序号</th><th>状态</th><th>范围</th><th>确认数</th><th>分支</th></tr></thead>
                <tbody>
                  {(selectedPositions ?? []).map((p) => (
                    <tr key={`${p.block_hash}-${p.index}`}>
                      <td>{p.height}</td>
                      <td><button className="link-btn" onClick={() => onSelectBlock(p.block_hash)}>{p.block_hash}</button></td>
                      <td>{p.index}</td>
                      <td><span className={p.canonical ? "ok-text" : "reverted-text"}>{p.canonical ? "主链有效" : "旧分叉撤回"}</span></td>
                      <td>{p.scope ? SCOPE_LABEL[p.scope] : "-"}</td>
                      <td>{p.confirmations}</td>
                      <td>{p.branch_id}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              <h4>交易引用</h4>
              {refs.length === 0 && <div className="panel-empty">无引用。</div>}
              <ul className="ref-list">
                {refs.map((r) => (
                  <li key={`${r.from_txid}-${r.from_block_hash}-${r.to_txid}-${r.vout}`}>
                    <div>
                      <button className="link-btn" onClick={() => onSelectTx(r.from_txid)}>{r.from_txid}</button>
                      <span> 花费 → </span>
                      <button className="link-btn" onClick={() => onSelectTx(r.to_txid)}>{r.to_txid}</button>
                      <span> vout {r.vout} </span>
                      <span className={r.active ? "ok-text" : "warn-text"}>
                        {r.active ? "当前有效" : r.resolved ? "非当前链视角" : "目标未到达"}
                      </span>
                    </div>
                    <div className="ref-detail">
                      所在区块{" "}
                      <button className="link-btn" onClick={() => onSelectBlock(r.from_block_hash)}>{r.from_block_hash}</button>
                      {" · "}{r.from_branch_id}{" · "}
                      {r.from_scope ? `${SCOPE_LABEL[r.from_scope]} · ` : ""}
                      {r.from_canonical ? `主链视角 · ${r.from_confirmations} 确认` : "旧分叉视角 · 0 确认"}
                      {" · 目标 "}
                      {r.resolved ? (
                        <>
                          <button className="link-btn" onClick={() => onSelectBlock(r.to_block_hash!)}>{r.to_block_hash}</button>
                          {" · "}{r.to_branch_id}{" · "}
                          {r.to_scope ? `${SCOPE_LABEL[r.to_scope]} · ` : ""}
                          {r.to_canonical ? `主链 ${r.to_confirmations} 确认` : "旧分叉 · 0 确认"}
                        </>
                      ) : (
                        "尚未在索引中解析到"
                      )}
                    </div>
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
