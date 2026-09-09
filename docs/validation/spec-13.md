# Spec #13 验证记录

范围：[Spec #13](https://github.com/keyding/cc-connect/issues/13) 及 #14–#22。基线 `e0e49036f78bd6062f793654c67ff4dd25101cb3` 已包含 #14、#15。当前记录以集成提交 `565a2f48` 为实现基准；本 PR 不合并、不切换现有服务。

依赖：#15 → #16 → #17 → #20；#15 → #18 → #19；#18 + #20 → #21；#19 + #21 → #22。

## 自动化证据

下表列出实际测试入口；源文件中的测试是可复现证据。测试替换 Telegram HTTP 或模型 CLI 边界，不等于真实账号、模型或部署验收。各实施提交均已通过 `go build ./...`、`go test ./...`、`go test ./core/ -run TestCUJ`；#16–#21 另有相关包竞态测试，#17、#19 修正后、#20、#21 有增量 lint 验证。

| Ticket / 实施提交 | 覆盖与可复现测试 |
| --- | --- |
| #14 / 基线 | [core/cuj_test.go](../../core/cuj_test.go)：`TestCUJ_B14_SharedDirectoryAcrossTopicsAndRestart`、`TestCUJ_B14_SharedSelectionModesAndIsolation`、`TestCUJ_B14_SharedNamesAreAtomic`、`TestCUJ_B14_FreshDirectoryDoesNotImportAgentHistory`；跨 Topic、选择隔离、名称及全新状态。 |
| #15 / 基线 | [core/shared_queue_test.go](../../core/shared_queue_test.go)：`TestSharedQueue_DurableFIFOAndFixedTarget`、`TestSharedQueue_SameActualDirectoryAndIndependentDirectory`、`TestSharedQueue_RestartKeepsQueuedAttachmentsAndReply`；持久接收、固定目标、实际目录串行。`TestCUJ_B15_SharedQueueSelectionModesAndConcurrentStart` 覆盖用户入口。 |
| #16 / `6c192a7e` | `TestCUJ_B16_SharedQueueStopCancelAndExplicitResume`、`TestCUJ_B16_StoppedQueueSurvivesRestart`、`TestCUJ_B16_ControlRechecksCurrentAuthorization`；取消所有权、停止、退出后显式恢复与实时权限。 |
| #17 / `228c01ef` | `TestCUJ_B17_InterruptedRequestNeedsOwnerWarningAndExplicitRecovery`、`TestCUJ_B17_UncommittedAdmissionIsNotExecutedAfterRestart`、`TestCUJ_B17_UncertainAdmissionFencesExecutionAndProvidesStableLookup`；持久重建与故障注入。[core/shared_recovery_process_test.go](../../core/shared_recovery_process_test.go) 的 `TestSharedRecovery_RealProcessGroupSurvivesHostCrash` 和 [core/shared_executor_unix_test.go](../../core/shared_executor_unix_test.go) 的 `TestSharedRecovery_DirectWaitDoesNotReleaseLiveProcessGroup` 使用真实本地测试进程，另已通过 Windows 交叉构建。 |
| #18 / `93b66623` | `TestCUJ_B18_ReplyOldAnswerPreservesDefaultAndRestart`；[platform/telegram/telegram_receipts_test.go](../../platform/telegram/telegram_receipts_test.go) 的 `TestReplyWithReceipt_HTTPContracts`、`TestDispatchMessage_ExplicitBotReferenceAndDuplicate`；旧回复路由、重启、重复与 Bot 身份。 |
| #19 / `392c3cdd`，lint 修正 `2cb3cb2b` | `TestCUJ_B19_MediaProgressEditsAndCrossTopicReplies`；[platform/telegram/telegram_media_receipts_test.go](../../platform/telegram/telegram_media_receipts_test.go) 的 `TestMediaAndPreviewReceipts_HTTPPartialFailureAndEditIdentity`、`TestExternalReply_ProtocolIdentityAndGroupUpgradeFailClosed`；[core/shared_output_adapters_test.go](../../core/shared_output_adapters_test.go) 的 `TestSharedOutput_ConcurrentAgentProcessEnvironment` 检查真实测试进程的独立任务环境。 |
| #20 / `db2ab336`，适配器 `52af0be8`、`f1b355f0` | `TestCUJ_B20_InitiatorNonceQuestionsAndDefaultIndependence`、`TestCUJ_B20_RestartTimeoutAndRevokedInteractionEntries`；[Telegram 契约](../../platform/telegram/telegram_interaction_test.go) 的 `TestSharedInteraction_CallbackBindingAndDuplicateProtocol`；[Claude 契约](../../agent/claudecode/interaction_contract_test.go) 的 `TestClaudeInteraction_ProcessProtocolContracts`；[Codex 契约](../../agent/codex/appserver_session_test.go) 的 `TestAppServerSession_ApprovalTimeoutEmitsErrorAndRejectsLateResponse`、`TestAppServerSession_ApprovalContractsConsumeRequestOnce`。 |
| #21 / `2834e6bb`，Telegram `cc06570d` | `TestCUJ_B21_RevocationCancelsOwnQueueAndStopsWaitingExecutor`、`TestCUJ_B21_DeleteConfirmationPreservesCodeAndInvalidatesOldReplies`、`TestCUJ_B21_ConfirmedDeleteAndConcurrentAdmissionAreAtomic`、`TestCUJ_B21_RestartRechecksQueuedRequestOwnersBeforeDispatch`；[Telegram 契约](../../platform/telegram/telegram_authorization_test.go) 的 `TestLiveAllowFrom_SharedDenialSkipsAttachmentsAndCallbackActions`；[配置重载契约](../../cmd/cc-connect/shared_authorization_test.go) 的 `TestReloadConfig_UpdatesActualTelegramAuthorizationBeforeSharedOperations` 通过管理 HTTP 重载实际 TOML。 |
| #22 | 本文仅交付验收准备与证据索引。真实联合验收 **NOT_RUN**，Ticket 不据此宣称完成。 |

## 已记录的阶段 CI

以下每个固定提交的 Linux / macOS Test 和 lint 均通过。工作流执行构建、模块校验、默认构建、全量 race 测试，Linux 另运行 smoke/regression；lint 包含 actionlint 和增量 golangci-lint。没有运行记录的可选性能测试不计入证据。阶段通过不能代替最终 PR head 的 CI。

| 固定提交 | GitHub Actions 记录 | 结果 |
| --- | --- | --- |
| `e1af5eb56c38e735bbc3d32987de13cdd6a6550a` | [阶段 1](https://github.com/keyding/cc-connect/actions/runs/34290679829) | PASS |
| `b62bfcbe44eef9ad162282676e8ec7056943bb0a` | [阶段 2](https://github.com/keyding/cc-connect/actions/runs/34291846805) | PASS |
| `5b4ec5bc78dfb24e0005f694054480a63004ab9f` | [阶段 3](https://github.com/keyding/cc-connect/actions/runs/34293097920) | PASS |
| `4ba3fb004055a8664d2fa5f499d5f9bc354bd639` | [阶段 4](https://github.com/keyding/cc-connect/actions/runs/34294840678) | PASS |
| `565a2f4874df070de348ef8c0102789f96daadca`（完整实现） | [阶段 5](https://github.com/keyding/cc-connect/actions/runs/34296214248) | PASS；Linux 3m00s、macOS 2m59s、lint 53s |

| 最终门槛 | 状态 / 待填写证据 |
| --- | --- |
| 最终代码审查与问题修复 | PENDING：审查基准、结论、修正提交 |
| 最终 PR head 构建、全量测试、CUJ、race、增量 lint | PENDING：精确 SHA 与结果 |
| 最终 PR head CI | PENDING：精确 SHA、运行 URL、各 job 结果 |
| PR ready for review | PENDING：在审查与最终 CI 通过后设置 |
| PR 合并及生产切换 | 不执行，超出本次授权 |

## 真实联合验收矩阵（#22）

准备独立 Bot、独立配置和全新绝对 `data_dir`，保留旧实例目录、Agent 历史、代码及 worktree。账号 A/B 均在配置 allowlist 中，群有 Topic X/Y。记录精确 PR SHA、CLI 版本、脱敏配置、时间、操作人、消息链接及服务/Agent 日志；不得记录 token。每项填写实际结果、证据及偏差。当前没有执行真实服务或发送消息。

对 Claude Code 与 Codex 分别执行，并对 `share_session_in_channel=false/true` 各执行一轮。Codex 审批使用本地 `backend="app_server"`，另确认默认 exec 的无审批 IPC 限制。

| 场景 | 人工步骤与预期 | 实际结果 |
| --- | --- | --- |
| 全新启动与保留旧状态 | 记录旧目录清单；新实例创建会话后正常重启。新目录不导入旧会话，重启保留新目录；旧文件与 Agent 历史不变。 | NOT_RUN |
| 双用户、双 Topic 选择 | A 在 X 创建 s1，B 在 Y 选择它；再创建/切换 s2，引用 s1 旧回复。引用仍投 s1且不改变默认；两种模式分别体现个人/Topic 共享默认。 | NOT_RUN |
| 文本、媒体、引用 | 发图片/文件/音频，产生多段、进度、编辑及媒体回复；逐一引用。有效同群跨 Topic 引用命中原会话；转发、未知/跨群/已删引用不猜默认。 | NOT_RUN |
| 固定排队目标 | 长任务期间 B 排入带附件请求，再改名/切换；检查队列和最终输出。原会话、附件、发起人及回复 Topic 保持提交值。 | NOT_RUN |
| 目录互斥与并行 | 两会话同实际目录（含符号链接）提交任务，再用不同目录提交。前者无重叠写入，后者可并行；以时间与进程日志佐证。 | NOT_RUN |
| 取消、停止、恢复 | A 排队，B 尝试取消 A 请求应拒绝；B 停止运行任务。确认执行进程退出后显式恢复未开始任务，未恢复前保持暂停。 | NOT_RUN |
| 审批与提问 | A 触发权限/多题；B 点击、普通回复审批文本、重复点击旧按钮均不执行；A 用生成按钮/命令回答。等待期间锁与队列保留，既有超时生效且不默认批准。 | NOT_RUN |
| 真实崩溃与重启 | 在未交付、进程开始后、等待审批时分别中断真实服务并重启。只恢复确定未开始任务；可能已执行任务不重放；旧按钮失效，未证实退出保持阻塞。记录实际子进程状态。 | NOT_RUN |
| 恢复边界 | 对已证实退出的中断请求使用 `/resolve`，再 `/resume` 未开始请求；尝试 `/continue` 确认。保留已有结果，不重发原任务；如无法恢复 Agent 进度应明确提示不支持。 | NOT_RUN |
| 撤销配置授权 | 分别在排队、运行、等待审批时从 allowlist 移除 A 并重载。A 仅收到通用拒绝，其排队请求取消、运行任务停止、按钮失效；B 和共享历史保留。 | NOT_RUN |
| 确认删除 | 忙碌会话删除被拒；空闲会话取得确认后新增请求，旧确认应失效；再完成空闲删除。默认/目录/旧引用失效，代码、worktree、Agent 历史保留。 | NOT_RUN |
| 投递与接收不确定 | 在可控环境造成投递失败或持久化故障。失败不重跑 Agent、不改投其他 Topic；接收不确定时依稳定请求 ID 查询队列后决定是否重提，不能把提示当接收成功。 | NOT_RUN |

本地测试进程崩溃仅证明受控 OS/协议场景，不替代上表的真实 Agent 与服务中断。执行者身份保存前的崩溃窗口、无法证明退出的远端/不支持平台、脱离进程组的外部写入均应保守处理；通用历史续聊不等于从工具执行进度恢复。没有以上必需真实证据前，不宣称 #22 完成或可正式双人协作。
