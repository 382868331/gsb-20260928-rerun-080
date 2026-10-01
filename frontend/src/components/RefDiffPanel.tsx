import { useEffect, useMemo, useState } from "react";
import { fetchRefDiff } from "../api";
import type { RefDiff, RefDiffEntry, RefDiffKind, TxRef, VersionMeta } from "../types";

interface Props {
  versions: VersionMeta[];
  current: number;
  onSelectTx: (txid: string) => void;
}

const KIND_LABEL: Record<RefDiffKind, string> = {
  position_migrated: "位置迁移",
  target_changed: "引用目标变化",
  confirmations_only: "仅确认数变化",
  added: "新增引用",
  removed: "消失引用",
};

const KIND_ORDER: RefDiffKind[] = [
  "position_migrated",
  "target_changed",
  "confirmations_only",
  "added",
  "removed",
];

function refSummary(r: TxRef): string {
  const target = r.resolved ? `${r.to_txid}@${r.to_block_hash}` : `${r.to_txid}(未解析)`;
  return `${r.from_txid}@${r.from_block_hash}(${r.from_branch_id}) → ${target}`;
}

function EntryLine({ entry, onSelectTx }: { entry: RefDiffEntry; onSelectTx: (id: string) => void }) {
  const before = entry.before;
  const after = entry.after;
  return (
    <li>
      {before && after ? (
        <>
          <button className="link-btn" onClick={() => onSelectTx(before.from_txid)}>{before.from_txid}</button>
          <div className="ref-detail">前：{refSummary(before)} · {before.from_confirmations} 确认</div>
          <div className="ref-detail">后：{refSummary(after)} · {after.from_confirmations} 确认</div>
        </>
      ) : (
        (() => {
          const r = (before ?? after)!;
          return (
            <>
              <button className="link-btn" onClick={() => onSelectTx(r.from_txid)}>{r.from_txid}</button>
              <span className="ref-detail"> {refSummary(r)}</span>
            </>
          );
        })()
      )}
    </li>
  );
}

// 分叉切换前后的引用差异查看：任选两个索引版本对比，
// 区分位置迁移、引用目标变化与仅确认数变化。
export function RefDiffPanel({ versions, current, onSelectTx }: Props) {
  const sorted = useMemo(() => [...versions].sort((a, b) => a.version - b.version), [versions]);
  const [from, setFrom] = useState<number | null>(null);
  const [to, setTo] = useState<number | null>(null);
  const [diff, setDiff] = useState<RefDiff | null>(null);
  const [error, setError] = useState<string | null>(null);

  const fromVersion = from ?? sorted[0]?.version ?? null;
  const toVersion = to ?? (current || sorted[sorted.length - 1]?.version || null);

  useEffect(() => {
    setDiff(null);
    setError(null);
    if (fromVersion == null || toVersion == null || sorted.length < 2) {
      return;
    }
    let cancelled = false;
    fetchRefDiff(fromVersion, toVersion)
      .then((d) => {
        if (!cancelled) setDiff(d);
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      });
    return () => {
      cancelled = true;
    };
  }, [fromVersion, toVersion, sorted.length]);

  if (sorted.length < 2) {
    return (
      <div className="panel-empty">至少两个索引版本才能查看引用差异（导入分叉数据后再试）。</div>
    );
  }

  const groups = new Map<RefDiffKind, RefDiffEntry[]>();
  for (const e of diff?.entries ?? []) {
    const list = groups.get(e.kind) ?? [];
    list.push(e);
    groups.set(e.kind, list);
  }

  return (
    <div className="ref-diff">
      <div className="row">
        <label>
          对比版本
          <select value={fromVersion ?? ""} onChange={(e) => setFrom(Number(e.target.value))}>
            {sorted.map((v) => (
              <option key={v.version} value={v.version}>v{v.version}{v.label ? ` · ${v.label}` : ""}</option>
            ))}
          </select>
        </label>
        <span>→</span>
        <select value={toVersion ?? ""} onChange={(e) => setTo(Number(e.target.value))}>
          {sorted.map((v) => (
            <option key={v.version} value={v.version}>v{v.version}{v.label ? ` · ${v.label}` : ""}</option>
          ))}
        </select>
      </div>
      {error && <div className="error">{error}</div>}
      {diff && diff.entries.length === 0 && (
        <div className="panel-empty">v{diff.from_version} 与 v{diff.to_version} 之间引用关系无变化。</div>
      )}
      {diff &&
        KIND_ORDER.filter((k) => groups.has(k)).map((kind) => (
          <div key={kind} className="diff-group">
            <h4>
              {KIND_LABEL[kind]}（{groups.get(kind)!.length}）
            </h4>
            <ul className="ref-list">
              {groups.get(kind)!.map((e, i) => (
                <EntryLine key={i} entry={e} onSelectTx={onSelectTx} />
              ))}
            </ul>
          </div>
        ))}
    </div>
  );
}
