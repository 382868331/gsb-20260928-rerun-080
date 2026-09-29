import { useCallback, useEffect, useMemo, useState } from "react";
import ChainGraph from "./ChainGraph";
import { fetchState, importBlocks, resetIndex, fetchVersions, VersionExpiredError } from "./api";
import { initialChain, reorgBatch, oldForkExtend } from "./demo";
import type { Snapshot, BlockView, TransactionView } from "./types";

type Sel = { kind: "block" | "tx"; hash: string } | null;

export default function App() {
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [pinnedVersion, setPinnedVersion] = useState(0); // 0 = follow current
  const [versions, setVersions] = useState<number[]>([]);
  const [currentVersion, setCurrentVersion] = useState(0);
  const [sel, setSel] = useState<Sel>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState<string | null>(null);

  const reloadVersions = useCallback(async () => {
    const v = await fetchVersions();
    setVersions(v.versions);
    setCurrentVersion(v.current);
  }, []);

  const load = useCallback(async (version = pinnedVersion) => {
    try {
      const snap = await fetchState(version);
      setSnapshot(snap);
      setError(null);
    } catch (e) {
      if (e instanceof VersionExpiredError) {
        setError(`查看的版本已过期或不存在，已切回最新版本（${currentVersion}）。`);
        setPinnedVersion(0);
        const snap = await fetchState(0);
        setSnapshot(snap);
      } else {
        setError((e as Error).message);
      }
    }
  }, [pinnedVersion, currentVersion]);

  useEffect(() => { void load(pinnedVersion); /* eslint-disable-next-line */ }, [pinnedVersion]);

  const run = useCallback(async (blocks: unknown[], label: string) => {
    setBusy(true);
    try {
      const res = await importBlocks(blocks);
      await reloadVersions();
      // Always move to the new backend version; every panel then reads it.
      setPinnedVersion(0);
      const snap = await fetchState(0);
      setSnapshot(snap);
      const parts = [
        `${label}：新导入 ${res.imported.length}`,
        `跳过 ${res.skipped.length}`,
        res.invalid.length ? `拒绝 ${res.invalid.length}` : null,
        `版本 v${res.previousVersion} → v${res.currentVersion}（${res.reason === "reorg" ? "主链分叉切换" : res.reason}）`,
      ].filter(Boolean);
      setNote(parts.join("，"));
      setError(null);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }, [reloadVersions]);

  const bootstrap = useCallback(async () => {
    setBusy(true);
    try {
      await resetIndex();
      await importBlocks(initialChain);
      await reloadVersions();
      setPinnedVersion(0);
      setSnapshot(await fetchState(0));
      setSel(null);
      setNote("已重建演示数据：G → A1 → A2（含跨分支重复交易 tx-shared）。");
      setError(null);
    } finally {
      setBusy(false);
    }
  }, [reloadVersions]);

  const blockMap = useMemo(() => {
    const m = new Map<string, BlockView>();
    snapshot?.blocks.forEach((b) => m.set(b.hash, b));
    return m;
  }, [snapshot]);

  const txMap = useMemo(() => {
    const m = new Map<string, TransactionView>();
    snapshot?.transactions.forEach((t) => m.set(t.hash, t));
    return m;
  }, [snapshot]);

  const counts = useMemo(() => {
    if (!snapshot) return null;
    return {
      canonical: snapshot.blocks.filter((b) => b.status === "canonical").length,
      forked: snapshot.blocks.filter((b) => b.status === "forked").length,
      orphan: snapshot.blocks.filter((b) => b.status === "orphan").length,
      dupTx: snapshot.transactions.filter((t) => t.duplicate).length,
    };
  }, [snapshot]);

  return (
    <div className="app">
      <header>
        <h1>链上数据研究 · 人工区块索引</h1>
        <div className="subtitle">仅索引人工提交区块，不连接任何公网节点 · 所有面板固定读取后端同一版本快照</div>
      </header>

      <div className="toolbar">
        <button className="primary" disabled={busy} onClick={() => void run(reorgBatch, "分叉批次")}>导入更重分叉（触发主链切换）</button>
        <button disabled={busy} onClick={() => void run(oldForkExtend, "旧链延伸")}>延伸旧分支 A2→A3</button>
        <button disabled={busy} onClick={() => void run(initialChain, "重复导入（去重）")}>重新提交已有批次（断点续传）</button>
        <button disabled={busy} onClick={() => void bootstrap()}>重置为演示数据</button>
        <span style={{ flex: 1 }} />
        <label className="muted">查看版本：
          <select
            value={pinnedVersion || currentVersion}
            onChange={(e) => {
              const v = Number(e.target.value);
              setSel(null);
              setPinnedVersion(v === currentVersion ? 0 : v);
            }}
          >
            {[...versions].reverse().map((v) => (
              <option key={v} value={v}>v{v}{v === currentVersion ? "（当前）" : "（历史可追溯）"}</option>
            ))}
          </select>
        </label>
      </div>

      {error && <div className="banner">{error}</div>}
      {note && <div className="banner warn">{note}</div>}

      {snapshot && (
        <div className="panel">
          <h2>链视图 v{snapshot.version} · 原因：{reasonZh(snapshot.reason)} · 主链尖端 {snapshot.tipHash || "—"}</h2>
          <div className="statline">
            <span>高度 <b>{snapshot.height}</b></span>
            <span>主链块 <b>{counts?.canonical}</b></span>
            <span>分叉块 <b>{counts?.forked}</b></span>
            <span>孤立块 <b>{counts?.orphan}</b></span>
            <span>跨块重复交易 <b>{counts?.dupTx}</b></span>
            <span className="muted">快照时间 {new Date(snapshot.createdAt).toLocaleString()}</span>
          </div>
          <ChainGraph blocks={snapshot.blocks} selected={sel?.kind === "block" ? sel.hash : null} onSelect={(h) => setSel({ kind: "block", hash: h })} />
        </div>
      )}

      <div className="grid">
        <div className="panel">
          <h2>区块（不只是列表：父子引用与分叉状态）</h2>
          <table>
            <thead><tr><th>高度</th><th>哈希</th><th>父块</th><th>状态</th><th>确认数</th><th>累计工作量</th></tr></thead>
            <tbody>
              {snapshot?.blocks.map((b) => (
                <tr key={b.hash} className={sel?.kind === "block" && sel.hash === b.hash ? "selected" : ""} onClick={() => setSel({ kind: "block", hash: b.hash })}>
                  <td>{b.height}</td>
                  <td className="mono">{b.hash}</td>
                  <td className="mono">{b.parentHash || <span className="muted">创世</span>}{!blockMap.has(b.parentHash) && b.parentHash && <span className="muted">（未连接）</span>}</td>
                  <td><StatusBadge status={b.status} /></td>
                  <td>{b.confirmations}</td>
                  <td>{b.chainWork}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        <div className="panel">
          <h2>交易（位置与确认随主链变化）</h2>
          <table>
            <thead><tr><th>交易</th><th>所在块</th><th>状态</th><th>确认数</th><th>出现次数</th></tr></thead>
            <tbody>
              {snapshot?.transactions.map((t) => (
                <tr key={t.hash} className={sel?.kind === "tx" && sel.hash === t.hash ? "selected" : ""} onClick={() => setSel({ kind: "tx", hash: t.hash })}>
                  <td className="mono">{t.hash}{t.duplicate && <div><span className="badge duplicate">跨块重复</span></div>}</td>
                  <td className="mono">{t.blockHash} <span className="muted">h{t.blockHeight}</span></td>
                  <td><StatusBadge status={t.status} /></td>
                  <td>{t.confirmations}</td>
                  <td>{t.occurrences.length}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

      {snapshot && sel && (
        <div className="panel detail">
          {sel.kind === "block" && blockMap.has(sel.hash) && <BlockDetail b={blockMap.get(sel.hash)!} txs={txMap} onTx={(h) => setSel({ kind: "tx", hash: h })} />}
          {sel.kind === "tx" && txMap.has(sel.hash) && <TxDetail t={txMap.get(sel.hash)!} blocks={blockMap} onBlock={(h) => setSel({ kind: "block", hash: h })} />}
        </div>
      )}

      <div className="panel muted">
        统计分析为后续版本预留：当前版本号 v{currentVersion}，历史版本保留最近 64 个；过期版本请求由后端返回 410。
      </div>
    </div>
  );
}

function reasonZh(r: string) {
  if (r === "reorg") return "reorg：新分支成为主链，旧结果撤回但仍可追溯";
  if (r === "initial") return "初始建链";
  return "主链延伸";
}

function StatusBadge({ status }: { status: string }) {
  const map: Record<string, string> = {
    canonical: "主链", forked: "旧分叉", orphan: "未连接",
    confirmed: "已确认",
  };
  return <span className={`badge ${status}`}>{map[status] ?? status}</span>;
}

function BlockDetail({ b, txs, onTx }: { b: BlockView; txs: Map<string, TransactionView>; onTx: (h: string) => void }) {
  return (
    <>
      <h2>区块详情 · {b.hash}（后端同一版本 v 内引用）</h2>
      <dl>
        <dt>状态 / 确认</dt><dd><StatusBadge status={b.status} /> 确认数 {b.confirmations}</dd>
        <dt>父块</dt><dd className="mono">{b.parentHash || "创世块"}</dd>
        <dt>工作量</dt><dd>本块 {b.weight}，累计 {b.chainWork}</dd>
        <dt>矿工 / 时间</dt><dd>{b.miner || "—"} · {new Date(b.timestamp).toLocaleString()}</dd>
        <dt>交易引用</dt>
        <dd>
          {b.transactionHashes.length === 0 && <span className="muted">无交易</span>}
          {b.transactionHashes.map((h, i) => {
            const t = txs.get(h);
            return (
              <span key={h} className="pill-row" style={{ marginBottom: 4 }}>
                <a className="mono" onClick={() => onTx(h)}>[{i}] {h}</a>
                {t && <StatusBadge status={t.status} />}
                {t?.duplicate && <span className="badge duplicate">重复</span>}
              </span>
            );
          })}
        </dd>
      </dl>
    </>
  );
}

function TxDetail({ t, blocks, onBlock }: { t: TransactionView; blocks: Map<string, BlockView>; onBlock: (h: string) => void }) {
  return (
    <>
      <h2>交易详情 · {t.hash}</h2>
      <dl>
        <dt>状态</dt><dd><StatusBadge status={t.status} /> 确认数 {t.confirmations}{t.duplicate && <span className="badge duplicate" style={{ marginLeft: 8 }}>跨块重复交易</span>}</dd>
        <dt>金额</dt><dd className="mono">{t.from} → {t.to} : {t.value}</dd>
        <dt>当前位置</dt><dd className="mono">{t.blockHash} @ 高度 {t.blockHeight}</dd>
        <dt>所有出现位置</dt>
        <dd>
          <ul className="occs">
            {t.occurrences.map((o) => (
              <li key={`${o.blockHash}-${o.index}`} className="mono">
                <a onClick={() => onBlock(o.blockHash)}>{o.blockHash}</a> @ 高度 {o.height} 索引 {o.index}
                {" "}<StatusBadge status={o.status} />
                {!blocks.has(o.blockHash) && <span className="muted">（该块不在当前版本）</span>}
              </li>
            ))}
          </ul>
          <div className="muted">分叉切换后，主链位置与确认状态由后端重算；旧位置可通过历史版本查看。</div>
        </dd>
      </dl>
    </>
  );
}
