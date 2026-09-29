import type { ImportResult, Snapshot } from "./types";

export class VersionExpiredError extends Error {
  constructor(public version: number) {
    super(`索引版本 ${version} 已过期`);
  }
}

async function handle(res: Response) {
  if (res.status === 410) {
    throw new VersionExpiredError(0);
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || `HTTP ${res.status}`);
  }
  return res.json();
}

export async function fetchState(version = 0): Promise<Snapshot> {
  const qs = version ? `?version=${version}` : "";
  const res = await fetch(`/api/state${qs}`);
  return handle(res);
}

export async function importBlocks(blocks: unknown[]): Promise<ImportResult> {
  const res = await fetch("/api/import", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(blocks),
  });
  return handle(res);
}

export async function resetIndex(): Promise<void> {
  await fetch("/api/reset", { method: "POST" });
}

export async function fetchVersions(): Promise<{ versions: number[]; current: number }> {
  const res = await fetch("/api/versions");
  return handle(res);
}
