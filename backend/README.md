# 链上数据研究网站 — 后端

Go 标准库实现的**人工区块**索引服务。本服务不连接任何公网节点，区块全部来自
`POST /api/import` 的人工提交，先落本地仅追加日志（`data/blocks.log`），再重算链状态。

## 核心语义

- **主链选择**：按累计工作量（`sum(weight)`，默认每块权重 1）选最优尖端；
  工作量相同比高度，再比哈希，保证结果确定。
- **分叉与撤回**：新分支成为主链后，旧分支区块标为 `forked`，其上交易确认数归零；
  交易位置与确认状态由后端在新版本中统一重算，不是前端重新排序。
- **重复交易**：同一交易哈希可出现在多个区块；`transactions[].occurrences` 列出全部位置，
  `duplicate=true`，主链上的最早位置作为当前位置。
- **版本快照**：每次导致状态变化的导入产生不可变 `Snapshot`，旧版本仍可通过
  `?version=N` 追溯；最近保留 64 个版本，过期返回 `410`。页面所有区块/交易详情都带同一版本号，
  保证“看到的是同一版本的状态”。
- **断点续传不重复**：导入先按哈希去重（内存中 + 批次内 + 落盘日志），重启后重放日志即可恢复；
  重复提交的区块进入 `skipped`，不产生新版本。
- **孤立块**：父块未知或父链成环的区块标为 `orphan`，父块到达后自动接入并重算。

## 运行

```bash
# 前端构建产物已嵌入二进制；也可在 frontend 目录 npm run dev 走 Vite 代理
go run .            # 默认 :8080，数据目录 ./data（INDEX_DIR 可改）
```

## API

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/api/import` | 提交区块数组（JSON），返回 imported/skipped/invalid 与版本变化 |
| GET | `/api/state?version=N` | 整站只读快照；省略版本取当前版本 |
| GET | `/api/blocks/{hash}?version=N` | 区块视图（同版本） |
| GET | `/api/transactions/{hash}?version=N` | 交易视图与全部出现位置（同版本） |
| GET | `/api/versions` | 可查询版本列表与当前版本 |
| POST | `/api/reset` | 仅用于演示/测试，清空本地数据 |

区块示例：

```json
{
  "hash": "B1",
  "parentHash": "G",
  "height": 1,
  "weight": 4,
  "timestamp": "2024-01-01T00:00:00Z",
  "miner": "bob",
  "transactions": [{ "hash": "tx-shared", "from": "carol", "to": "dave", "value": "7" }]
}
```

## 测试

```bash
go test ./...
```

覆盖：分叉后旧交易撤回但旧版本可查、跨分支重复交易位置迁移、导入中断/重启去重、
孤立块到达后接入、历史版本 410 与同版本详情一致性。
