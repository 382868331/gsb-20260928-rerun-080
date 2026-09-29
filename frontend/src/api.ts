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
  return req(`/api/state${q}`);
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
