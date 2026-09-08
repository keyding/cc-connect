# Git worktree 清理与恢复约束

核查日期：2026-09-08。范围：Git 官方文档与隔离实验；不决定产品备份格式。对应决策票：核实 Git worktree 清理与代码恢复约束。

## 文档事实

- linked worktree 顶层 `.git` 是指针文件；私有 `HEAD`、index 等位于主库的 `worktrees/<id>`，对象库和多数 refs 共享。必须通过 `rev-parse --git-path` 等解析位置，不能假设 `.git` 是完整仓库。[worktree DETAILS](https://git-scm.com/docs/git-worktree#_details)、[仓库布局](https://git-scm.com/docs/gitrepository-layout)
- `remove` 普通模式拒绝修改及未跟踪文件；`prune` 清除已缺失工作目录的登记；`lock` 防止登记被裁剪以及普通移动、删除，不是执行互斥锁。主工作区不能由 `worktree move/remove` 处理；带子模块的 linked worktree 也有限制。手工搬迁主库或两端后，可用 `repair` 修复双向路径，但它不能补回丢失对象。[worktree 命令](https://git-scm.com/docs/git-worktree#_commands)
- bundle 保存指定 refs 及其可达对象，未推送提交只要被包含的 ref 引用就能保留。它不保存 index、工作文件、仓库配置等；增量 bundle 依赖前置对象，`verify` 检查适用性。不能把 `--all` 当作完整机器状态备份，也不能假定只在 reflog 中的旧提交必定包含。[bundle](https://git-scm.com/docs/git-bundle)
- upstream 是配置的远端跟踪分支，可能与 push 目标不同；远端跟踪 refs 是本地记录，fetch 才更新。`upstream..HEAD` 可检查相对差集，但“为零”只代表这份记录中的可达关系，无 upstream 时无法据此得出已推送。[rev-parse](https://git-scm.com/docs/git-rev-parse)、[rev-list](https://git-scm.com/docs/git-rev-list)、[fetch](https://git-scm.com/docs/git-fetch)
- 普通 status 不显示 ignored 文件；不能将所有忽略项都理解成可丢弃缓存。[status](https://git-scm.com/docs/git-status)

## 隔离实测

本机 Git 2.51.1，临时目录 `cc-git-recovery-z3w3hen3`，实验结束已删除；没有使用项目历史或远端。步骤：初始化主库并提交基础文件；建立 session worktree，增加未发布提交；同一文件先暂存后再修改，另建未跟踪文件；创建 `bundle --all` 并复制工作目录；搬迁主库；repair 原 worktree；从 bundle 克隆 session；另建干净分支测试删除。

| 检查 | 结果 |
| --- | --- |
| 无 upstream 的 `rev-parse --verify '@{upstream}'` | 退出 128 |
| 脏 worktree 普通 remove | 退出 128 |
| 仅复制的 worktree，在主库搬迁后执行 status | 退出 128 |
| repair 后原 worktree 状态 | 仍为 `MM file`、`?? new` |
| bundle 克隆 | 恢复相同未发布 HEAD；文件停在提交版本，无暂存改动及未跟踪文件 |
| 锁定干净 worktree 普通 remove | 退出 128 |
| 解锁后删除干净、未发布分支的 worktree | 退出 0 |

这是上述 Git 文档机制的最小验证，不是 cc-connect 恢复验收。最后一项表明产品必须另行保护未发布提交，不能只依赖 remove 的检查。

## 推断：恢复内容下界

根据仓库布局与 bundle 边界，单独打包 worktree 只能保留文件快照，不能独立恢复 Git 会话。至少需要：目标提交及依赖对象、分支/HEAD 身份；文件内容、删除信息、模式与符号链接、必要未跟踪文件；若要恢复暂存区则保存 index 及对应对象或等效状态；恢复目录与会话的关联。原样恢复还要覆盖私有管理状态和共享配置，搬迁后验证链接。需核实对象是否借用 alternates，不能只复制本库对象目录就声称自包含。[仓库布局](https://git-scm.com/docs/gitrepository-layout)、[bundle](https://git-scm.com/docs/git-bundle)

## 待决问题

备份如何取得一致切点；是否支持进行中的 merge/rebase、detached HEAD、子模块、外部对象及大文件；如何处理未知推送状态和 ignored 用户成果；恢复是否必须保留暂存区。这些是后续规格边界，尚未决策或实测。已确认的“不删除未提交或未推送成果”约束不因本研究改变。
