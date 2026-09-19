# SSP SDK 使用说明

## 1. 客户端构造

每个请求先由调用方生成唯一的 32 字节 request_id，再传给构造函数。SDK 不替调用方
自动生成编号，这样并行请求可以由应用明确管理和关联。

Go：

~~~go
subscribeID, _ := client.NewRequestID()
publishID, _ := client.NewRequestID()

subscribeWire, err := client.NewSubscribe(subscribeID, "news")
publishWire, err := client.NewPublish(publishID, "news", []byte("{\"title\":\"hello\"}"))
~~~

正式签名：

~~~go
client.NewPublish(requestID, channel, contentJSON) ([]byte, error)
client.NewSubscribe(requestID, channel) ([]byte, error)
client.NewUnsubscribe(requestID, channel) ([]byte, error)
client.NewSubscriptionsRequest(requestID) ([]byte, error)
client.NewBillingRequest(requestID, fromMS, toMS, limit, cursor) ([]byte, error)
client.ParsePublish(wireCBOR) (protocol.Publish, error)
client.ParseActionResult(wireCBOR) (protocol.ActionResult, error)
client.ParseSubscriptionsResponse(wireCBOR) (protocol.SubscriptionsResponse, error)
client.ParseBillingResponse(wireCBOR) (protocol.BillingResponse, error)
~~~

TypeScript：

~~~ts
const subscribeId = newRequestId();
const publishId = newRequestId();
const subscribeWire = newSubscribe(subscribeId, "news");
const publishWire = newPublish(
  publishId,
  "news",
  new TextEncoder().encode(' {"title":"hello"} '),
);
~~~

对应签名为 newPublish(requestId, channel, contentJson)、newSubscribe(requestId,
channel)、newUnsubscribe(requestId, channel)、newSubscriptionsRequest(requestId)、
newBillingRequest(requestId, fromMs, toMs, limit, cursor)、parsePublish、parseActionResult、
parseSubscriptionsResponse 和 parseBillingResponse。newRequestId()
使用 Web Crypto/Node 的安全随机源，不使用 Math.random()。

账单查询的 fromMs/toMs 使用 Unix 毫秒；TypeScript API 使用 bigint，Go API 使用 uint64。
fromMs 包含、toMs 不包含，limit 范围是 1..100，第一页 cursor 传空字符串。BillingResponse
成功时返回 currency、network、records 和 nextCursor；records 只包含实际正扣费记录，
免费操作和失败操作不返回。

content_json 必须是完整合法 JSON 的 UTF-8 字节；SDK 验证但不重排 JSON，收到的原始
空白和字节会保留。Publish 不能使用 *；Subscribe/Unsubscribe 可以使用 *。

## 2. 长期 Stream 与并行响应

长期连接为每条 Stream 建立一个 Session：

~~~go
session := service.NewSession(remotePublicKey, backend, dispatcher)
defer session.Close()

var wait sync.WaitGroup
for _, payload := range [][]byte{subscribeWire, publishWire} {
    wait.Add(1)
    go func(payload []byte) {
        defer wait.Done()
        result, err := session.HandleMessage(context.Background(), payload)
        if err != nil {
            // 无法安全构造响应；是否结束连接由外部应用决定。
            return
        }
        if result.HasResponse {
            // 外部应用用一个串行 writer 写 result.Response。
        }
        if result.Cause != nil {
            // 业务失败已有 ActionResult；只记录本地 Cause。
            _ = errors.Is(result.Cause, protocol.ErrBackendFailure)
        }
    }(payload)
}
wait.Wait()
~~~

TypeScript：

~~~ts
const session = new Session(remotePublicKey, backend, dispatcher);
const [subscribeResult, publishResult] = await Promise.all([
  session.handleMessage(subscribeWire),
  session.handleMessage(publishWire),
]);
~~~

不同 request_id 的业务可以并行，响应可以乱序；外部连接应用必须用一个串行 writer
写出完整响应，客户端通过响应中的 request_id 关联，而不能依赖响应顺序。

成功响应规则：

- Publish、Subscribe、Unsubscribe：解析 ActionResult；
- SubscriptionsRequest：解析唯一 SubscriptionsResponse；
- BillingRequest：解析唯一 BillingResponse；
- Publish/Subscribe/Unsubscribe/SubscriptionsResponse 成功结果都有实际 charged_amount，金额是字符串，不转换成浮点数；BillingResponse 将金额放在 records.charged_amount 中；
- Publish 成功只表示服务端已接受、已扣费并开始广播，不代表任何订阅者已收到。

上述 Publish ActionResult 只属于客户端 → Supplier 请求。生产 Dispatcher 向订阅者发送时：

- 为订阅者 Stream 生成新的 delivery request_id，不复制第一跳 request_id；
- channel 保持相同；
- content_json 直接复用收到的 CP 原始字节，不 parse/stringify；
- 订阅者把 Publish 当作交付事件，不返回 ActionResult；
- 完整两跳 SSP Wire 不要求相同。

如果 Subscribe 后要查询更新后的集合，应先收到对应 ActionResult，再发送查询请求。不同
编号可并行不等于存在依赖的操作会自动排序。

## 3. 重复编号和结果字段

