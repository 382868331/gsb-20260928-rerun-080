import type { CompareResult, ImportResponse, RawBlock, RefDiff, Snapshot, Study, StudySummary, VersionMeta } from "./types";

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

export function fetchState(study: string, version?: number): Promise<{ snapshot: Snapshot }> {
  const params = new URLSearchParams({ study });
  if (version != null) params.set("v", String(version));
  return req<{ snapshot: Snapshot }>(`/api/state?${params}`).then((res) => {
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

export function fetchVersions(study: string): Promise<{ versions: VersionMeta[] }> {
  return req(`/api/versions?study=${encodeURIComponent(study)}`);
}

export function fetchRefDiff(study: string, from: number, to: number): Promise<RefDiff> {
  return req<RefDiff>(`/api/refdiff?study=${encodeURIComponent(study)}&from=${from}&to=${to}`).then((d) => ({
    ...d,
    entries: d.entries ?? [],
  }));
}

export function importBlocks(study: string, blocks: RawBlock[], label: string): Promise<ImportResponse> {
  return req("/api/import", {
    method: "POST",
    body: JSON.stringify({ blocks, label, study }),
  });
}

export function fetchSample(): Promise<{ blocks: RawBlock[] }> {
  return req("/api/sample");
}

export function confirmCheckpoint(study: string, hash: string, note: string): Promise<{ state: Snapshot }> {
  return req("/api/checkpoint", {
    method: "POST",
    body: JSON.stringify({ hash, note, study }),
  });
}

export function resolveConflict(study: string, id: string): Promise<{ state: Snapshot }> {
  return req("/api/conflicts/resolve", {
    method: "POST",
    body: JSON.stringify({ id, action: "independent", study }),
  });
}

export function fetchStudies(): Promise<{ studies: StudySummary[] }> {
  return req("/api/studies").then((res) => ({ studies: res.studies ?? [] }));
}

export function createStudy(id: string, name: string): Promise<{ study: Study }> {
  return req("/api/studies", {
    method: "POST",
    body: JSON.stringify({ id, name }),
  });
}

export function fetchCompare(a: string, b: string): Promise<CompareResult> {
  return req<CompareResult>(`/api/compare?a=${encodeURIComponent(a)}&b=${encodeURIComponent(b)}`).then((res) => ({
    ...res,
    shared: res.shared ?? [],
    only_a: res.only_a ?? [],
    only_b: res.only_b ?? [],
    refs_only_a: res.refs_only_a ?? [],
    refs_only_b: res.refs_only_b ?? [],
  }));
}

export function mergeStudies(payload: {
  a: string;
  b: string;
  new_id?: string;
  name?: string;
  accept: string[];
  deferred: string[];
  keep_checkpoint: "a" | "b" | "";
}): Promise<{ study: Study; state: Snapshot }> {
  return req("/api/merge", { method: "POST", body: JSON.stringify(payload) });
}

export function retractBlock(study: string, hash: string): Promise<{ state: Snapshot }> {
  return req("/api/retract", {
    method: "POST",
    body: JSON.stringify({ study, hash }),
  });
}
