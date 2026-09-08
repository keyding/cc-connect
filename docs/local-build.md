# 本分支的使用范围

默认支持 Telegram，以及 Claude Code（配置名 `claudecode`）和 Codex。
其他 agent、平台的源码暂时保留，用于以后恢复和同步上游；它们不注册进默认二进制。
Web 管理界面也默认不编译，构建不需要 Node.js 或前端资源。
上游 README 中的完整能力列表不代表此分支默认启用的能力。

## 构建与配置

```sh
make build
# 或直接构建，默认支持范围相同：
go build -o cc-connect ./cmd/cc-connect
cp config.example.toml config.toml
```

设置 `TELEGRAM_BOT_TOKEN`、`TELEGRAM_ALLOW_FROM`（逗号分隔的 Telegram 数字用户 ID），
并修改 `work_dir`。Claude Code 使用 `mode = "default"`；Codex 使用 `mode = "suggest"`。
同时启用两个项目时，使用不同的 Bot Token，避免两个轮询器抢占同一个 Bot。
完整的上游配置参考保存在 [config.full.example.toml](config.full.example.toml)。
本次调整不会修改已部署服务或已有配置。

## 显式恢复功能

```sh
# 恢复一个额外 agent：
make build AGENTS=claudecode,codex,gemini PLATFORMS_INCLUDE=telegram
# 恢复全部适配器：
make build AGENTS=all PLATFORMS_INCLUDE=all
# 仅为默认适配器增加 Web 管理界面（需要前端构建环境）：
make build WITH_WEB=1
```

直接调用 Go 时，`full_plugins` 恢复全部适配器；原有 `no_<name>` 标签仍可排除单项。
`with_web` 启用 Web 管理界面，需先 `make web`；`no_web` 优先禁用它。
`make build-noweb`、`make release` 和 `make release-all` 使用同一套选择逻辑。
平台选择值使用插件文件中的标签名，例如 `cloud_web`、`wps_agentspace`。

源码仍在同一个 Go module，因此 `go.mod/go.sum` 保留全部适配器的依赖，
`go test ./...` 仍测试保留的包。精简的是默认可执行文件和构建入口，
不会明显降低 Claude/Codex 自身的内存开销或模型响应时间。
CLI 中部分上游平台的配置辅助命令和帮助文字仍保留，不代表适配器已经启用。

## 验证

```sh
go build ./...
go test ./...
go test ./core/ -run TestCUJ
make build
```

## GitHub CI

`ci.yml` 在 main 推送、面向 main 的 PR 或手动触发时运行。
检查任务和测试任务并行；仅 PR 的旧运行会被新提交取消。

- Linux 和 macOS 都构建所有保留的 Go 包、验证依赖校验和、构建默认 Telegram 二进制，并执行全量 race 测试。
- macOS 覆盖 launchd 等条件编译代码。smoke 和 regression 共用 Linux job，合并运行。
- 基准测试没有性能回退阈值，因此不再每次推送都执行；手动运行 CI 时勾选 `benchmarks` 即可。
- 删除重复覆盖率测试和 Codecov 上传；不再为此自用分支运行 Issue 欢迎评论及过期自动关闭任务。
- golangci-lint 保持增量检查：PR 对比 base SHA，main 推送对比 before SHA，手动运行对比父提交。未设置新的全仓 lint 零告警门槛。
- Go 兼容性版本仍来自 `go.mod`。golangci-lint 使用官方预编译包，避免为了构建检查工具额外下载 Go 1.26 工具链。

当前工具版本已对照官方发布核实：
[checkout v7.0.1](https://github.com/actions/checkout/releases/tag/v7.0.1)、
[setup-go v7.0.0](https://github.com/actions/setup-go/releases/tag/v7.0.0)、
[golangci-lint-action v9.3.0](https://github.com/golangci/golangci-lint-action/releases/tag/v9.3.0)、
[golangci-lint v2.13.2](https://github.com/golangci/golangci-lint/releases/tag/v2.13.2)、
[actionlint v1.7.12](https://github.com/rhysd/actionlint/releases/tag/v1.7.12)。
