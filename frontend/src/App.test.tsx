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
      active: false, independent_sources: 1,
    },
    {
      from_txid: "txA", to_txid: "coin-G", vout: 0, resolved: true,
      from_block_hash: "B2", from_branch_id: "main", from_canonical: true, from_confirmations: 3,
      to_block_hash: "G", to_branch_id: "main", to_canonical: true, to_confirmations: 5,
      active: true, independent_sources: 1,
    },
  ],
  conflicts: [],
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
});

describe("版本一致性", () => {
  it("导入完成后顶部选择器与主体共同指向新版本", async () => {
    const imported: Snapshot = { ...snapshot, version: 3, label: "新导入" };
    vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => ({
      ok: true,
      json: async () => {
        if (url.includes("/api/import")) return { added: 1, skipped: 0, state: imported };
        if (url.includes("/api/state")) return { snapshot };
        if (url.includes("/api/versions")) {
          return { versions: [
            { version: 2, created_at: "", tip_hash: "B4", tip_height: 4 },
            { version: 3, created_at: "", tip_hash: "B4", tip_height: 4, label: "新导入" },
          ] };
        }
        return {};
      },
      text: async () => "",
      status: 200,
    })));
    render(<App />);
    await waitFor(() => expect(screen.getAllByText(/v2/).length).toBeGreaterThan(0));
    // 粘贴任意合法区块数组并导入。
    fireEvent.change(screen.getByPlaceholderText(/\[\{"hash"/), {
      target: { value: JSON.stringify([{ hash: "X", prev_hash: "", txs: [] }]) },
    });
    fireEvent.click(screen.getByText("导入并重建索引"));
    // 主体与选择器都应指向 v3。
    await waitFor(() => expect(screen.getAllByText(/v3/).length).toBeGreaterThan(0));
    const select = screen.getAllByRole("combobox")[0] as HTMLSelectElement;
    expect(select.value).toBe("3");
    expect(screen.getByText(/导入完成：新增 1/)).toBeTruthy();
  });

  it("切换版本失败时保留原视角并显示未完成状态", async () => {
    vi.stubGlobal("fetch", vi.fn(async (url: string) => {
      if (url.includes("/api/state?v=3")) {
        return { ok: false, status: 404, text: async () => "版本不存在", json: async () => ({}) };
      }
      return {
        ok: true,
        json: async () => {
          if (url.includes("/api/state")) return { snapshot };
          if (url.includes("/api/versions")) {
            return { versions: [
              { version: 2, created_at: "", tip_hash: "B4", tip_height: 4 },
              { version: 3, created_at: "", tip_hash: "B4", tip_height: 4 },
            ] };
          }
          return {};
        },
      };
    }));
    render(<App />);
    await waitFor(() => expect(screen.getAllByText(/v2/).length).toBeGreaterThan(0));
    fireEvent.change(screen.getAllByRole("combobox")[0], { target: { value: "3" } });
    // 切换失败：提示未完成，选择器与主体仍对应 v2。
    await waitFor(() => expect(screen.getByText(/切换到 v3 未完成/)).toBeTruthy());
    const select = screen.getAllByRole("combobox")[0] as HTMLSelectElement;
    expect(select.value).toBe("2");
    expect(screen.getByText(/当前仍显示 v2/)).toBeTruthy();
  });
});

describe("研究线比较与合并", () => {
  const lineSnapshot: Snapshot = {
    ...snapshot,
    version: 1,
    line_id: "L3",
    label: "合并 L1 + L2（依据 C1）",
    line: {
      id: "L3",
      name: "合并线",
      parent_lines: ["L1", "L2"],
      base_comparison: "C1",
      adoptions: {
        G: { hash: "G", comparison_id: "C1", source_line: "both", adopted_at: "2026-10-01T00:00:00Z" },
        B2: { hash: "B2", comparison_id: "C1", source_line: "L2", adopted_at: "2026-10-01T00:00:00Z" },
      },
      pending_gaps: [{ hash: "B4", missing_parents: ["B3"] }],
      deferred: [{ hash: "A2", comparison_id: "C1", reason: "跨越 L1 已确认检查点的冲突资料，暂不采用" }],
      withdrawals: [],
    },
  };

  function stubLineFetch() {
    vi.stubGlobal("fetch", vi.fn(async (url: string) => ({
      ok: true,
      json: async () => {
        if (url.includes("/api/lines/state")) return { snapshot: lineSnapshot };
        if (url.includes("/api/lines/versions")) {
          return { versions: [{ version: 1, created_at: "", tip_hash: "B4", tip_height: 4, label: "合并" }] };
        }
        if (url.includes("/api/lines")) {
          return { lines: [
            { id: "L1", name: "研究线 A", created_at: "", block_count: 4, checkpoints: 2, latest_version: 3 },
            { id: "L2", name: "研究线 B", created_at: "", block_count: 3, checkpoints: 1, latest_version: 2 },
            { id: "L3", name: "合并线", created_at: "", block_count: 5, checkpoints: 1, latest_version: 1,
              parent_lines: ["L1", "L2"], base_comparison: "C1" },
          ] };
        }
        if (url.includes("/api/state")) return { snapshot };
        if (url.includes("/api/versions")) return { versions: [{ version: 2, created_at: "", tip_hash: "B4", tip_height: 4 }] };
        return {};
      },
    })));
  }

  it("切换到研究线后各区域与版本选择器共同指向线版本", async () => {
    stubLineFetch();
    render(<App />);
    await waitFor(() => expect(screen.getAllByText(/v2/).length).toBeGreaterThan(0));
    // 选择研究线 L3（合并线）。
    const lineSelect = screen.getByLabelText(/当前研究线/) as HTMLSelectElement;
    fireEvent.change(lineSelect, { target: { value: "L3" } });
    await waitFor(() => expect(screen.getAllByText(/合并线/).length).toBeGreaterThan(0));
    // 版本选择器指向研究线自己的版本序列。
    const versionSelect = screen.getAllByRole("combobox")[0] as HTMLSelectElement;
    expect(versionSelect.value).toBe("1");
    // 待补充范围、暂缓冲突与采用审计共同出现在线视图中。
    expect(screen.getByText(/待补充范围（缺少中间资料，未拼接）/)).toBeTruthy();
    expect(screen.getByText(/暂缓的冲突部分（仍有效，待裁决）/)).toBeTruthy();
    expect(screen.getByText(/采用决定审计（每个区块来自哪次比较）/)).toBeTruthy();
    expect(screen.getAllByText(/C1/).length).toBeGreaterThan(0);
  });
});
