import { useState } from "react";
import { confirmCheckpoint, resolveCheckpoint, revokeCheckpoint } from "../api";
import type { BlockView, Snapshot } from "../types";

interface Props {
  snapshot: Snapshot;
  selectedBlock: BlockView | null;
  onChanged: (snap: Snapshot, msg: string) => void;
  onSelectTx: (txid: string) => void;
}

const KIND_LABEL: Record<string, string> = {
  position_migrated: "位置迁移",
  target_changed: "目标变化",
  confirmations_only: "仅确认数变化",
  added: "新增引用",
  removed: "移除引用",
};

// 检查点面板：展示研究者确认的检查点、确认依据、越过检查点的冲突
// 及独立研究线。冲突裁决（采纳竞争链 / 建立独立研究）必须由研究者
// 显式触发，系统不会自动撤销确认。
export function CheckpointPanel({ snapshot, selectedBlock, onChanged, onSelectTx }: Props) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const cp = snapshot.checkpoint ?? null;
  const lines = snapshot.research_lines ?? [];

  async function run(action: () => Promise<{ state: Snapshot }>, ok: (v: number) => string) {
    setBusy(true);
    setError(null);
    try {
      const { state } = await action();
      onChanged(state, ok(state.version));
    } catch (e) {
      setError(`${e instanceof Error ? e.message : String(e)}（操作未完成，当前视角不变）`);
    } finally {
      setBusy(false);
    }
  }

  const confirmSelected = () => {
    if (!selectedBlock) return;
    void run(
      () => confirmCheckpoint(selectedBlock.hash, ""),
      (v) => `已确认检查点 ${selectedBlock.hash} @ #${selectedBlock.height}，当前版本 v${v}`,
    );
  };

  const conflict = cp?.conflict ?? null;
  const refEntries = conflict?.ref_changes?.entries ?? [];

  return (
    <section className="checkpoint-panel">
      <h2>链检查点（研究者确认）</h2>
      {!cp && (
        <div className="panel-empty">
          尚未确认检查点。在链拓扑中选择一个主链区块后，在“区块详情”中点击“确认为检查点”，
          该区块及以前的历史即成为确认范围；之后试图改写这段历史的更重分叉会被保留为冲突，等待你裁决。
        </div>
      )}
      {cp && (
        <div className="checkpoint-body">
          <div className="checkpoint-status-row">
            <span className={`status-badge ${cp.status === "conflict" ? "conflict" : "confirmed"}`}>
              {cp.status === "conflict" ? "存在越过检查点的竞争链（待裁决）" : "检查点有效"}
            </span>
            <span>
              检查点 <strong>{cp.block_hash}</strong> @ #{cp.height} · 确认于 {cp.confirmed_at}
              {cp.note ? ` · ${cp.note}` : ""}
            </span>
          </div>
          <div className="checkpoint-basis">
            确认依据：版本 v{cp.basis.version} · 当时端点 {cp.basis.tip_hash} #{cp.basis.tip_height} ·
            已确认链段 {cp.basis.chain.join(" → ")}（{cp.basis.chain.length} 块，重开后仍可追踪）
          </div>
          {conflict && (
            <div className="conflict-box">
              <p>
                <strong>冲突：</strong>新资料提出更重的竞争链 <strong>{conflict.rival_tip_hash}</strong> @ #
                {conflict.rival_tip_height}（累计工作量 {conflict.rival_work}），在高度 {conflict.diverge_height}{" "}
                处分叉，试图改写检查点之前的已确认历史。系统已保留原确认，主链视角不变；请决定是否采纳，
                或将其登记为独立研究线。
              </p>
              {refEntries.length > 0 && (
                <details>
                  <summary>两种视角下的引用差异（{refEntries.length} 条）</summary>
                  <ul className="ref-list">
                    {refEntries.map((e, i) => (
                      <li key={i}>
                        <span className="dup-tag">{KIND_LABEL[e.kind] ?? e.kind}</span>{" "}
                        <button className="link-btn" onClick={() => onSelectTx((e.before ?? e.after)!.from_txid)}>
                          {(e.before ?? e.after)!.from_txid}
                        </button>
                        {" → "}
                        <button className="link-btn" onClick={() => onSelectTx((e.before ?? e.after)!.to_txid)}>
                          {(e.before ?? e.after)!.to_txid}
                        </button>
                        {e.before && e.after && e.before.from_block_hash !== e.after.from_block_hash && (
                          <span>
                            {" "}位置 {e.before.from_block_hash} → {e.after.from_block_hash}
                          </span>
                        )}
                      </li>
                    ))}
                  </ul>
                </details>
              )}
              <div className="row">
                <button
                  className="danger"
                  disabled={busy}
                  onClick={() =>
                    void run(
                      () => resolveCheckpoint("adopt", ""),
                      (v) => `已采纳竞争链 ${conflict.rival_tip_hash} 并撤销原检查点确认，当前版本 v${v}`,
                    )
                  }
                >
                  采纳竞争链（撤销原确认）
                </button>
                <button
                  disabled={busy}
                  onClick={() =>
                    void run(
                      () => resolveCheckpoint("branch", ""),
                      (v) => `已保留原确认，竞争链 ${conflict.rival_tip_hash} 登记为独立研究线，当前版本 v${v}`,
                    )
                  }
                >
                  保留确认，建立独立研究线
                </button>
              </div>
            </div>
          )}
          <div className="row">
            {selectedBlock?.canonical && selectedBlock.hash !== cp.block_hash && (
              <button disabled={busy} onClick={confirmSelected}>
                把选中区块 {selectedBlock.hash} 确认为新检查点
              </button>
            )}
            <button
              className="danger"
              disabled={busy}
              onClick={() => void run(revokeCheckpoint, (v) => `已撤销检查点确认，当前版本 v${v}`)}
            >
              撤销检查点确认
            </button>
          </div>
        </div>
      )}
      {!cp && selectedBlock?.canonical && (
        <div className="row">
          <button disabled={busy} onClick={confirmSelected}>
            确认选中区块 {selectedBlock.hash} 为检查点
          </button>
        </div>
      )}
      {lines.length > 0 && (
        <div className="research-lines">
          <h4>独立研究线（{lines.length}）</h4>
          <ul>
            {lines.map((l) => (
              <li key={l.id}>
                <strong>{l.label}</strong> · 端点 {l.tip_hash} @ #{l.tip_height} · 登记于 {l.created_at}
              </li>
            ))}
          </ul>
        </div>
      )}
      {error && <div className="error">{error}</div>}
    </section>
  );
}

