# libp2p + SSP / CP 协议族消融（SSP 侧）

状态：SSP SDK 消融和 SatSubscription 生产组合已施工并完成当前仓库验收；外部消费者待验收。

跨仓库完整设计以
[`ChannelProtocol/docs/SSP-CP协议族消融设计.md`](../../ChannelProtocol/docs/SSP-CP协议族消融设计.md)
为真值，施工以
[`004-SSP-CP协议族消融施工单`](../../ChannelProtocol/施工单/20260902/004-SSP-CP协议族消融施工单.md)
为真值。

## 1. libp2p 与 SSP 是一个身份域

- libp2p 已认证公钥就是 SSP 调用者和付款人；
- SSP 从连接/Session 上下文取得该身份；
- SSP Wire 不携带 sender、publisher 或 payer 公钥；
- SSP 不增加业务签名；
- Backend/Dispatcher 的扣费身份只能来自连接上下文，不能来自 Wire。

这就是 SSP 层的唯一真值：不在连接公钥之外再声明一次 SSP 发送者。

## 2. CP 是独立内容域

CP 自己保留：

- `publisher_public_key`；
- `message_id`；
- `issued_at_ms` / `expires_at_ms`；
- `signature`；
- 独立的严格 JSON、签名、加密、去重和关联规则。

CP 报文作为 exact `content_json` 放入 SSP Publish。CP 不依赖 SSP，离开原连接后仍能
验签。

## 3. A 可以提交 B 的报文

```text
libp2p/SSP identity A → 调用、ActionResult、扣费
CP publisher B        → 内容作者、验签、内容去重
```

Supplier：

1. 从 libp2p 连接认证 A；
2. SSP 解析 request_id、channel 和 exact content_json；
3. CP 使用报文内的 B 验签；
4. 不比较 A 与 B；
5. 按 A 的账户扣费；
6. 交付 B 的 exact CP content_json。

对于 Inbox，Supplier 没有接收者私钥，因此只能检查可见信封；最终 B 的内层签名由
接收者解密后验证。

## 4. 两个编号不合并

- SSP `request_id`：A 与 Supplier 的请求/ActionResult 和收费幂等；
- CP `message_id`：B 的签名内容、内容重试、去重和协议关联。

同一 CP message_id 可以通过新的 SSP 请求或不同 libp2p 身份代发。是否再次收费属于
Supplier 策略，不属于 CP 协议。

## 5. Dispatcher

```text
从 libp2p 取得 caller/payer A
  → SSP 解析 request_id/channel/exact content_json
  → CP 做 Supplier 能完成的 channel 检查
  → Supplier 策略
  → 按 A、request_id 和 deterministic Wire 摘要检查幂等并扣费
  → 交付 exact content_json
  → 向 A 返回 ActionResult
```

不增加 `ReviewPublishAdmission`、Validator、Router 或 Middleware。SSP Core 不依赖
CP；生产程序在现有具体 Dispatcher 中组合两个库。

## 6. 原样转发只指 CP 字节

确定规则：

- CP `content_json` 必须逐字节原样交付；
- channel 必须相同，因为 CP 签名域绑定实际 channel；
- Supplier 不重排 JSON、不替 B 重签、不重新加密；
- 完整 SSP Wire 不要求相同，也不应直接跨 Stream 转发；
- A → S 使用 A 的原 request_id，并在原流接收 ActionResult；
- S → R 使用 S 为订阅者 Stream 新生成的 delivery request_id；
- R 把 Supplier 订阅交付路径上的 Publish 当作事件，不返回 ActionResult；
- 两跳仍使用相同 Publish Kind 和 document 形状，不新增 Publication Kind。

第一跳 request_id 不能进入第二跳。delivery request_id 不进入 CP 签名、内容去重或关联。

## 7. 版本

- 项目包/SDK 版本：`0.3.0`；
- SSP WireVersion：仍为 `1`；
- SSP Kind 数值：不变；
- CP protocol 和 envelope 版本：不变；
- 不为未发布旧形状建立兼容层。

## 8. SSP 侧验收

- Wire 中不存在身份公钥和业务签名；
- 付款人只来自已认证 libp2p 连接；
- A 提交 B 的合法 CP 报文能够通过；
- 账本向 A 而不是 B 扣费；
- SSP request_id 与 CP message_id 保持独立；
- 第二跳 channel 与输入相同，CP content_json 与输入逐字节相同；
- 收费操作保存 deterministic SSP Wire 的 32 字节 SHA-256 `request_digest`；同一付款人和
  request_id 的不同 channel、content 或 Kind 返回 `REJECTED`；
- 第一跳和第二跳 request_id 独立，完整 SSP Wire 不要求相同；
- 订阅者交付不产生 SSP ActionResult。
