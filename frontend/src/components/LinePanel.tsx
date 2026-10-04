import { useState } from "react";
import { compareLines, mergeLines } from "../api";
import type { Comparison, LineMeta, Snapshot } from "../types";

interface Props {
  lines: LineMeta[];
  currentLine: string | null;
  snapshot: Snapshot | null;
  onSelectLine: (id: string | null) => void;
  onCreateLine: (name: string) => void;
  onLineState: (snap: Snapshot, msg: string) => void;
  onError: (msg: string) => void;
}

// 研究线面板：创建/选择研究线，比较两条线（共有事实、兼容后续资料、
// 跨越确认边界的冲突、同一交易跨分叉说明），并选择只接纳兼容资料
// 合并成新研究线；冲突部分暂缓而非判为无效。
export function LinePanel({ lines, currentLine, snapshot, onSelectLine, onCreateLine, onLineState, onError }: Props) {
  const [newName, setNewName] = useState("");
  const [compareA, setCompareA] = useState("");
  const [compareB, setCompareB] = useState("");
  const [comparison, setComparison] = useState<Comparison | null>(null);
  const [keep, setKeep] = useState<"a" | "b">("a");
  const [accept, setAccept] = useState<Set<string>>(new Set());
  const [mergeName, setMergeName] = useState("");
  const [busy, setBusy] = useState(false);

  const lineName = (id: string) => lines.find((l) => l.id === id)?.name ?? id;

  async function doCompare() {
    if (!compareA || !compareB || compareA === compareB) {
      onError("请选择两条不同的研究线进行比较。");
      return;
    }
    setBusy(true);
    try {
      const { comparison: cmp } = await compareLines(compareA, compareB);
      setComparison(cmp);
      setAccept(new Set([...cmp.compatible_a, ...cmp.compatible_b]));
      setKeep("a");
      setMergeName(`合并 ${lineName(cmp.line_a)} + ${lineName(cmp.line_b)}`);
    } catch (e) {
      onError(`比较未完成（${e instanceof Error ? e.message : String(e)}）。`);
    } finally {
      setBusy(false);
    }
  }

  async function doMerge() {
    if (!comparison) return;
    setBusy(true);
    try {
      const { state } = await mergeLines(comparison.id, mergeName, keep, [...accept]);
      onLineState(
        state,
        `已合并 ${comparison.line_a} + ${comparison.line_b} 为新研究线 ${state.line?.name ?? ""}（依据 ${comparison.id}），原两线保持可回看。`,
      );
      setComparison(null);
    } catch (e) {
      onError(`合并未完成（${e instanceof Error ? e.message : String(e)}），当前视角保持不变。`);
    } finally {
      setBusy(false);
    }
  }

  function toggleAccept(hash: string) {
    setAccept((prev) => {
      const next = new Set(prev);
      if (next.has(hash)) next.delete(hash);
      else next.add(hash);
      return next;
    });
  }

  const lineView = snapshot?.line ?? null;

  return (
    <details className="import-panel" open={lines.length > 0}>
      <summary>研究线：比较与合并（各自确认、各自导入，合并可审计）</summary>

      <div className="row" style={{ alignItems: "center", flexWrap: "wrap" }}>
        <label>
          当前研究线：{" "}
          <select value={currentLine ?? ""} onChange={(e) => onSelectLine(e.target.value || null)}>
            <option value="">全部索引（默认视图）</option>
            {lines.map((l) => (
              <option key={l.id} value={l.id}>
                {l.id} · {l.name}（{l.block_count} 块{l.parent_lines?.length ? ` · 合并自 ${l.parent_lines.join("+")}` : ""}）
              </option>
            ))}
          </select>
        </label>
        <input placeholder="新研究线名称" value={newName} onChange={(e) => setNewName(e.target.value)} />
        <button
          onClick={() => {
            if (newName.trim()) {
              onCreateLine(newName.trim());
              setNewName("");
            }
          }}
        >
          创建研究线
        </button>
      </div>

      {lines.length >= 2 && (
        <div className="row" style={{ alignItems: "center", flexWrap: "wrap" }}>
          <select value={compareA} onChange={(e) => setCompareA(e.target.value)}>
            <option value="">研究线甲…</option>
            {lines.map((l) => <option key={l.id} value={l.id}>{l.id} · {l.name}</option>)}
          </select>
          <select value={compareB} onChange={(e) => setCompareB(e.target.value)}>
            <option value="">研究线乙…</option>
            {lines.map((l) => <option key={l.id} value={l.id}>{l.id} · {l.name}</option>)}
          </select>
          <button className="primary" onClick={doCompare} disabled={busy}>比较两条研究线</button>
        </div>
      )}

      {comparison && (
        <div className="compare-result">
          <h4>
            比较 {comparison.id}：{comparison.line_a}（{lineName(comparison.line_a)}） vs{" "}
            {comparison.line_b}（{lineName(comparison.line_b)}）
          </h4>
          <p className="ref-detail">
            共有事实（按区块 Hash）：{comparison.shared.length > 0 ? comparison.shared.join(", ") : "无"}
          </p>
          <p className="ref-detail">
            跨越确认边界的冲突（暂缓，不判为无效）：
            {comparison.conflicts.length === 0 ? "无" : ""}
          </p>
          <ul className="ref-list">
            {comparison.conflicts.map((c) => (
              <li key={c.hash}>
                <span className="warn-text">{c.hash}</span> 位于 {c.in_line}（{lineName(c.in_line)}），
                跨越 {c.crosses_line}（{lineName(c.crosses_line)}）已确认的检查点 {c.checkpoint}
              </li>
            ))}
          </ul>
          {comparison.tx_notes.length > 0 && (
            <>
              <h4>同一交易在两条线不同分叉里的出现</h4>
              <ul className="ref-list">
                {comparison.tx_notes.map((n) => (
                  <li key={n.txid}>
                    <strong>{n.txid}</strong>：{comparison.line_a} 中位于 {n.blocks_in_a.join(", ")}；
                    {comparison.line_b} 中位于 {n.blocks_in_b.join(", ")}
                  </li>
                ))}
              </ul>
            </>
          )}

          <h4>选择接纳的兼容后续资料（未被对方确认边界排斥）</h4>
          {comparison.compatible_a.length === 0 && comparison.compatible_b.length === 0 && (
            <p className="ref-detail">没有可接纳的兼容后续资料。</p>
          )}
          {comparison.compatible_a.map((h) => (
            <label key={h} className="accept-item">
              <input type="checkbox" checked={accept.has(h)} onChange={() => toggleAccept(h)} />
              {h} <span className="ref-detail">（来自 {comparison.line_a} · {lineName(comparison.line_a)}）</span>
            </label>
          ))}
          {comparison.compatible_b.map((h) => (
            <label key={h} className="accept-item">
              <input type="checkbox" checked={accept.has(h)} onChange={() => toggleAccept(h)} />
              {h} <span className="ref-detail">（来自 {comparison.line_b} · {lineName(comparison.line_b)}）</span>
            </label>
          ))}

          <div className="row" style={{ alignItems: "center", flexWrap: "wrap" }}>
            <label>
              保留确认：{" "}
              <select value={keep} onChange={(e) => setKeep(e.target.value as "a" | "b")}>
                <option value="a">{comparison.line_a}（{lineName(comparison.line_a)}）的确认</option>
                <option value="b">{comparison.line_b}（{lineName(comparison.line_b)}）的确认</option>
              </select>
            </label>
            <input placeholder="新研究线名称" value={mergeName} onChange={(e) => setMergeName(e.target.value)} />
            <button className="primary" onClick={doMerge} disabled={busy}>
              合并为新研究线
            </button>
          </div>
          <p className="ref-detail">
            只接纳勾选的兼容资料；缺少中间资料时会保留待补充范围，不会拼接出从未存在的链。
          </p>
        </div>
      )}

      {lineView && (
        <div className="compare-result">
          <h4>
            当前研究线 {lineView.id} · {lineView.name}
            {lineView.parent_lines?.length ? `（合并自 ${lineView.parent_lines.join(" + ")}，依据 ${lineView.base_comparison}）` : ""}
          </h4>
          {lineView.pending_gaps.length > 0 && (
            <>
              <h4>待补充范围（缺少中间资料，未拼接）</h4>
              <ul className="ref-list">
                {lineView.pending_gaps.map((g) => (
                  <li key={g.hash}>
                    <strong>{g.hash}</strong>
                    {g.missing_parents?.length ? ` 缺少父区块：${g.missing_parents.join(", ")}` : ""}
                    {g.missing_refs?.length ? ` 缺少引用目标：${g.missing_refs.join(", ")}` : ""}
                  </li>
                ))}
              </ul>
            </>
          )}
          {lineView.deferred.length > 0 && (
            <>
              <h4>暂缓的冲突部分（仍有效，待裁决）</h4>
              <ul className="ref-list">
                {lineView.deferred.map((d) => (
                  <li key={d.hash}>
                    <span className="warn-text">{d.hash}</span>（{d.comparison_id}）{d.reason}
                  </li>
                ))}
              </ul>
            </>
          )}
          {lineView.withdrawals.length > 0 && (
            <>
              <h4>已撤回的未确认资料</h4>
              <ul className="ref-list">
                {lineView.withdrawals.map((w) => (
                  <li key={w.hash}>{w.hash}（撤于 {w.withdrawn_at}）</li>
                ))}
              </ul>
            </>
          )}
          <h4>采用决定审计（每个区块来自哪次比较）</h4>
          <table className="data-table">
            <thead><tr><th>区块</th><th>来源比较</th><th>来源线</th><th>采用时间</th></tr></thead>
            <tbody>
              {Object.values(lineView.adoptions).map((a) => (
                <tr key={a.hash}>
                  <td>{a.hash}</td>
                  <td>{a.comparison_id}</td>
                  <td>{a.source_line === "both" ? "两线共有" : `${a.source_line}（${lineName(a.source_line)}）`}</td>
                  <td>{a.adopted_at}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </details>
  );
}
