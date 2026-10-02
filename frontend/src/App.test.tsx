import { describe, expect, it, vi, beforeEach } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import App from "./App";
import type { Snapshot } from "./types";

const snapshot: Snapshot = {
  version: 2,
  created_at: "2026-09-29T00:00:00Z",
  label: "分叉切换",
  tip_hash: "B4",
  tip_height: 4,
  blocks: [
    { hash: "G", prev_hash: "", height: 0, canonical: true, branch_id: "main", confirmations: 5, work: 100, txs: [] },
    { hash: "A1", prev_hash: "G", height: 1, canonical: false, branch_id: "fork-A1", confirmations: 0, work: 10, txs: [] },
    { hash: "A2", prev_hash: "A1", height: 2, canonical: false, branch_id: "fork-A1", confirmations: 0, work: 10, txs: [{ index: 0, txid: "txA", memo: "旧分叉位置" }] },
    { hash: "B1", prev_hash: "G", height: 1, canonical: true, branch_id: "main", confirmations: 4, work: 10, txs: [] },
    { hash: "B2", prev_hash: "B1", height: 2, canonical: true, branch_id: "main", confirmations: 3, work: 10, txs: [{ index: 0, txid: "txA" }] },
    { hash: "B4", prev_hash: "B2", height: 4, canonical: true, branch_id: "main", confirmations: 1, work: 10, txs: [] },
  ],
  tx_index: [
    { txid: "txA", block_hash: "A2", index: 0, height: 2, canonical: false, confirmations: 0, branch_id: "fork-A1" },
    { txid: "txA", block_hash: "B2", index: 0, height: 2, canonical: true, confirmations: 3, branch_id: "main" },
  ],
  tx_refs: [
    {
      from_txid: "txA", to_txid: "coin-G", vout: 0, resolved: true,
      from_block_hash: "A2", from_branch_id: "fork-A1", from_canonical: false, from_confirmations: 0,
      to_block_hash: "G", to_branch_id: "main", to_canonical: true, to_confirmations: 5,
      active: false,
    },
    {
      from_txid: "txA", to_txid: "coin-G", vout: 0, resolved: true,
      from_block_hash: "B2", from_branch_id: "main", from_canonical: true, from_confirmations: 3,
      to_block_hash: "G", to_branch_id: "main", to_canonical: true, to_confirmations: 5,
      active: true,
    },
  ],
};

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn(async (url: string) => ({
    ok: true,
    json: async () => {
      if (url.includes("/api/state")) return { snapshot };
      if (url.includes("/api/versions")) return { versions: [{ version: 2, created_at: "", tip_hash: "B4", tip_height: 4 }] };
      return {};
    },
  })));
});

