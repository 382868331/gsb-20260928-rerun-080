import { useState } from "react";
import { fetchCompare, mergeStudies } from "../api";
import type { CompareResult, Snapshot, Study, StudySummary } from "../types";
import { RELATION_LABEL } from "../types";

interface Props {
  current: string;
  studies: StudySummary[];
  onMerged: (study: Study, state: Snapshot) => void;
  onError: (msg: string) => void;
}

// 两条研究线对比与合并：展示共有事实、兼容后续与跨越确认边界的
// 冲突；研究者勾选只接纳其中未确认的一段，选择保留哪套确认，
// 冲突部分可暂不采用（记为暂缓，不标成资料无效）。
export function ComparePanel({ current, studies, onMerged, onError }: Props) {
  const [other, setOther] = useState("");
  const [result, setResult] = useState<CompareResult | null>(null);
  const [accept, setAccept] = useState<Set<string>>(new Set());
  const [keepCp, setKeepCp] = useState<"a" | "b" | "">("a");
  const [newID, setNewID] = useState("");
  const [busy, setBusy] = useState(false);

  const candidates = studies.filter((s) => s.id !== current);

  async function runCompare(target: string) {
    setOther(target);
    setResult(null);
    if (!target) return;
    try {
      const cmp = await fetchCompare(current, target);
      setResult(cmp);
      setAccept(new Set());
      setKeepCp(cmp.checkpoint_a ? "a" : cmp.checkpoint_b ? "b" : "");
    } catch (e) {
      onError(e instanceof Error ? e.message : String(e));
    }
  }

  function toggle(hash: string) {
    const next = new Set(accept);
    if (next.has(hash)) next.delete(hash);
    else next.add(hash);
    setAccept(next);
  }

  async function doMerge() {
    if (!result) return;
    setBusy(true);
    try {
      // 未接纳的冲突区块记为暂缓：不标成资料无效，保留待后续决定。
      const deferred = [...result.only_a, ...result.only_b]
        .filter((b) => b.relation === "conflict" && !accept.has(b.hash))
        .map((b) => b.hash);
      const { study, state } = await mergeStudies({
        a: result.study_a,
        b: result.study_b,
        new_id: newID.trim() || undefined,
        accept: [...accept],
        deferred,
        keep_checkpoint: keepCp,
      });
      onMerged(study, state);
      setResult(null);
      setOther("");
      setNewID("");
    } catch (e) {
      onError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  const groups: Array<{ title: string; blocks: CompareResult["only_a"] }> = result
    ? [
        { title: `仅 ${result.study_a} 拥有`, blocks: result.only_a },
        { title: `仅 ${result.study_b} 拥有`, blocks: result.only_b },
      ]
    : [];

  return (
    <details className="import-panel">
      <summary>比较与合并研究线</summary>
      <div className="row">
        <label>
          与当前研究线（{current}）比较：
          <select value={other} onChange={(e) => void runCompare(e.target.value)}>
            <option value="">选择另一条研究线…</option>
            {candidates.map((s) => (
              <option key={s.id} value={s.id}>{s.name}（{s.id}）</option>
            ))}
          </select>
        </label>
      </div>
      {result && (
        <div>
          <h4>
            共有事实（按区块身份，{result.shared.length} 个）：
            {result.shared.map((b) => b.hash).join(", ") || "无"}
          </h4>
          {groups.map((g) => (
            <div key={g.title}>
              <h4>{g.title}（{g.blocks.length} 个，勾选以接纳）</h4>
              <table className="data-table">
                <thead>
                  <tr><th>接纳</th><th>区块</th><th>高度</th><th>相对另一线</th><th>本线范围</th></tr>
                </thead>
                <tbody>
                  {g.blocks.map((b) => (
                    <tr key={b.hash}>
                      <td>
                        <input
                          type="checkbox"
                          checked={accept.has(b.hash)}
                          onChange={() => toggle(b.hash)}
                        />
                      </td>
                      <td>{b.hash}</td>
                      <td>#{b.height}</td>
                      <td>{RELATION_LABEL[b.relation]}</td>
                      <td>{b.scope ?? "-"}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ))}
          <h4>
            各自独有的交易引用：{result.study_a} {result.refs_only_a.length} 条 ·{" "}
            {result.study_b} {result.refs_only_b.length} 条
          </h4>
          <ul>
            {[...result.refs_only_a.map((r) => ({ r, side: result.study_a })),
              ...result.refs_only_b.map((r) => ({ r, side: result.study_b }))].map(({ r, side }) => (
              <li key={`${side}-${r.from_txid}@${r.from_block_hash}->${r.to_txid}`}>
                [{side}] {r.from_txid}（块 {r.from_block_hash}）→ {r.to_txid}
                {r.resolved ? `（目标在 ${r.to_block_hash}）` : "（目标未解析）"}
              </li>
            ))}
          </ul>
          <div className="row">
            <label>
              保留哪套确认：
              <select value={keepCp} onChange={(e) => setKeepCp(e.target.value as "a" | "b" | "")}>
                <option value="">都不保留</option>
                {result.checkpoint_a && <option value="a">保留 {result.study_a} 的检查点 {result.checkpoint_a.hash}</option>}
                {result.checkpoint_b && <option value="b">保留 {result.study_b} 的检查点 {result.checkpoint_b.hash}</option>}
              </select>
            </label>
            <input
              className="search"
              placeholder="新研究线 id（留空自动生成）"
              value={newID}
              onChange={(e) => setNewID(e.target.value)}
            />
            <button className="primary" disabled={busy} onClick={() => void doMerge()}>
              {busy ? "合并中…" : "合并为新研究线"}
            </button>
          </div>
          <p className="muted">
            新线 = 两线共有 + 勾选的接纳段；缺少中间资料时保留待补充范围；
            未接纳的冲突部分记为暂缓而非无效；原有两条线保持可回看。
          </p>
        </div>
      )}
    </details>
  );
}
