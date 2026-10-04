import type { Comparison, ImportResponse, LineMeta, RawBlock, RefDiff, Snapshot, VersionMeta } from "./types";

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

export function importBlocks(blocks: RawBlock[], label: string, lineId?: string): Promise<ImportResponse> {
  return req("/api/import", {
    method: "POST",
    body: JSON.stringify({ blocks, label, line_id: lineId || undefined }),
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

// ---- 研究线：比较、合并、撤回 ----

export function fetchLines(): Promise<{ lines: LineMeta[] }> {
  return req<{ lines: LineMeta[] }>("/api/lines").then((r) => ({ lines: r.lines ?? [] }));
}

export function createLine(name: string): Promise<{ line: LineMeta }> {
  return req("/api/lines", { method: "POST", body: JSON.stringify({ name }) });
}

export function fetchLineState(line: string, version?: number): Promise<{ snapshot: Snapshot }> {
  const q = version != null ? `?line=${line}&v=${version}` : `?line=${line}`;
  return req<{ snapshot: Snapshot }>(`/api/lines/state${q}`).then((res) => {
    const snap = res.snapshot;
    snap.blocks = (snap.blocks ?? []).map((b) => ({ ...b, txs: b.txs ?? [] }));
    snap.tx_index = snap.tx_index ?? [];
    snap.tx_refs = snap.tx_refs ?? [];
    snap.conflicts = snap.conflicts ?? [];
    return { snapshot: snap };
  });
}

export function fetchLineVersions(line: string): Promise<{ versions: VersionMeta[] }> {
  return req<{ versions: VersionMeta[] }>(`/api/lines/versions?line=${line}`).then((r) => ({
    versions: r.versions ?? [],
  }));
}

export function confirmLineCheckpoint(line: string, hash: string, note: string): Promise<{ state: Snapshot }> {
  return req("/api/lines/checkpoint", {
    method: "POST",
    body: JSON.stringify({ line, hash, note }),
  });
}

export function compareLines(a: string, b: string): Promise<{ comparison: Comparison }> {
  return req("/api/lines/compare", { method: "POST", body: JSON.stringify({ a, b }) });
}

export function mergeLines(
  comparisonId: string,
  name: string,
  keep: "a" | "b",
  accept: string[],
): Promise<{ line: LineMeta; state: Snapshot }> {
  return req("/api/lines/merge", {
    method: "POST",
    body: JSON.stringify({ comparison_id: comparisonId, name, keep, accept }),
  });
}

export function withdrawFromLine(line: string, hash: string): Promise<{ state: Snapshot }> {
  return req("/api/lines/withdraw", {
    method: "POST",
    body: JSON.stringify({ line, hash }),
  });
}
