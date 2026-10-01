import { useEffect, useState } from "react";
import { fetchRefDiff } from "../api";
import type { RefChange, RefDiff, TxRef, VersionMeta } from "../types";

interface Props {
  versions: VersionMeta[];
  current: number;
  onSelectTx: (txid: string) => void;
  onSelectBlock: (hash: string) => void;
}

const KIND_LABEL: Record<RefChange["kind"], string> = {
  moved: "位置迁移",
  retargeted: "引用目标变化",
  confirmations: "仅确认数变化",
  added: "新增引用",
  removed: "消失引用",
};

// 分叉切换前后的引用差异查看：任选两个索引版本对比，
// 区分位置迁移、引用目标变化与仅确认数变化。
export function RefDiffPanel({ versions, current, onSelectTx, onSelectBlock }: Props) {
  const sorted = [...versions].sort((a, b) => a.version - b.version);
  const [fromV, setFromV] = useState<number | null>(null);
  const [toV, setToV] = useState<number | null>(null);
  const [diff, setDiff] = useState<RefDiff | null>(null);
  const [error, setError] = useState<string | null>(null);

  const from = fromV ?? (sorted.length > 1 ? sorted[sorted.length - 2].version : current);
  const to = toV ?? current;

  useEffect(() => {
    if (versions.length === 0 || from === to) {
      setDiff(null);
      return;
    }
    let cancelled = false;
    setError(null);
    fetchRefDiff(from, to)
      .then((d) => { if (!cancelled) setDiff(d); })
      .catch((e) => { if (!cancelled) { setDiff(null); setError(e instanceof Error ? e.message : String(e)); } });
    return () => { cancelled = true; };
  }, [from, to, versions.length]);

  if (versions.length < 2) {
    return <div className="panel-empty">至少需要两个索引版本才能对比引用差异。</div>;
  }

  const groups = new Map<RefChange["kind"], RefChange[]>();
  for (const c of diff?.changes ?? []) {
    const list = groups.get(c.kind) ?? [];
    list.push(c);
    groups.set(c.kind, list);
  }

  const refPos = (r?: TxRef) =>
    r ? (
      <span>
        <button className="link-btn" onClick={() => onSelectBlock(r.from_block_hash)}>{r.from_block_hash}</button>
        #{r.from_height}（{r.branch_id}{r.canonical ? "，主链" : "，撤回"}，{r.confirmations} 确认
        {r.valid ? "，当前有效" : "，当前无效"}）
      </span>
    ) : null;

  return (
    <div className="ref-diff">
      <div className="row">
        <label>
          从版本{" "}
          <select value={from} onChange={(e) => setFromV(Number(e.target.value))}>
            {sorted.map((v) => <option key={v.version} value={v.version}>v{v.version}{v.label ? ` · ${v.label}` : ""}</option>)}
          </select>
        </label>
        <label>
          到版本{" "}
          <select value={to} onChange={(e) => setToV(Number(e.target.value))}>
            {sorted.map((v) => <option key={v.version} value={v.version}>v{v.version}{v.label ? ` · ${v.label}` : ""}</option>)}
          </select>
        </label>
      </div>
      {from === to && <div className="panel-empty">请选择两个不同的版本。</div>}
      {error && <div className="error">{error}</div>}
      {diff && from !== to && (
        <div>
          {diff.changes.length === 0 && <div className="panel-empty">两个版本之间引用关系没有变化。</div>}
          {(["moved", "retargeted", "confirmations", "added", "removed"] as const).map((kind) => {
            const list = groups.get(kind);
            if (!list || list.length === 0) return null;
            return (
              <div key={kind}>
                <h4>{KIND_LABEL[kind]}（{list.length}）</h4>
                <ul className="ref-list">
                  {list.map((c, i) => (
                    <li key={`${kind}-${c.from_txid}-${c.to_txid}-${c.vout}-${i}`}>
                      <button className="link-btn" onClick={() => onSelectTx(c.from_txid)}>{c.from_txid}</button>
                      {" → "}
                      {kind === "retargeted" ? (
                        <span>{c.before?.to_txid} ⇒ {c.after?.to_txid}</span>
                      ) : (
                        <button className="link-btn" onClick={() => onSelectTx(c.to_txid)}>{c.to_txid}</button>
                      )}
                      {" "}vout {c.vout}
                      <div className="ref-detail">
                        {kind === "added" && <span>新位置：{refPos(c.after)}</span>}
                        {kind === "removed" && <span>原位置：{refPos(c.before)}</span>}
                        {(kind === "moved" || kind === "confirmations") && (
                          <span>原 {refPos(c.before)} → 新 {refPos(c.after)}</span>
                        )}
                        {kind === "retargeted" && <span>花费位置：{refPos(c.after ?? c.before)}</span>}
                      </div>
                    </li>
                  ))}
                </ul>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
