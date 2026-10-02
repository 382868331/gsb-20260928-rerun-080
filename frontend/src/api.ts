import type { ImportResponse, RawBlock, RefDiff, Snapshot, VersionMeta } from "./types";

async function req<T>(url: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(url, {
    headers: { "Content-Type": "application/json" },
    ...init,
  });
  if (!resp.ok) {
    throw new Error(`${resp.status}: ${await resp.text()}`);
  }
  return resp.json() as Promise<T>;
}

export function fetchState(version?: number): Promise<{ snapshot: Snapshot }> {
  const q = version != null ? `?v=${version}` : "";
  return req<{ snapshot: Snapshot }>(`/api/state${q}`).then((res) => {
    const snap = res.snapshot;
    // 空快照或缺省字段一律归一化为空数组，避免空结果导致页面崩溃。
    snap.blocks = (snap.blocks ?? []).map((b) => ({ ...b, txs: b.txs ?? [] }));
    snap.tx_index = snap.tx_index ?? [];
    snap.tx_refs = snap.tx_refs ?? [];
    snap.conflicts = snap.conflicts ?? [];
    return { snapshot: snap };
  });
}

// 导入、确认检查点、裁决冲突的响应都直接携带最新完整快照，
// 前端以此为准同步各区域与版本选择器，避免主体与选择器版本脱节。
export function normalizeSnapshot(snap: Snapshot): Snapshot {
  snap.blocks = (snap.blocks ?? []).map((b) => ({ ...b, txs: b.txs ?? [] }));
  snap.tx_index = snap.tx_index ?? [];
  snap.tx_refs = snap.tx_refs ?? [];
  snap.conflicts = snap.conflicts ?? [];
  return snap;
}

export function fetchVersions(): Promise<{ versions: VersionMeta[] }> {
  return req("/api/versions");
}

export function fetchRefDiff(from: number, to: number): Promise<RefDiff> {
  return req<RefDiff>(`/api/refdiff?from=${from}&to=${to}`).then((d) => ({
    ...d,
    entries: d.entries ?? [],
  }));
}

export function importBlocks(blocks: RawBlock[], label: string): Promise<ImportResponse> {
  return req("/api/import", {
    method: "POST",
    body: JSON.stringify({ blocks, label }),
  });
}

export function fetchSample(): Promise<{ blocks: RawBlock[] }> {
  return req("/api/sample");
}

export function confirmCheckpoint(hash: string, note: string): Promise<{ state: Snapshot }> {
  return req("/api/checkpoint", {
    method: "POST",
    body: JSON.stringify({ hash, note }),
  });
}

export function resolveConflict(id: string): Promise<{ state: Snapshot }> {
  return req("/api/conflicts/resolve", {
    method: "POST",
    body: JSON.stringify({ id, action: "independent" }),
  });
}
