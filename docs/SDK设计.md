# SSP SDK 设计

> libp2p + SSP 身份域与独立 CP 内容域的跨仓库消融目标见
> [SSP + CP 协议族消融](./SSP-CP协议族消融.md)。本文其余部分仍是当前实现说明。

## 1. 边界和目标

Go 与 TypeScript SDK 项目包版本均为 0.2.0；SSP 协议版本仍为 V1。SDK 负责：

- deterministic CBOR 编解码和严格字段校验；
- content_json 完整 JSON 与 exact bytes 保留；
- channel、* 和订阅集合规范化；
- Session 的 request_id 登记、重复抑制和并发边界；
- Backend/Dispatcher 的调用结果封装。

SDK 不负责认证、网络、分帧、连接生命周期、真实广播、SPI 或计费配置文件。外层应用向
SDK 交付已认证 remote_public_key 和完整 Wire。

## 2. Wire 文档

外层固定为：

~~~text
[wire_version, wire_kind, document_cbor]
~~~

document_cbor 是内层 deterministic CBOR 完整字节，以 bstr 放入外层。六种 Kind：

| Kind | 名称 | 中文含义 |
|---:|---|---|
| 1 | Publish | 发布请求或广播交付 |
| 2 | Subscribe | 订阅普通 channel 或 * |
| 3 | Unsubscribe | 取消普通 channel 或全部订阅 |
| 4 | SubscriptionsRequest | 查询订阅集合 |
| 5 | SubscriptionsResponse | 返回订阅集合和实际扣费 |
| 6 | ActionResult | 返回动作成功/失败和实际扣费 |

内层数组固定为：

~~~text
Publish               = [request_id, channel, content_json]
Subscribe             = [request_id, channel]
Unsubscribe           = [request_id, channel]
SubscriptionsRequest = [request_id]
SubscriptionsResponse = [request_id, charged_amount, channels]
ActionResult           = [request_id, success, charged_amount, error_code]
~~~

所有 request_id 都是固定 32 字节 bstr。响应逐字节回显请求编号。成功动作的金额是接口
返回的实际扣费；失败固定 "0"。Publish、Subscribe、Unsubscribe 成功返回 ActionResult，
查询成功返回唯一 SubscriptionsResponse。

解码先检查外层版本、Kind、document bstr 和 canonical bytes，再在服务 Session 中读取
request_id，最后校验具体业务字段。这样后续 channel/JSON/字段组合错误可返回
INVALID_REQUEST，而没有安全编号的输入不会伪造响应。

## 3. 金额和错误

Amount 是规范十进制字符串，不提供 float 构造接口。金额最多 64 个 ASCII
字节、18 位小数，禁止符号、指数、前导零和小数末尾零；免费为 "0"，小额如 "0.001"
按原字符串跨语言传递。

ActionResult 的线上错误码只有 INVALID_REQUEST、REJECTED、INTERNAL_ERROR。本地
错误仍使用稳定分类：INVALID_WIRE、UNSUPPORTED_VERSION、UNSUPPORTED_KIND、
NON_CANONICAL_CBOR、MESSAGE_TOO_LARGE、INVALID_CHANNEL、INVALID_JSON、
INVALID_SUBSCRIPTIONS、INVALID_PUBLIC_KEY、BACKEND_FAILURE、DISPATCH_FAILURE、
SESSION_LIMIT、SESSION_CLOSED。

Backend/Dispatcher 业务失败会生成线上失败 ActionResult，同时保留本地 cause。Go 的
Result.Cause 使用多重 %w 包装，errors.Is/As 可以同时找到稳定 Backend/Dispatcher
分类和原始错误；此时返回 error 为 nil，避免连接层跳过响应。TypeScript 的
SessionResult.cause 是 SSPError，原始异常位于标准 Error.cause。cause、堆栈和数据库
文本绝不进入 Wire。

## 4. Session 并发模型

~~~text
收到完整 Wire
  → 外层解析 + 读取 request_id
  → Session 锁内检查/登记编号
  → 释放锁
  → DecodeRequest 与 Backend/Dispatcher 业务调用
  → 生成一条响应或标记静默丢弃
~~~

锁只保护编号集合，不串行化不同编号的业务调用。相同编号的并发到达只有第一次登记者
执行，其他调用返回 Suppressed=true / { suppressed: true }，没有响应且不返回历史结果。
不同编号可以并行，外部传输应用必须用单独的串行 writer 写响应。

