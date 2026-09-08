# 共享会话目录试用（Issue #14）

这是共享协作规格的目录切片，可独立验证双用户跨 Topic 创建、发现、选择和改名。共享任务执行、持久队列和旧消息路由由后续票实现；本阶段普通消息（包括附件）会提示当前选择，**不接收、保存或执行任务**。尚不能用于正式两人协作。

## 全新实例

为试用实例使用独立配置、独立 Bot Token 和一个全新的绝对 `data_dir`。不要复用旧实例目录、复制旧状态或清理 Agent 历史。配置顶层及 Telegram options 示例：

```toml
language = "zh"
data_dir = "/absolute/path/to/cc-connect-directory-preview"

# 保留项目及 agent 配置，work_dir 使用原有项目目录。
# 在该项目的 Telegram [projects.platforms.options] 下设置：
# shared_session_directory = true
# share_session_in_channel = false
# allow_from = "<Alice 的数字 ID>,<Bob 的数字 ID>"
```

`shared_session_directory` 默认 false，避免尚未实现的共享执行影响现有实例。启用后仅群和超级群进入目录流程；私聊及其他平台沿用既有行为，不加入共享目录。`allow_from` 仍在平台入口逐条校验，群内仍遵循 @Bot / `group_reply_all` 设置。

新实例重启必须继续使用相同 `data_dir`、项目名称和选择模式；不要每次生成新目录。目录只呈现本实例登记的会话，不调用 Agent 历史列表，也不会导入、改写或删除旧实例数据。创建会话不创建分支或 worktree。

## 命令与选择

- `/new [名称]`：原子创建待开始会话并选择它；无名自动生成唯一 `session-N`。重名失败不会自动加入已有会话。
- `/list`：同群同项目跨 Topic 的目录，按创建顺序编号；`*` 表示本入口当前选择。显示名称、固定 Agent 类型及稳定 ID。
- `/switch <序号、完整名称或 ID>`：只改变本入口默认选择。数字优先解释为列表序号。名称包含空格时可直接填写或加引号。
- `/name <新名称>`（或 `/rename`）：改名当前选择；首尾空白和 ASCII 英文字母大小写不构成区别。
- `/current`：显示当前选择。未选择时提示创建/选择，不保存原任务供稍后重放。

`share_session_in_channel = false` 是默认值：每人每 Topic 独立选择；true：同 Topic 的授权成员共用选择。没有 Topic 时使用群入口。两种模式的目录都跨用户和 Topic 共享，不跨群、项目或平台共享。新建、改名和切换不删除原共享对象。

目录试用仅开放以上命令，其他命令明确提示支持范围，避免触发旧的按入口执行、历史导入或删除流程。共享执行开放前，选中会话后的普通消息只展示目标及未接收提示。

## 验证与限制

自动化通过 `Engine.ReceiveMessage` 驱动真实 Engine 和文件持久化，替换 Agent 进程及平台发送边界；Telegram 测试覆盖协议范围、发起人和两种选择键模式。重建 Engine 验证持久恢复，不等同于真实服务进程重启。

请在后续联合验收中用两个真实 Telegram 账号、两个 Topic 和真实 Agent 验证完整执行、队列、权限及重启。本 PR 的 CI 通过只表示目录代码可合并，不代表真实 Telegram/Agent 联合验收通过。
