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
    return { snapshot: snap };
  });
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
