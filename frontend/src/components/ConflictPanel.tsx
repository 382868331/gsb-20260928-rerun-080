import type { CheckpointView, Conflict } from "../types";

interface Props {
  conflicts: Conflict[];
  checkpoint: CheckpointView | null;
  onResolve: (id: string) => void;
}

// 越过检查点的替代链冲突：系统保留冲突及其引用差异，不自动撤销
// 已确认的历史范围；是否建立独立研究由研究者明确裁决。
export function ConflictPanel({ conflicts, checkpoint, onResolve }: Props) {
  return (
    <div className="conflict-panel">
      <p className="conflict-note">
        以下替代链越过了已确认的检查点{checkpoint ? `（${checkpoint.hash} @ #${checkpoint.height}）` : ""}，
        与确认范围矛盾。系统已保留冲突及其引用差异，原确认不会被自动撤销；
        检查点之后的分叉变化仍可正常更新。
      </p>
      <ul className="ref-list">
        {conflicts.map((c) => (
          <li key={c.id}>
            <div>
              <strong>{c.id}</strong>{" "}
              <span className={c.status === "pending" ? "warn-text" : "ok-text"}>
                {c.status === "pending" ? "待裁决" : "已建立独立研究"}
              </span>
            </div>
            <div className="ref-detail">
              替代链端点 {c.rival_tip} @ #{c.rival_height} · 链上累计工作量 {c.rival_work}
            </div>
            {c.status === "pending" && (
              <div className="row">
                <button onClick={() => onResolve(c.id)}>为该替代链建立独立研究</button>
              </div>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}

