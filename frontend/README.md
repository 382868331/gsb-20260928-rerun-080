# 前端

React 19 + Vite + TypeScript + @xyflow/react。

- `npm run dev` 启动开发服务器，`/api` 代理到 `localhost:8080`。
- `npm run build` 将产物直接输出到 `../backend/dist`，由 Go 通过 `go:embed` 提供。

页面顶部按钮演示：导入更重分叉触发 reorg、延伸旧分支、重复提交去重、版本切换。
分叉图按高度布局，主链绿色高亮；区块与交易互相点击引用，详情中的状态全部来自所选的同一个后端快照。
