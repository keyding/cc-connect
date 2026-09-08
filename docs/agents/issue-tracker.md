# Issue tracker: GitHub

本仓库的正式需求、规格和决策票保存在 keyding/cc-connect
的 GitHub Issues，使用 gh CLI 操作。

## 操作约定

- 读取：gh issue view <number> --repo keyding/cc-connect --comments
- 创建：gh issue create --repo keyding/cc-connect --title "..." --body-file <file>
- 评论：gh issue comment <number> --repo keyding/cc-connect --body-file <file>
- 查询：gh issue list --repo keyding/cc-connect，按状态和标签过滤。
- 标签与认领：使用 gh issue edit。
- 关闭：先记录解决结果，再使用 gh issue close。
- 多行正文写入临时文件，通过 --body-file 提交。
- 对用户引用任务时，使用带链接的标题。
- GitHub Issues 与 PR 共用编号；遇到歧义时确认对象类型。

## Pull requests as a triage surface

PRs as a request surface: no.

## 发布与本地草稿

技能要求“发布到 tracker”时，创建或更新 GitHub Issue。
.scratch/ 可保留草稿和研究附件。

本地地图迁移时保留标题、状态、研究结果、
父子关系与依赖，并在本地文件记录 GitHub 链接；
迁移后以 GitHub Issue 为准，避免两处独立维护状态。

## Wayfinding operations

- Map：单一 Issue，标签 wayfinder:map，保存目标、约束、
  已决事项索引、尚未明确项及范围外事项。
- Child：使用 GitHub 原生 sub-issue 关系，标签为
  wayfinder:research、wayfinder:prototype、
  wayfinder:grilling 或 wayfinder:task。
- 先创建全部票，再建立依赖。
- Blocking：使用 GitHub 原生 issue dependencies，通过 gh api 操作。
  添加 blocked_by 时使用阻塞票的数据库 id，而非 Issue 编号或 node_id。
- 只有原生关系不可用时，才使用父票任务列表、子票 Part of 链接
  和 Blocked by 正文约定。
- Frontier：父地图下开放、未认领且无开放阻塞票的子票；
  按地图子票顺序选择。
- Claim：工作前将票分配给当前开发者。
- Resolve：先发表评论记录答案，再关闭票，最后在地图的
  Decisions so far 追加标题链接及一句摘要。
- 研究附件从票中链接；完整决定只保存在对应票中。
