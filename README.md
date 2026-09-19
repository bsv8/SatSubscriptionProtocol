# SSP V1

SSP（Sat Subscription Protocol）是运行在外层已认证长期连接上的最小订阅广播协议。SDK
只负责 deterministic CBOR、JSON 校验、订阅状态和 Backend/Dispatcher 调用边界，不建立
HTTP、WebSocket、WebRTC 或 libp2p 连接。

外部应用负责连接、身份认证、分帧、写回响应、在线连接注册和实际广播发送。SDK 从外层
取得 remote_public_key（已认证的 33 字节压缩公钥形状）和一个完整 SSP Wire。

libp2p + SSP 身份域与独立 CP 内容域的整体消融设计见
[`docs/SSP-CP协议族消融.md`](docs/SSP-CP协议族消融.md)。该文档记录已实施边界和验收状态；
下文说明当前实现。

## Wire 与响应

完整 Wire 固定为：[wire_version, wire_kind, document_cbor]。document_cbor 是内层
文档的完整 deterministic CBOR 字节，并作为 exact bstr 放入外层。

| wire_kind | 英文类型 | 中文含义 | 方向 |
|---:|---|---|---|
| 1 | Publish | 发布/广播 channel 与 JSON 原始字节 | 双向 |
| 2 | Subscribe | 订阅普通 channel 或 * 全频道 | 客户端 → 服务端 |
| 3 | Unsubscribe | 取消普通 channel 或 * 全部订阅 | 客户端 → 服务端 |
| 4 | SubscriptionsRequest | 查询当前订阅集合 | 客户端 → 服务端 |
| 5 | SubscriptionsResponse | 返回订阅集合和实际扣费金额 | 服务端 → 客户端 |
| 6 | ActionResult | 返回动作成功/失败和实际扣费金额 | 服务端 → 客户端 |
| 7 | BillingRequest | 查询当前已认证身份自己的账单 | 客户端 → 服务端 |
| 8 | BillingResponse | 返回账单分页记录或错误 | 服务端 → 客户端 |

五种客户端请求的内层文档都以 request_id（固定 32 字节请求编号）开始：

~~~text
Publish                = [request_id, channel, content_json]
Subscribe              = [request_id, channel]
Unsubscribe            = [request_id, channel]
SubscriptionsRequest  = [request_id]
SubscriptionsResponse  = [request_id, charged_amount, channels]
ActionResult            = [request_id, success, charged_amount, error_code]
BillingRequest          = [request_id, from_ms, to_ms, limit, cursor]
BillingResponse         = [request_id, success, currency, network, records, next_cursor, error_code]
ChargeRecord            = [charge_id, occurred_at_ms, action, channel, source_request_id, charged_amount]
~~~

request_id 只在同一条 Stream 内关联请求和响应，不是广播消息 ID。Session 只在当前 Stream
内抑制重复编号，不保存跨 Stream 的结果；生产 Backend/Dispatcher 必须为可收费动作按
`(已认证 payer_id, request_id)` 索引并保存 deterministic SSP Wire 的 SHA-256
`request_digest`。只有摘要也相同才是重试；同一付款人和 request_id 但 channel、content 或
Kind 不同必须返回 `REJECTED`，不能复用收费结果。不同付款人使用独立的收费键。Publish、Subscribe、Unsubscribe 成功返回 ActionResult；
SubscriptionsRequest 成功返回唯一的 SubscriptionsResponse；BillingRequest 成功返回唯一的 BillingResponse。动作和订阅列表成功结果携带本次
实际 charged_amount。账单查询的 from_ms 包含、to_ms 不包含，limit 范围为 1..100，第一页
cursor 为空；TypeScript 时间字段使用 bigint。BillingResponse.records 只返回实际正扣费
记录，免费和失败操作不返回，查询身份取自外层认证公钥。

目标广播边界中，上一句的 Publish 响应只适用于客户端 → Supplier 请求。Supplier 向订阅者
交付时生成新的 delivery request_id，保持 channel，并逐字节复用 CP content_json；订阅者将
其作为事件处理，不返回 ActionResult。完整第一跳 SSP Wire 不会直接转发。

Publish/Subscribe/Unsubscribe/SubscriptionsRequest 失败如果能安全读出 request_id，返回失败
ActionResult；BillingRequest 失败返回失败 BillingResponse，且都不关闭 Stream：

| error_code | 中文含义 |
|---|---|
| INVALID_REQUEST | 编号可读，但后续业务字段无效 |
| REJECTED | 格式正确，但被余额、权限或服务策略拒绝 |
| INTERNAL_ERROR | Backend、Dispatcher 或服务端内部失败 |

外层损坏、缺少或错误长度的 request_id、身份无效和 Session 达到编号上限时，SDK 返回
本地错误，不伪造响应；是否关闭连接由外部应用决定。

## 精确金额与固定限制

charged_amount 是 ASCII 规范十进制字符串，不使用 float32、float64 或 JavaScript
number。允许 "0"、"12"、"0.001"、"0.123456789012345678"，最多 18 位
小数、最多 64 字节；禁止负号、指数、额外前导零和小数末尾零。失败固定为 "0"。

收费价格由使用 SDK 的服务程序配置；SDK 不读取价格配置、不调用 SPI。Backend/Dispatcher
成功返回的金额必须是实际扣除金额，检查、扣费和状态变更的原子事务由接口实现保证。
SPI 的整数余额与 SSP 小数金额换算规则见 [SPI 集成与金额换算](docs/SPI集成与金额换算.md)。