describe("App 分叉快照渲染", () => {
  it("显示版本、主链端点与被撤回旧分叉统计", async () => {
    render(<App />);
    await waitFor(() => expect(screen.getAllByText(/B4/).length).toBeGreaterThan(0));
    expect(screen.getAllByText(/v2/).length).toBeGreaterThan(0);
    expect(screen.getAllByText(/已撤回旧分叉/).length).toBeGreaterThan(0);
  });

  it("重复交易在旧分叉与新主链各有位置，确认状态随区块一致", async () => {
    render(<App />);
    await waitFor(() => expect(screen.getAllByText(/v2/).length).toBeGreaterThan(0));
    fireEvent.click(screen.getAllByText("txA").filter((el) => el.tagName === "BUTTON")[0]);
    // 展开后应同时出现 A2（撤回，0 确认）与 B2（主链，3 确认）两个位置。
    await waitFor(() => expect(screen.getByText("旧分叉撤回")).toBeTruthy());
    expect(screen.getByText("主链有效")).toBeTruthy();
    expect(screen.getByText("重复 ×2（跨分叉）")).toBeTruthy();
  });

  it("引用同时展示所在区块、链视角与当前有效性", async () => {
    render(<App />);
    await waitFor(() => expect(screen.getAllByText(/v2/).length).toBeGreaterThan(0));
    fireEvent.click(screen.getAllByText("txA").filter((el) => el.tagName === "BUTTON")[0]);
    // 同一引用在旧分叉（非当前链视角）与主链（当前有效）各一条。
    await waitFor(() => expect(screen.getByText("当前有效")).toBeTruthy());
    expect(screen.getByText("非当前链视角")).toBeTruthy();
    expect(screen.getByText(/旧分叉视角 · 0 确认/)).toBeTruthy();
    expect(screen.getByText(/主链视角 · 3 确认/)).toBeTruthy();
  });

  it("首次打开空索引（null 字段）不白屏，并给出明确状态", async () => {
    vi.stubGlobal("fetch", vi.fn(async (url: string) => ({
      ok: true,
      json: async () => {
        if (url.includes("/api/state")) {
          return { snapshot: { version: 0, created_at: "", tip_hash: "", tip_height: 0, blocks: null, tx_index: null, tx_refs: null } };
        }
        if (url.includes("/api/versions")) return { versions: [] };
        return {};
      },
    })));
    render(<App />);
    await waitFor(() => expect(screen.getByText(/当前没有索引版本/)).toBeTruthy());
  });

  it("导入完成后选择器与页面各区域同步指向新版本", async () => {
    const snapshotV3: Snapshot = { ...snapshot, version: 3, label: "新导入" };
    vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => ({
      ok: true,
      json: async () => {
        if (url.includes("/api/import")) return { added: 1, skipped: 0, state: snapshotV3 };
        if (url.includes("/api/state")) return { snapshot };
        if (url.includes("/api/versions")) {
          return {
            versions: [
              { version: 2, created_at: "", tip_hash: "B4", tip_height: 4 },
              { version: 3, created_at: "", tip_hash: "B4", tip_height: 4, label: "新导入" },
            ],
          };
        }
        return {};
      },
    })));
    render(<App />);
    await waitFor(() => expect(screen.getAllByText(/v2/).length).toBeGreaterThan(0));
    // 展开导入面板并提交。
    fireEvent.click(screen.getByText(/导入人工区块/));
    fireEvent.change(screen.getByPlaceholderText(/"hash"/), { target: { value: '[{"hash":"X","prev_hash":"","txs":[]}]' } });
    fireEvent.click(screen.getByText("导入并重建索引"));
    // 主体与顶部选择器都应指向 v3，而不是停留在旧版本。
    await waitFor(() => expect(screen.getAllByText(/v3/).length).toBeGreaterThan(0));
    const select = screen.getAllByRole("combobox")[0] as HTMLSelectElement;
    expect(select.value).toBe("3");
    expect(screen.getByText(/导入完成：新增 1/)).toBeTruthy();
  });

  it("版本切换失败时保留原视角并显示未完成状态", async () => {
    vi.stubGlobal("fetch", vi.fn(async (url: string) => {
      if (url.includes("/api/state?v=1")) {
        return { ok: false, status: 404, text: async () => "版本不存在", json: async () => ({}) };
      }
      return {
        ok: true,
        json: async () => {
          if (url.includes("/api/state")) return { snapshot };
          if (url.includes("/api/versions")) {
            return {
              versions: [
                { version: 1, created_at: "", tip_hash: "A3", tip_height: 3 },
                { version: 2, created_at: "", tip_hash: "B4", tip_height: 4 },
              ],
            };
          }
          return {};
        },
      };
    }));
    render(<App />);
    await waitFor(() => expect(screen.getAllByText(/v2/).length).toBeGreaterThan(0));
    const select = screen.getAllByRole("combobox")[0] as HTMLSelectElement;
    fireEvent.change(select, { target: { value: "1" } });
    // 切换失败：保留 v2 视角，显示未完成状态。
    await waitFor(() => expect(screen.getByText(/版本切换未完成/)).toBeTruthy());
    expect(screen.getByText(/仍显示 v2 视角/)).toBeTruthy();
    expect(screen.getAllByText(/分叉切换/).length).toBeGreaterThan(0);
  });

  it("检查点冲突时展示冲突与引用差异，不自动撤销确认", async () => {
    const withCheckpoint: Snapshot = {
      ...snapshot,
      checkpoint: {
        block_hash: "B1",
        height: 1,
        confirmed_at: "2026-09-29T01:00:00Z",
        basis: { version: 1, tip_hash: "B4", tip_height: 4, chain: ["G", "B1"] },
        status: "conflict",
        conflict: {
          rival_tip_hash: "A3",
          rival_tip_height: 3,
          rival_work: 999,
          diverge_height: 0,
          ref_changes: {
            from_version: 2,
            to_version: 2,
            entries: [{ kind: "position_migrated", before: snapshot.tx_refs[0], after: snapshot.tx_refs[1] }],
          },
        },
      },
    };
    vi.stubGlobal("fetch", vi.fn(async (url: string) => ({
      ok: true,
      json: async () => {
        if (url.includes("/api/state")) return { snapshot: withCheckpoint };
        if (url.includes("/api/versions")) return { versions: [{ version: 2, created_at: "", tip_hash: "B4", tip_height: 4 }] };
        return {};
      },
    })));
    render(<App />);
    await waitFor(() => expect(screen.getByText(/存在越过检查点的竞争链/)).toBeTruthy());
    expect(screen.getByText(/确认依据：版本 v1/)).toBeTruthy();
    expect(screen.getByText(/试图改写检查点之前的已确认历史/)).toBeTruthy();
    expect(screen.getByText("采纳竞争链（撤销原确认）")).toBeTruthy();
    expect(screen.getByText("保留确认，建立独立研究线")).toBeTruthy();
  });
});
