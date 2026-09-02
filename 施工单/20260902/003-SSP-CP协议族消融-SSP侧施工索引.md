# 003 - SSP + CP 协议族消融（SSP 侧施工索引）

状态：已施工并完成 SSP 仓库验收；SatSubscription 生产组合已覆盖，外部消费者待验收。

跨仓库唯一施工单位于
[`ChannelProtocol/施工单/20260902/004-SSP-CP协议族消融施工单.md`](../../../ChannelProtocol/施工单/20260902/004-SSP-CP协议族消融施工单.md)。
本仓库不维护第二份施工细节，避免字段、阶段和验收门槛漂移。

SSP 侧对应总施工单：

- 阶段 0：盘点 libp2p → SSP 付款身份流、Wire、fixture 和消费者；
- 阶段 1：冻结“SSP 身份来自连接、CP 身份来自报文”的边界向量；
- 阶段 2：审计 SSP Wire 无公钥/签名、Backend 只按连接身份扣费，并让 PublishEvent 删除
  完整入站 Wire，只保留 exact content_json 和必要上下文；
- 阶段 6：配合生产 SatSubscription 的具体 Dispatcher 组合与 exact content_json；
- 阶段 7：冻结 SSP request_id 的收费幂等，不与 CP message_id 合并；持久化记录同时保存
  deterministic SSP Wire 的 SHA-256 `request_digest`，摘要冲突返回 `REJECTED`；
- 阶段 8：两跳分别包装 SSP Wire，只原样交付 CP content_json；
- 阶段 9：迁移 SSP 消费者；
- 总施工单第 14 节：冻结版本并发布 `0.2.0` 项目包。

禁止事项：

- 不在 SSP Wire 增加身份公钥或业务签名；
- 不要求 libp2p/SSP 身份等于 CP 作者；
- 不用 CP 作者字段选择扣费账户；
- 不合并 SSP request_id 与 CP message_id；
- 不修改 `WireVersion = 1` 或其他协议版本；
- 不建立 V2 协商和未发布旧形状兼容层；
- 不把第一跳 request_id 带入订阅者 Stream；
- 不要求两跳完整 SSP Wire 相同；
- 第二跳必须保持 channel 和 CP content_json 字节；
- 不回退当前已写代码。

任何实施必须同时阅读
[`docs/SSP-CP协议族消融.md`](../../docs/SSP-CP协议族消融.md)
和总施工单的停工条件。
