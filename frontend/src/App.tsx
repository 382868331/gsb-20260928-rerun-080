import { useCallback, useEffect, useMemo, useState } from "react";
import { fetchState, fetchVersions } from "./api";
import type { ImportResponse, Snapshot, VersionMeta } from "./types";
import { ChainGraph } from "./components/ChainGraph";
import { BlockDetail } from "./components/BlockDetail";
import { TxExplorer } from "./components/TxExplorer";
import { VersionBar } from "./components/VersionBar";
import { ImportPanel } from "./components/ImportPanel";
import { RefDiffPanel } from "./components/RefDiffPanel";
import { CheckpointPanel } from "./components/CheckpointPanel";
import "./styles.css";

export default function App() {
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [versions, setVersions] = useState<VersionMeta[]>([]);
  const [version, setVersion] = useState<number | undefined>(undefined);
  const [selectedBlock, setSelectedBlock] = useState<string | null>(null);
  const [selectedTx, setSelectedTx] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [incomplete, setIncomplete] = useState<string | null>(null);

  // 快照与版本号必须一起更新，保证选择器与页面各区域始终对应同一版本。
  const applySnapshot = useCallback((snap: Snapshot) => {
    setSnapshot(snap);
    setVersion(snap.version || undefined);
    setIncomplete(null);
    setError(null);
  }, []);

  const refreshVersions = useCallback(async () => {
    try {
      const { versions: vs } = await fetchVersions();
      setVersions(vs);
    } catch {
      // 版本列表刷新失败不影响当前视角。
    }
  }, []);

  const load = useCallback(async (v?: number) => {
    setError(null);
    try {
      const { snapshot: snap } = await fetchState(v);
      applySnapshot(snap);
      const { versions: vs } = await fetchVersions();
      setVersions(vs);
    } catch (e) {
      // 切换或载入失败：保留原视角，并明确显示未完成状态。
      const reason = e instanceof Error ? e.message : String(e);
      setSnapshot((current) => {
        if (current) {
          setIncomplete(`版本切换未完成：${reason}。仍显示 v${current.version} 视角，可重试。`);
        } else {
          setError(reason);
        }
        return current;
      });
    }
  }, [applySnapshot]);

  useEffect(() => {
    void load();
  }, [load]);

  const onImported = useCallback((result: ImportResponse, msg: string) => {
    // 导入响应自带同一版本的新快照，直接应用，选择器同步指向新版本。
    applySnapshot(result.state);
    setMessage(msg);
    void refreshVersions();
  }, [applySnapshot, refreshVersions]);

  const onCheckpointChanged = useCallback((snap: Snapshot, msg: string) => {
    applySnapshot(snap);
    setMessage(msg);
    void refreshVersions();
  }, [applySnapshot, refreshVersions]);

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
      <ImportPanel onImported={onImported} />
      {message && <div className="info">{message}</div>}
      {incomplete && <div className="warn">{incomplete}</div>}
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

          <CheckpointPanel
            snapshot={snapshot}
            selectedBlock={selectedBlockView}
            onChanged={onCheckpointChanged}
            onSelectTx={setSelectedTx}
          />

          <section>
            <h2>链拓扑（蓝=当前主链，黄=被撤回旧分叉{snapshot.checkpoint ? "，绿=确认范围，红=冲突范围" : ""}）</h2>
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
            <h2>分叉切换前后引用差异</h2>
            <RefDiffPanel versions={versions} current={snapshot.version} onSelectTx={setSelectedTx} />
          </section>
          </>
          )}
        </>
      )}
    </div>
  );
}
