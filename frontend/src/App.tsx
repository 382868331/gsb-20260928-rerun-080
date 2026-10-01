import { useCallback, useEffect, useMemo, useState } from "react";
import { fetchState, fetchVersions } from "./api";
import type { Snapshot, VersionMeta } from "./types";
import { ChainGraph } from "./components/ChainGraph";
import { BlockDetail } from "./components/BlockDetail";
import { TxExplorer } from "./components/TxExplorer";
import { VersionBar } from "./components/VersionBar";
import { ImportPanel } from "./components/ImportPanel";
import { RefDiffPanel } from "./components/RefDiffPanel";
import "./styles.css";

export default function App() {
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [versions, setVersions] = useState<VersionMeta[]>([]);
  const [version, setVersion] = useState<number | undefined>(undefined);
  const [selectedBlock, setSelectedBlock] = useState<string | null>(null);
  const [selectedTx, setSelectedTx] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async (v?: number) => {
    setError(null);
    try {
      const { snapshot: snap } = await fetchState(v);
      setSnapshot(snap);
      setVersion(snap.version || undefined);
      const { versions: vs } = await fetchVersions();
      setVersions(vs);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const selectedBlockView = useMemo(
    () => snapshot?.blocks.find((b) => b.hash === selectedBlock) ?? null,
    [snapshot, selectedBlock],
  );

  const canonicalCount = snapshot?.blocks.filter((b) => b.canonical).length ?? 0;
  const forkCount = (snapshot?.blocks.length ?? 0) - canonicalCount;

  return (
    <div className="app">
      <header>
        <h1>链上数据研究台</h1>
        <p className="subtitle">人工区块索引 · 主链分叉撤回追溯 · 不连接任何公网节点</p>
      </header>

      <VersionBar versions={versions} current={version ?? 0} onSelect={(v) => void load(v === 0 ? undefined : v)} onRefresh={() => void load(version)} />
      <ImportPanel onImported={(msg) => { setMessage(msg); void load(); }} />
      {message && <div className="info">{message}</div>}
      {error && <div className="error">{error}</div>}

      {!snapshot && !error && <div className="panel-empty">正在加载索引快照…</div>}

      {snapshot && (
        <>
          {snapshot.blocks.length === 0 ? (
            <div className="panel-empty">
              当前没有索引版本：尚未导入任何人工区块。展开“导入人工区块”载入内置分叉样例或粘贴区块 JSON 后即可浏览。
            </div>
          ) : (
          <>
          <section className="summary">
            <div>版本 <strong>v{snapshot.version}</strong>{snapshot.label ? ` · ${snapshot.label}` : ""}</div>
            <div>主链端点 <strong>{snapshot.tip_hash || "-"}</strong> @ #{snapshot.tip_height}</div>
            <div>主链区块 <strong>{canonicalCount}</strong></div>
            <div>已撤回分叉区块 <strong>{forkCount}</strong>（仍可追溯）</div>
            <div>交易位置记录 <strong>{snapshot.tx_index.length}</strong></div>
            <div>交易引用 <strong>{snapshot.tx_refs.length}</strong></div>
          </section>

          <section>
            <h2>链拓扑（蓝=当前主链，黄=被撤回旧分叉）</h2>
            <ChainGraph blocks={snapshot.blocks} selectedBlock={selectedBlock} onSelect={setSelectedBlock} />
          </section>

          <section className="grid-2">
            <div>
              <h2>区块详情</h2>
              <BlockDetail block={selectedBlockView} onSelectTx={setSelectedTx} />
            </div>
            <div>
              <h2>交易索引与引用</h2>
              <TxExplorer snapshot={snapshot} selectedTx={selectedTx} onSelectTx={setSelectedTx} onSelectBlock={setSelectedBlock} />
            </div>
          </section>

          <section>
            <h2>引用差异（分叉切换前后版本对比）</h2>
            <div className="ref-diff-panel">
              <RefDiffPanel versions={versions} current={snapshot.version} onSelectTx={setSelectedTx} onSelectBlock={setSelectedBlock} />
            </div>
          </section>
          </>
          )}
        </>
      )}
    </div>
  );
}
