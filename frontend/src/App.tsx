import { useCallback, useEffect, useMemo, useState } from "react";
import {
  confirmCheckpoint,
  confirmLineCheckpoint,
  createLine,
  fetchLines,
  fetchLineState,
  fetchLineVersions,
  fetchState,
  fetchVersions,
  normalizeSnapshot,
  resolveConflict,
  withdrawFromLine,
} from "./api";
import type { ImportResponse, LineMeta, Snapshot, VersionMeta } from "./types";
import { ChainGraph } from "./components/ChainGraph";
import { BlockDetail } from "./components/BlockDetail";
import { TxExplorer } from "./components/TxExplorer";
import { VersionBar } from "./components/VersionBar";
import { ImportPanel } from "./components/ImportPanel";
import { RefDiffPanel } from "./components/RefDiffPanel";
import { ConflictPanel } from "./components/ConflictPanel";
import { LinePanel } from "./components/LinePanel";
import "./styles.css";

export default function App() {
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [versions, setVersions] = useState<VersionMeta[]>([]);
  const [version, setVersion] = useState<number | undefined>(undefined);
  // currentLine 非空时，链、检查点、交易详情与版本选择器共同指向该研究线。
  const [currentLine, setCurrentLine] = useState<string | null>(null);
  const [lines, setLines] = useState<LineMeta[]>([]);
  const [selectedBlock, setSelectedBlock] = useState<string | null>(null);
  const [selectedTx, setSelectedTx] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  // incomplete 非空表示上一次切换/载入未完成：页面保留原视角，
  // 顶部选择器与各区域仍对应旧版本，并明确提示未完成状态。
  const [incomplete, setIncomplete] = useState<string | null>(null);

  const refreshLines = useCallback(async () => {
    try {
      const { lines: ls } = await fetchLines();
      setLines(ls);
    } catch {
      // 研究线列表刷新失败不打断主视角。
    }
  }, []);

  const refreshVersions = useCallback(async () => {
    try {
      const { versions: vs } = currentLine ? await fetchLineVersions(currentLine) : await fetchVersions();
      setVersions(vs);
    } catch {
      // 版本列表刷新失败不打断主视角。
    }
  }, [currentLine]);

  // 切换/载入版本：成功后页面各区域与选择器共同对应该版本；
  // 失败时保留原视角并提示未完成。
  const load = useCallback(async (v?: number) => {
    try {
      const { snapshot: snap } = currentLine ? await fetchLineState(currentLine, v) : await fetchState(v);
      setSnapshot(snap);
      setVersion(snap.version || undefined);
      setIncomplete(null);
      void refreshVersions();
    } catch (e) {
      const reason = e instanceof Error ? e.message : String(e);
      setIncomplete(
        v != null
          ? `切换到 v${v} 未完成（${reason}），当前仍显示 ${snapshot ? `v${snapshot.version}` : "空索引"}。`
          : `载入最新索引未完成（${reason}）。`,
      );
    }
  }, [refreshVersions, snapshot, currentLine]);

  useEffect(() => {
    void load();
    void refreshLines();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // 切换研究线：页面各区域与版本选择器一起改指该线的最新版本。
  const switchLine = useCallback((id: string | null) => {
    setCurrentLine(id);
    setSelectedBlock(null);
    setSelectedTx(null);
    setMessage(null);
    setIncomplete(null);
    (async () => {
      try {
        const { snapshot: snap } = id ? await fetchLineState(id) : await fetchState();
        setSnapshot(snap);
        setVersion(snap.version || undefined);
        const { versions: vs } = id ? await fetchLineVersions(id) : await fetchVersions();
        setVersions(vs);
      } catch (e) {
        const reason = e instanceof Error ? e.message : String(e);
        setIncomplete(`切换研究线未完成（${reason}），当前视角保持不变。`);
        setCurrentLine(null);
      }
    })();
  }, []);

  // 导入/检查点/裁决的响应直接携带最新快照：各区域与顶部选择器
  // 一起切换到该版本，避免主体已更新而选择器仍指向旧版本。
  const applySnapshot = useCallback((snap: Snapshot, msg: string) => {
    const normalized = normalizeSnapshot(snap);
    setSnapshot(normalized);
    setVersion(normalized.version || undefined);
    setIncomplete(null);
    setMessage(msg);
    void refreshVersions();
  }, [refreshVersions]);

  const onImported = useCallback((result: ImportResponse) => {
    // 导入到研究线时响应同时携带线快照：各区域与选择器一起指向线版本。
    if (currentLine && result.line_state) {
      applySnapshot(
        result.line_state,
        `导入完成：新增 ${result.added}，跳过已索引 ${result.skipped}，当前研究线版本 v${result.line_state.version}。`,
      );
      return;
    }
    const state = result.state;
    applySnapshot(
      state,
      `导入完成：新增 ${result.added}，跳过已索引 ${result.skipped}，当前版本 v${state.version}，主链端点 ${state.tip_hash}`,
    );
  }, [applySnapshot, currentLine]);

  const onConfirmCheckpoint = useCallback(async (hash: string) => {
    try {
      if (currentLine) {
        const { state } = await confirmLineCheckpoint(currentLine, hash, "");
        applySnapshot(state, `已在研究线 ${currentLine} 确认检查点 ${hash}，线版本 v${state.version}。`);
      } else {
        const { state } = await confirmCheckpoint(hash, "");
        applySnapshot(state, `已确认检查点 ${hash}：该区块及之前进入确认范围，当前版本 v${state.version}。`);
      }
    } catch (e) {
      setIncomplete(`确认检查点未完成（${e instanceof Error ? e.message : String(e)}），当前视角保持不变。`);
    }
  }, [applySnapshot, currentLine]);

  const onCreateLine = useCallback(async (name: string) => {
    try {
      const { line } = await createLine(name);
      setMessage(`研究线 ${line.id}（${line.name}）已创建，导入区块后即可确认检查点。`);
      void refreshLines();
    } catch (e) {
      setIncomplete(`创建研究线未完成（${e instanceof Error ? e.message : String(e)}）。`);
    }
  }, [refreshLines]);

  const onLineState = useCallback((snap: Snapshot, msg: string) => {
    applySnapshot(snap, msg);
    if (snap.line_id) {
      setCurrentLine(snap.line_id);
    }
    void refreshLines();
  }, [applySnapshot, refreshLines]);

  const onWithdraw = useCallback(async (hash: string) => {
    if (!currentLine) return;
    try {
      const { state } = await withdrawFromLine(currentLine, hash);
      applySnapshot(state, `已从研究线 ${currentLine} 撤回未确认资料 ${hash}，线版本 v${state.version}。`);
    } catch (e) {
      setIncomplete(`撤回未完成（${e instanceof Error ? e.message : String(e)}），当前视角保持不变。`);
    }
  }, [applySnapshot, currentLine]);

  const onResolveConflict = useCallback(async (id: string) => {
    try {
      const { state } = await resolveConflict(id);
      applySnapshot(state, `已为冲突 ${id} 建立独立研究，原确认范围保持不变，当前版本 v${state.version}。`);
    } catch (e) {
      setIncomplete(`冲突裁决未完成（${e instanceof Error ? e.message : String(e)}），当前视角保持不变。`);
    }
  }, [applySnapshot]);

  const selectedBlockView = useMemo(
    () => snapshot?.blocks.find((b) => b.hash === selectedBlock) ?? null,
    [snapshot, selectedBlock],
  );

  const canonicalCount = snapshot?.blocks.filter((b) => b.canonical).length ?? 0;
  const forkCount = (snapshot?.blocks.length ?? 0) - canonicalCount;
  const pendingConflicts = snapshot?.conflicts.filter((c) => c.status === "pending") ?? [];

  return (
    <div className="app">
      <header>
        <h1>链上数据研究台</h1>
        <p className="subtitle">人工区块索引 · 主链分叉撤回追溯 · 检查点确认 · 不连接任何公网节点</p>
      </header>

      <VersionBar versions={versions} current={version ?? 0} onSelect={(v) => void load(v === 0 ? undefined : v)} onRefresh={() => void load(version)} />
      <ImportPanel onImported={onImported} lineId={currentLine ?? undefined} />
      <LinePanel
        lines={lines}
        currentLine={currentLine}
        snapshot={snapshot}
        onSelectLine={switchLine}
        onCreateLine={(name) => void onCreateLine(name)}
        onLineState={onLineState}
        onError={setIncomplete}
      />
      {message && <div className="info">{message}</div>}
      {incomplete && <div className="incomplete">{incomplete}</div>}

      {!snapshot && !incomplete && <div className="panel-empty">正在加载索引快照…</div>}

      {snapshot && (
        <>
          {snapshot.blocks.length === 0 ? (
            <div className="panel-empty">
              当前没有索引版本：尚未导入任何人工区块。展开“导入人工区块”载入内置分叉样例或粘贴区块 JSON 后即可浏览。
            </div>
          ) : (
          <>
          <section className="summary">
            <div>
              {snapshot.line ? `研究线 ${snapshot.line.id} · ${snapshot.line.name} · ` : ""}
              版本 <strong>v{snapshot.version}</strong>{snapshot.label ? ` · ${snapshot.label}` : ""}
            </div>
            <div>主链端点 <strong>{snapshot.tip_hash || "-"}</strong> @ #{snapshot.tip_height}</div>
            <div>主链区块 <strong>{canonicalCount}</strong></div>
            <div>已撤回分叉区块 <strong>{forkCount}</strong>（仍可追溯）</div>
            <div>交易位置记录 <strong>{snapshot.tx_index.length}</strong></div>
            <div>交易引用 <strong>{snapshot.tx_refs.length}</strong></div>
            <div>
              检查点{" "}
              {snapshot.checkpoint ? (
                <strong>{snapshot.checkpoint.hash} @ #{snapshot.checkpoint.height}（此前为确认范围）</strong>
              ) : (
                "尚未确认"
              )}
            </div>
            <div>待裁决冲突 <strong>{pendingConflicts.length}</strong></div>
            {snapshot.line && (
              <>
                <div>待补充范围 <strong>{snapshot.line.pending_gaps.length}</strong></div>
                <div>暂缓冲突 <strong>{snapshot.line.deferred.length}</strong></div>
                <div>已撤回资料 <strong>{snapshot.line.withdrawals.length}</strong></div>
              </>
            )}
          </section>

          {snapshot.conflicts.length > 0 && (
            <section>
              <h2>越过检查点的替代链冲突</h2>
              <ConflictPanel conflicts={snapshot.conflicts} checkpoint={snapshot.checkpoint ?? null} onResolve={onResolveConflict} />
            </section>
          )}

          <section>
            <h2>链拓扑（蓝=暂定主链，绿=确认范围，黄=被撤回旧分叉，红=冲突范围）</h2>
            <ChainGraph blocks={snapshot.blocks} selectedBlock={selectedBlock} onSelect={setSelectedBlock} />
          </section>

          <section className="grid-2">
            <div>
              <h2>区块详情</h2>
              <BlockDetail
                block={selectedBlockView}
                checkpoint={snapshot.checkpoint ?? null}
                onSelectTx={setSelectedTx}
                onConfirmCheckpoint={onConfirmCheckpoint}
                onWithdraw={currentLine ? onWithdraw : undefined}
              />
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