冻结限制（Go protocol 与 TypeScript protocol 值一致）：

| Go 常量 | TypeScript 常量 | 数值 | 中文含义 |
|---|---|---:|---|
| RequestIDBytes | REQUEST_ID_BYTES | 32 | request_id 固定字节数 |
| MaxAmountBytes | MAX_AMOUNT_BYTES | 64 | 金额 ASCII 字节上限 |
| MaxAmountScale | MAX_AMOUNT_SCALE | 18 | 金额最大小数位数 |
| MaxRequestIDsPerSession | MAX_REQUEST_IDS_PER_SESSION | 65536 | 单 Stream 编号登记上限 |
| MaxWireBytes | MAX_WIRE_BYTES | 1048576 | 完整 Wire 最大字节数 |
| MaxDocumentBytes | MAX_DOCUMENT_BYTES | 1048512 | 内层 document 最大字节数 |
| MaxChannelUTF8Bytes | MAX_CHANNEL_UTF8_BYTES | 256 | channel UTF-8 最大字节数 |
| MaxContentJSONBytes | MAX_CONTENT_JSON_BYTES | 1048000 | JSON 原始字节最大长度 |
| MaxSubscriptions | MAX_SUBSCRIPTIONS | 1024 | 订阅集合最大元素数 |
| MaxBillingLimit | MAX_BILLING_LIMIT | 100 | 一次账单查询最多请求的记录数 |
| MaxBillingRecords | MAX_BILLING_RECORDS | 100 | 一页账单响应最多返回的记录数 |
| MaxBillingTextBytes | MAX_BILLING_TEXT_BYTES | 256 | 账单标识、动作、货币和网络文本上限 |
| MaxBillingCursorBytes | MAX_BILLING_CURSOR_BYTES | 256 | 分页游标 UTF-8 字节上限 |

普通 channel 精确匹配、区分大小写，不裁剪、不做 Unicode 归一化。* 只能用于订阅
操作或作为唯一的订阅列表元素；Publish 不能使用 *。content_json 验证为一份完整
合法 JSON 后仍保留原始 UTF-8 字节。

## Go 使用

~~~go
ctx := context.Background()
backend := subscription.NewMemoryBackend()
dispatcher := service.NewRecordingDispatcher()
session := service.NewSession(remotePublicKey, backend, dispatcher)
defer session.Close()

subscribeID, _ := client.NewRequestID()
publishID, _ := client.NewRequestID()
subscribeWire, _ := client.NewSubscribe(subscribeID, "news")
publishWire, _ := client.NewPublish(publishID, "news", []byte("{\"title\":\"hello\"}"))

// 不同 request_id 可以并行；外部应用按响应中的 request_id 关联结果，
// 并用一个串行 writer 写回完整响应，避免响应字节交错。
go func() { result, err := session.HandleMessage(ctx, subscribeWire); _ = result; _ = err }()
go func() { result, err := session.HandleMessage(ctx, publishWire); _ = result; _ = err }()
~~~

Go Result 的 HasResponse 表示是否需要写回 Response，Suppressed 表示同一
Session 内相同编号是否被静默丢弃。业务失败仍有失败 ActionResult；Cause 只用于本地
诊断，调用方应先写回 Response，再用 errors.Is/As 检查 Cause。HandleMessage 的 error
只表示无法安全构造响应的本地错误；业务失败不会通过非空 error 阻止响应写回。

## TypeScript 使用

~~~ts
const session = new Session(remotePublicKey, backend, dispatcher);
const subscribeId = newRequestId();
const publishId = newRequestId();

const [subscribeResult, publishResult] = await Promise.all([
  session.handleMessage(newSubscribe(subscribeId, "news")),
  session.handleMessage(newPublish(
    publishId,
    "news",
    new TextEncoder().encode("{\"title\":\"hello\"}"),
  )),
]);

const subscribeAction = parseActionResult(subscribeResult.response);
const publishAction = parseActionResult(publishResult.response);
// 通过 subscribeAction.requestId / publishAction.requestId 匹配乱序响应，
// chargedAmount 是字符串，不能转换成 number。
session.close();
~~~

所有公开构造函数都会校验并复制 requestId、JSON 字节和 Wire 字节。TypeScript
SSPError.code 提供稳定本地分类，业务失败的 TypeScript SessionResult.cause 使用标准
Error.cause 保留原始异常；这些本地信息不会进入 ActionResult。

## 目录与验收

- spec/v1/ssp.cddl：V1 CDDL、字段和固定限制；
- protocol/、wire/、client/、subscription/、service/：Go SDK；
- typescript/：sat-subscription-protocol TypeScript SDK；
- testdata/v1/：Go 与 TypeScript 共用的八种 Wire、状态、dispatch、billing、invalid fixture；
- [ChannelProtocol](https://github.com/bsv8/ChannelProtocol)：独立的三方频道协议及 Go/TypeScript SDK；
- docs/SPI集成与金额换算.md：SPI 整数余额与 SSP 精确小数金额的应用层换算；
- integration/fixture/、scripts/test-integration.sh：无网络跨语言检查；
- examples/：不启动网络的 Go/TypeScript 调用示例。

~~~text
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
cd typescript && npm ci && npm test && npm run build && npm pack --dry-run
./scripts/validate-cddl.sh
./scripts/test-integration.sh
./scripts/test-examples.sh
~~~
