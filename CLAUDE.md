# CLAUDE.md

本项目的所有约定都在 AGENTS.md，这里只引用它，避免两份规则不一致：

@AGENTS.md

## Claude Code 补充

- 改 UI 后用 `ARCMD_SNAPSHOT=<scratchpad 目录> go test -run Snapshot ./internal/app` 渲染 PNG，再用 Read 查看图片自检；不要只凭代码判断界面效果。
- 需要真机运行时用 `ARCMD_HOME=<scratchpad 目录>/home go run ./cmd/arcmd`，不要往用户真实的 `~/Documents/ArcMD` 写测试数据。
- 不确定 MyGo API 时，先 `go doc` 或读 module cache 里的源码和 `docs/`，再写代码。
