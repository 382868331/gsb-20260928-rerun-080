import type { ImportResponse, RawBlock, Snapshot, VersionMeta } from "./types";

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
  return req<{ snapshot: Snapshot }>(`/api/state${q}`).then((data) => ({
    snapshot: normalizeSnapshot(data.snapshot),
  }));
}

// 兼容旧版本落盘数据：null 数组统一归一为空数组，
// 保证渲染层永远拿到可遍历的快照，不会因空结果白屏。
function normalizeSnapshot(snap: Snapshot): Snapshot {
  return {
    ...snap,
    blocks: (snap.blocks ?? []).map((b) => ({ ...b, txs: b.txs ?? [] })),
    tx_index: snap.tx_index ?? [],
    tx_refs: snap.tx_refs ?? [],
  };
}

export function fetchVersions(): Promise<{ versions: VersionMeta[] }> {
  return req("/api/versions");
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
