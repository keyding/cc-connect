# Telegram 消息关联接口事实

核查日期：2026-09-08。代码基线：`016669df6db6e8a04035afa6e33c8845a34b1968`。仅核查公开官方文档及本地源码，未访问账号消息；不确定产品策略。

## 官方契约

- 普通消息 ID 仅在 chat 内唯一；Topic 有独立 thread ID。`message_id=0` 可能表示尚未实际发出的消息或临时消息，不能直接当持久路由键。[Message](https://core.telegram.org/bots/api#message)
- 同 chat、同 thread 回复可带 `reply_to_message`，但不递归附带回复链；跨 Topic/群引用可带 `external_reply`，原消息标识是可选字段。[Message](https://core.telegram.org/bots/api#message)、[ExternalReplyInfo](https://core.telegram.org/bots/api#externalreplyinfo)
- 文本发送成功返回 Message；普通消息编辑成功返回编辑后的 Message，inline 编辑返回 True；媒体组返回多个 Message。文本上限为解析实体后 4096 字符。[发送](https://core.telegram.org/bots/api#sendmessage)、[编辑](https://core.telegram.org/bots/api#editmessagetext)、[媒体组](https://core.telegram.org/bots/api#sendmediagroup)
- 回复目标不存在时，`allow_sending_without_reply` 可允许无引用发送；跨 chat/Topic 回复不支持此退路。不可访问消息类型保留 chat、message ID，date 为零。[回复参数](https://core.telegram.org/bots/api#replyparameters)、[不可访问消息](https://core.telegram.org/bots/api#inaccessiblemessage)
- 群升级提供迁入/迁出 chat ID。待接收 updates 最长保留 24 小时，这不是聊天历史保留期。官方没有普通群全历史枚举接口；新增个人主页聊天查询也只返回有限近期消息。[迁移字段](https://core.telegram.org/bots/api#message)、[updates](https://core.telegram.org/bots/api#getting-updates)、[个人主页聊天查询](https://core.telegram.org/bots/api#getuserpersonalchatmessages)

## 当前适配器事实

以下源码链接固定在基线提交，避免后续变动造成引用漂移。

- `handleMessage` 先构造当前用户/Topic 的 SessionKey；`dispatchMessage` 将回复内容附加为文本。原消息 ID 未作为路由字段传给 core；`isDirectedAtBot` 仅用回复作者判定是否对 Bot 发言。[入口与分发](https://github.com/keyding/cc-connect/blob/016669df6db6e8a04035afa6e33c8845a34b1968/platform/telegram/telegram.go#L405)、[引用内容](https://github.com/keyding/cc-connect/blob/016669df6db6e8a04035afa6e33c8845a34b1968/platform/telegram/telegram_reply.go#L12)
- Reply、Send、图片、文件、音频和按钮发送大多丢弃成功响应。分块逐条发送，只首块附带引用或按钮，后块也丢弃 ID。预览保留 chat/thread/message 句柄，后续编辑同条消息，未在这些路径保存会话归属。[发送与媒体](https://github.com/keyding/cc-connect/blob/016669df6db6e8a04035afa6e33c8845a34b1968/platform/telegram/telegram.go#L1041)、[预览与分块](https://github.com/keyding/cc-connect/blob/016669df6db6e8a04035afa6e33c8845a34b1968/platform/telegram/telegram.go#L1425)
- 启动排空接口读取并丢弃积压 update；入口另有过旧消息过滤。因此当前启动流程也不是历史回填手段。[启动处理](https://github.com/keyding/cc-connect/blob/016669df6db6e8a04035afa6e33c8845a34b1968/platform/telegram/telegram.go#L287)

## 推断与未解项

推断：未来可在每次成功发送时记录每条有效消息 ID 与内部会话的关联；一条回答分块后不是一个 ID。引用文字本身不能证明内部会话身份。上线前未记录的旧消息、发送成功但响应丢失的消息，无法仅靠上述接口完整重建归属。

待决策：映射保存多久、跨 Topic 引用缺标识如何提示、群迁移旧消息映射如何衔接，以及发送成功与本地落盘之间崩溃如何呈现。文档未保证群迁移前后消息 ID 可机械换算，也未保证普通群删除事件完整推送；这些不能作为自动清理依据。本研究不宣称经过真实客户端或迁移验收。