每个 Session 最多登记 65536 个不同编号。上限错误是本地 SESSION_LIMIT，不编码为
ActionResult。Close 释放集合；关闭后新调用返回 SESSION_CLOSED，已开始的 Backend/
Dispatcher 调用不会被 SDK 强制取消。

## 5. 状态和事务接口

Go：

~~~go
type Backend interface {
    Subscribe(ctx context.Context, publicKey protocol.PublicKey, requestID protocol.RequestID, channel string) (protocol.Amount, error)
    Unsubscribe(ctx context.Context, publicKey protocol.PublicKey, requestID protocol.RequestID, channel string) (protocol.Amount, error)
    Subscriptions(ctx context.Context, publicKey protocol.PublicKey, requestID protocol.RequestID) (channels []string, chargedAmount protocol.Amount, err error)
}

type Dispatcher interface {
    Dispatch(ctx context.Context, event PublishEvent) (chargedAmount protocol.Amount, err error)
}
~~~

TypeScript 的 Backend 与 Dispatcher 使用 RequestId 和 Amount，方法名
分别为 subscribe、unsubscribe、subscriptions、dispatch。成功金额必须是实际扣除
金额，不是报价。

Backend 必须在同一事务边界内完成检查、收费和状态变更；查询的列表和收费也必须来自同一
事务快照。SDK 不实现数据库事务，只验证并规范化 Backend 返回的列表。

Session 的 `seen` 集合只提供当前 Stream 的快速抑制，不是持久化账本。生产 Backend/Dispatcher
必须用已认证的 `payer_id` 与 `request_id` 索引收费操作，并保存 deterministic SSP Wire 的
SHA-256 `request_digest`；三者全部相同才返回首次保存的原始收费结果。同一付款人和
request_id 的不同 channel、content 或 Kind 必须映射为 `REJECTED`，不同付款人的相同
request_id 独立收费。该契约复用现有接口参数，不增加 IdempotencyManager 或其他中间层。

`PublishEvent` 只保留请求 request_id、已认证调用者/付款公钥、channel 和 exact
content_json，不保留完整入站 Wire。生产 Dispatcher 为每个订阅者 Stream 生成新的 delivery
request_id，保持 channel 和 content_json 字节；订阅交付路径不生成 ActionResult。该边界已在
SatSubscription 的实际 Server/Dispatcher 组合和集成测试中落地。

## 6. 对外服务接口

Go：

~~~go
session := service.NewSession(remotePublicKey, backend, dispatcher)
result, err := session.HandleMessage(ctx, completeWire)
~~~

result.Response 是需要写回的完整 Wire，result.HasResponse 区分“有响应”和“无响应”，
result.Suppressed 表示重复编号静默丢弃，result.Cause 是本地诊断错误。业务失败同时
有失败 ActionResult 和本地 Cause，但返回 error 为 nil；外部应用应先按 HasResponse
写响应，再记录 Cause。格式错误、身份错误、Session 上限和关闭状态没有伪造响应，返回
非空 error。

TypeScript：

~~~ts
const session = new Session(remotePublicKey, backend, dispatcher);
const result = await session.handleMessage(completeWire);
~~~

返回 { response?: Uint8Array, suppressed: boolean, cause?: SSPError }。TypeScript SDK 只保留
Session 入口；长期 Stream 必须复用同一 Session，订阅交付也不得把出站 Publish 再送入该入口。

## 7. 严格编码和限制

- 只使用定长数组、uint、bool、text string 和 byte string；
- 禁止 map、tag、不定长数组、错误字段数量和尾随字节；
- 所有 CBOR 头必须为最短编码，重编码必须与输入逐字节相同；
- Publish 的 JSON 先验证 UTF-8 和完整 RFC 8259 JSON，再保留原始字节；
- channels 必须无重复、按 UTF-8 字节升序，* 只能单独出现；
- 解码前检查 Wire/document/字段固定上限。

Go 与 TypeScript 的冻结值：request_id 32 字节、金额 64 字节/18 位小数、Session 65536、
Wire 1 MiB、document 1048512 字节、channel 256 字节、JSON 1048000 字节、订阅集合
1024 个元素。

## 8. 测试和依赖

共享 fixture 覆盖六种 Kind、请求编号回显、精确金额、非法金额、响应组合、并发去重、
编号上限、cause、JSON exact bytes、订阅状态和跨语言字节一致性。SDK 依赖树不引入 SPI、
HTTP、WebSocket 或 bitcoin-libp2p 类型。
