import { useState } from "react";
import { fetchSample, importBlocks } from "../api";
import type { RawBlock } from "../types";

interface Props {
  onImported: (msg: string) => void;
}

// 人工区块导入：支持粘贴 JSON 或载入内置分叉样例。
// 后端做幂等去重，中断后重复提交同一批数据不会重复索引。
export function ImportPanel({ onImported }: Props) {
  const [text, setText] = useState("");
  const [label, setLabel] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function loadSample() {
    setError(null);
    const data = await fetchSample();
    setText(JSON.stringify(data.blocks, null, 2));
  }

  async function submit() {
    setBusy(true);
    setError(null);
    try {
      let blocks: RawBlock[];
      try {
        blocks = JSON.parse(text) as RawBlock[];
      } catch {
        throw new Error("JSON 解析失败，请粘贴区块数组");
      }
      if (!Array.isArray(blocks)) {
        throw new Error("顶层必须是区块数组");
      }
      const result = await importBlocks(blocks, label || `手动导入 ${new Date().toLocaleString()}`);
      onImported(`导入完成：新增 ${result.added}，跳过已索引 ${result.skipped}，当前版本 v${result.state.version}，主链端点 ${result.state.tip_hash}`);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <details className="import-panel">
      <summary>导入人工区块（JSON，支持中断后重放）</summary>
      <input className="search" placeholder="本次导入标签（可选）" value={label} onChange={(e) => setLabel(e.target.value)} />
      <textarea rows={8} value={text} onChange={(e) => setText(e.target.value)} placeholder='[{"hash":"G","prev_hash":"","work":100,"txs":[...]}]' />
      <div className="row">
        <button onClick={loadSample}>载入内置分叉样例</button>
        <button className="primary" onClick={submit} disabled={busy}>{busy ? "导入中…" : "导入并重建索引"}</button>
      </div>
      {error && <div className="error">{error}</div>}
    </details>
  );
}
