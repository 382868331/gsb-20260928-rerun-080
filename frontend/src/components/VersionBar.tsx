import type { VersionMeta } from "../types";

interface Props {
  versions: VersionMeta[];
  current: number;
  onSelect: (v: number) => void;
  onRefresh: () => void;
}

export function VersionBar({ versions, current, onSelect, onRefresh }: Props) {
  return (
    <div className="version-bar">
      <label>
        索引版本（页面全部数据来自同一版本）：
        <select value={current} onChange={(e) => onSelect(Number(e.target.value))}>
          {versions.length === 0 && <option value={0}>v0（空）</option>}
          {[...versions].reverse().map((v) => (
            <option key={v.version} value={v.version}>
              v{v.version} · tip {v.tip_hash} #{v.tip_height}{v.label ? ` · ${v.label}` : ""}
            </option>
          ))}
        </select>
      </label>
      <button onClick={onRefresh}>重新加载当前版本</button>
    </div>
  );
}
