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
