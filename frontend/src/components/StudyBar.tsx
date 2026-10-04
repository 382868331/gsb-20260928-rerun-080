import { useState } from "react";
import { createStudy } from "../api";
import type { StudySummary } from "../types";

interface Props {
  studies: StudySummary[];
  current: string;
  onSelect: (id: string) => void;
  onCreated: () => void;
  onError: (msg: string) => void;
}

// 研究线选择器：页面的链、检查点、交易详情与版本选择
// 都共同指向这里选中的当前研究线。
export function StudyBar({ studies, current, onSelect, onCreated, onError }: Props) {
  const [newID, setNewID] = useState("");
  const [newName, setNewName] = useState("");
  const currentMeta = studies.find((s) => s.id === current);

  async function submit() {
    if (!newID.trim()) return;
    try {
      await createStudy(newID.trim(), newName.trim() || newID.trim());
      setNewID("");
      setNewName("");
      onCreated();
    } catch (e) {
      onError(e instanceof Error ? e.message : String(e));
    }
  }

  return (
    <div className="version-bar">
      <label>
        当前研究线：
        <select value={current} onChange={(e) => onSelect(e.target.value)}>
          {studies.map((s) => (
            <option key={s.id} value={s.id}>
              {s.name}（{s.id}）· tip {s.tip_hash || "-"} #{s.tip_height} · {s.blocks} 块
            </option>
          ))}
          {studies.length === 0 && <option value="main">默认研究线（main）</option>}
        </select>
      </label>
      <input
        className="search"
        placeholder="新研究线 id"
        value={newID}
        onChange={(e) => setNewID(e.target.value)}
      />
      <input
        className="search"
        placeholder="名称（可选）"
        value={newName}
        onChange={(e) => setNewName(e.target.value)}
      />
      <button onClick={() => void submit()}>建立独立研究线</button>
      {currentMeta?.origin && (
        <span className="muted">
          来源：{currentMeta.origin}
          {currentMeta.parents?.length ? `（${currentMeta.parents.join(" + ")}）` : ""}
        </span>
      )}
    </div>
  );
}