同一 Session 中重复提交相同编号会静默丢弃：不进入 Backend/Dispatcher、不扣费、不响应、
不返回第一次结果。结果字段：

~~~text
Go:
  Response    []byte   // 需要写回的完整 Wire
  HasResponse bool     // 是否存在响应
  Suppressed  bool     // 是否因重复 request_id 丢弃
  Cause       error    // 仅本地诊断，不进入 Wire

TypeScript:
  response?: Uint8Array // 需要写回的完整 Wire
  suppressed: boolean  // 是否因重复 request_id 丢弃
  cause?: SSPError      // 仅本地诊断，不进入 Wire
~~~

同一编号在新 Stream 中是新请求。每个 Session 最多登记 65536 个不同编号，第 65537 个
新编号返回本地 SESSION_LIMIT；外部应用负责结束该 Stream。Close 后调用返回 SESSION_CLOSED。

## 4. Backend、Dispatcher 与错误

Go 接口：

~~~go
type Backend interface {
    Subscribe(ctx, publicKey, requestID, channel) (chargedAmount protocol.Amount, err error)
    Unsubscribe(ctx, publicKey, requestID, channel) (chargedAmount protocol.Amount, err error)
    Subscriptions(ctx, publicKey, requestID) (channels []string, chargedAmount protocol.Amount, err error)
    ListChargeRecords(ctx, publicKey, request protocol.BillingRequest) (page protocol.BillingPage, err error)
}

type Dispatcher interface {
    Dispatch(ctx, event PublishEvent) (chargedAmount protocol.Amount, err error)
}
~~~

TypeScript Backend 的方法是 subscribe(publicKey, requestId, channel)、
unsubscribe(publicKey, requestId, channel)、subscriptions(publicKey, requestId)、
listChargeRecords(publicKey, request)；其中 request 使用 BillingRequest，成功返回
BillingPage；
Dispatcher.dispatch(event) 返回实际 Amount。Backend/Dispatcher 自己保证收费与状态
变更的原子事务，SDK 不读取价格文件和 SPI。Session 只做当前 Stream 的重复抑制；生产
实现还必须按 `(payer_id, request_id)` 索引并保存 deterministic SSP Wire 的 SHA-256
`request_digest`：摘要相同的跨 Stream 重试返回原结果，摘要不同映射为 `REJECTED`；不同
付款人的相同 request_id 独立收费。

业务失败仍返回失败 ActionResult；BillingRequest 业务失败返回字段固定的失败
BillingResponse：

| ActionResult | 含义 |
|---|---|
| INVALID_REQUEST | request_id 可读但业务字段无效 |
| REJECTED | 余额、权限或策略拒绝，不扣费 |
| INTERNAL_ERROR | Backend、Dispatcher 或内部错误，不泄漏细节 |

Go 的 Cause 会包装 ErrBackendFailure 或 ErrDispatchFailure 并保留原始错误，使用
errors.Is/As 检查。业务失败时 Result.HasResponse 为 true、Result.Cause 非空，但返回
error 为 nil；只有无法安全构造响应的错误才返回非空 error。TypeScript 的 cause 是
SSPError，原异常在标准 Error.cause。本地错误和 cause 不编码进 Wire。

金额与 SPI 的整数余额换算见 [SPI 集成与金额换算](./SPI集成与金额换算.md)。

## 5. 字段对照

| 线上字段 | 类型 | 中文含义 |
|---|---|---|
| wire_version | CBOR uint | SSP Wire 版本，V1 为 1 |
| wire_kind | CBOR uint | 八种消息类型编号 |
| request_id | CBOR bstr，32 字节 | 同一 Stream 内关联请求与响应 |
| channel | CBOR tstr | 精确匹配的 channel；* 表示全频道操作 |
| content_json | CBOR bstr | 完整合法 JSON 的原始 UTF-8 字节 |
| charged_amount | CBOR tstr | 实际扣费的精确十进制字符串 |
| success | CBOR bool | ActionResult 动作是否成功 |
| error_code | CBOR tstr | INVALID_REQUEST/REJECTED/INTERNAL_ERROR |
| channels | CBOR tstr 数组 | 无重复、按 UTF-8 字节升序的订阅集合 |
| from_ms | CBOR uint | 账单开始时间，Unix 毫秒，包含 |
| to_ms | CBOR uint | 账单结束时间，Unix 毫秒，不包含 |
| limit | CBOR uint | 账单单页数量，1..100 |
| cursor | CBOR tstr | 不透明分页游标，第一页为空 |
| currency | CBOR tstr | 成功账单页的货币标识 |
| network | CBOR tstr | 成功账单页的网络标识 |
| records | CBOR array | 实际正扣费记录列表 |
| next_cursor | CBOR tstr | 下一页游标；没有下一页时为空 |
| charge_id | CBOR tstr | 账单记录编号，当前映射 operation_id |
| occurred_at_ms | CBOR uint | 扣费发生时间，Unix 毫秒 |
| action | CBOR tstr | 产生扣费的 SSP 动作 |
| source_request_id | CBOR bstr，32 字节 | 产生扣费的 SSP 请求编号 |
| remote_public_key | 外层认证输入 | 订阅状态所属身份，不进入 SSP Wire |
