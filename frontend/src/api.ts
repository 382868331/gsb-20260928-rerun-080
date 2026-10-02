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

// 空快照或缺省字段一律归一化为空数组，避免空结果导致页面崩溃。
export function normalizeSnapshot(snap: Snapshot): Snapshot {
  snap.blocks = (snap.blocks ?? []).map((b) => ({ ...b, txs: b.txs ?? [] }));
  snap.tx_index = snap.tx_index ?? [];
  snap.tx_refs = snap.tx_refs ?? [];
  snap.research_lines = snap.research_lines ?? [];
  return snap;
}

export function fetchState(version?: number): Promise<{ snapshot: Snapshot }> {
  const q = version != null ? `?v=${version}` : "";
  return req<{ snapshot: Snapshot }>(`/api/state${q}`).then((res) => ({
    snapshot: normalizeSnapshot(res.snapshot),
  }));
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
  return req<ImportResponse>("/api/import", {
    method: "POST",
    body: JSON.stringify({ blocks, label }),
  }).then((res) => ({ ...res, state: normalizeSnapshot(res.state) }));
}

export function fetchSample(): Promise<{ blocks: RawBlock[] }> {
  return req("/api/sample");
}

export function confirmCheckpoint(hash: string, note: string): Promise<{ state: Snapshot }> {
  return req<{ state: Snapshot }>("/api/checkpoint", {
    method: "POST",
    body: JSON.stringify({ hash, note }),
  }).then((res: { state: Snapshot }) => ({ state: normalizeSnapshot(res.state) }));
}

export function revokeCheckpoint(): Promise<{ state: Snapshot }> {
  return req<{ state: Snapshot }>("/api/checkpoint", { method: "DELETE" }).then((res) => ({
    state: normalizeSnapshot(res.state),
  }));
}

export function resolveCheckpoint(action: "adopt" | "branch", label: string): Promise<{ state: Snapshot }> {
  return req<{ state: Snapshot }>("/api/checkpoint/resolve", {
    method: "POST",
    body: JSON.stringify({ action, label }),
  }).then((res) => ({ state: normalizeSnapshot(res.state) }));
}
