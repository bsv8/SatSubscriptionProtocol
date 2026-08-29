# SSP SDK 设计

## 1. 目标

Go 与 TypeScript SDK 当前版本均为 `0.1.0`；Go `protocol.SDKVersion` 与 TypeScript
`protocol` 子入口的 `SDK_VERSION` / `SDKVersion` 必须保持一致。

SSP SDK 是协议的规范实现。第三方程序引用 SDK 后，不自行实现：

- CBOR 编解码和严格校验
- JSON content 校验
- `*` 的订阅集合语义
- Subscriptions 返回的规范排序
- Publish 与订阅集合的匹配规则

SDK 不包含 HTTP、WebSocket、WebRTC、bitcoin-libp2p 或其他传输代码，也不依赖这些
项目。SDK 只接收外部长期连接提供的已认证公钥和完整 Wire 字节，返回处理结果、需要
写回原连接的 Wire，或通过抽象 Dispatcher 产生广播事件。

身份认证由外层完成，规则见《身份与传输层设计》。支付和找回由 SPI 完成，不属于
SSP SDK。

## 2. Wire 消息

完整消息统一为：

```text
[wire_version, wire_kind, document_cbor]
```

`document_cbor` 是对应内层文档的完整 deterministic CBOR 字节，并以 CBOR byte string
放入外层 Wire。解析后必须保留收到的 exact bytes；不能先解码内层文档，再用另一份编码
替换它。

| Kind | 名称 | 中文含义 |
|---:|---|---|
| 1 | `Publish` | 发布或交付一条广播 |
| 2 | `Subscribe` | 订阅一个 channel 或所有 channel |
| 3 | `Unsubscribe` | 取消一个 channel 或全部订阅 |
| 4 | `SubscriptionsRequest` | 查询已订阅列表 |
| 5 | `SubscriptionsResponse` | 返回已订阅列表 |

`Publish` 同时用于客户端向服务程序发布，以及服务程序向匹配的在线订阅者交付。
方向由已认证连接和调用位置确定，不在 Wire 中增加方向字段。

## 3. 认证文档

### 3.1 Publish

```text
publish_cbor = [
  channel,       // 非空 UTF-8 文本，不能是 "*"
  content_json   // 包含一份完整 JSON 文本的 UTF-8 字节串
]
```

`content_json` 必须是 RFC 8259 JSON 文本。SDK 在编码和解码时都必须验证它是单个完整
JSON 值；尾随的非空白内容必须拒绝。SSP 中继和服务端必须保存并转发收到的 exact bytes，
不能先解析 JSON 再重新序列化。

SSP 不解释 content 的字段和业务含义。

### 3.2 Subscribe

```text
subscribe_cbor = [
  channel        // 非空 UTF-8 文本，允许 "*"
]
```

### 3.3 Unsubscribe

```text
unsubscribe_cbor = [
  channel        // 非空 UTF-8 文本，允许 "*"
]
```

### 3.4 SubscriptionsRequest

```text
subscriptions_request_cbor = []
```

### 3.5 SubscriptionsResponse

```text
subscriptions_response_cbor = [
  channels_cbor
]

channels_cbor = [
  channel,
  ...
]
```

`channels_cbor` 是集合的规范表示：

- 空集合编码为空数组。
- 普通 channel 不能重复，必须按 UTF-8 字节升序排列。
- 全频道订阅必须编码为仅包含 `*` 的数组。
- `*` 不能与普通 channel 同时出现。

## 4. Channel 规则

- Channel 是非空 UTF-8 文本。
- 名称按 UTF-8 字节精确比较，区分大小写。
- SDK 不裁剪空白，不执行 Unicode 归一化。
- `*` 是唯一特殊名称，只能用于 Subscribe 和 Unsubscribe。
- 除 `*` 外，不定义保留前缀、层级或其他通配符。
- Channel 不需要预先存在，也没有创建或删除报文。

## 5. 订阅状态

订阅状态属于外层认证得到的 `remote_public_key`，Wire 不能声明或覆盖该身份。

普通状态是一个 channel 集合。全频道状态使用规范集合 `{ "*" }` 表示：

| 当前状态 | 操作 | 新状态 |
|---|---|---|
| 任意状态 | `Subscribe("*")` | `{ "*" }` |
| `{ "*" }` | `Subscribe(channel)` | `{ "*" }` |
| 普通集合 | `Subscribe(channel)` | 加入 channel |
| `{ "*" }` | `Unsubscribe(channel)` | `{ "*" }` |
| 普通集合 | `Unsubscribe(channel)` | 移除 channel |
| 任意状态 | `Unsubscribe("*")` | 空集合 |

所有 Subscribe 和 Unsubscribe 操作都是幂等的。服务程序必须原子保存一次操作后的完整
订阅状态。

同一条有序流上的 Wire 必须依次处理。SubscriptionsResponse 必须反映它之前已经成功
处理的 Subscribe 和 Unsubscribe。

## 6. 广播交付

收到 Publish 后，服务程序将相同的 channel 和 `content_json` exact bytes 交付给：

- 普通订阅集合包含该 channel 的在线身份；或
- 订阅集合为 `{ "*" }` 的在线身份。

一条 Publish 对同一身份最多产生一条交付。SSP v1 没有消息 ID、确认、重试、历史或
离线队列，因此不提供跨断线的可靠交付承诺。单条有序传输流中的交付保持该流的发送
顺序；SSP 不定义跨连接或跨发布者的全局顺序。

## 7. 身份和 SPI 边界

- SSP Wire 不携带公钥、签名、余额或支付信息。
- SSP 服务只接受外层提供的已认证 `remote_public_key`。
- 订阅集合以 `remote_public_key` 标识。
- 发布权限、订阅权限和计费策略在进入 SSP 处理前或处理后由应用层决定。
- 应用可以使用同一身份调用 SPI，但 SSP 不解析 SPI Wire，也不引用 SPI 字段。
- SPI 不可用时如何拒绝操作属于应用策略，不改变 SSP Wire。

## 8. SDK 分层

```text
ssp/protocol      版本、Kind、字段类型、限制常量和 channel 规则
ssp/wire          CBOR 编解码、JSON 校验和严格解码
ssp/client        创建发布/订阅报文，解析列表和广播
ssp/service       按 Kind 处理消息、生成列表响应并调用抽象 Dispatcher
ssp/subscription  订阅集合状态转换、规范排序和 Backend
```

第三方服务实现订阅后端：

```go
type SubscriptionBackend interface {
    Subscribe(ctx, remotePublicKey, channel) error
    Unsubscribe(ctx, remotePublicKey, channel) error
    Subscriptions(ctx, remotePublicKey) ([]string, error)
}
```

后端方法必须实现第 5 节的原子、幂等状态转换。SDK 必须再次验证并规范化后端返回的列表，
不能直接信任其顺序。

服务程序把 Publish 交给抽象 dispatcher。SDK 提供规范的 `Matches` 纯函数；外部应用实现
dispatcher，并使用自己的 bitcoin-libp2p 在线连接完成匹配和实际发送。本项目不提供
bitcoin-libp2p、WebSocket 或其他网络适配器。

## 9. 对外接口

客户端接口：

```go
NewPublish(channel, contentJSON) (wireCBOR, error)
NewSubscribe(channel) (wireCBOR, error)
NewUnsubscribe(channel) (wireCBOR, error)
NewSubscriptionsRequest() (wireCBOR, error)
ParsePublish(wireCBOR) (protocol.Publish, error)
ParseSubscriptionsResponse(wireCBOR) (channels, error)
```

服务程序接口：

```go
HandleMessage(ctx, remotePublicKey, wireCBOR, backend, dispatcher) (
    responseWireCBOR, hasResponse, error,
)
```

返回规则：

- Subscribe、Unsubscribe 和 Publish 成功时没有响应 Wire。
- SubscriptionsRequest 成功时返回 SubscriptionsResponse Wire。
- `hasResponse` 明确区分“没有直接响应”和“存在响应”，调用者不能发送伪造的空 Wire。
- Publish 的订阅者交付由 dispatcher 完成，不作为发送者的响应。
- 格式错误、未知 Kind、后端或 Dispatcher 错误作为本地错误返回给外部应用，不定义
  ErrorResponse。

## 10. 严格编码

- 使用 RFC 8949 deterministic CBOR。
- 协议结构只使用定长数组，不使用 map。
- 禁止 tag、不定长数组和尾随字段。
- `document_cbor` 使用 CBOR byte string，内容是完整的内层 deterministic CBOR。
- Channel 使用 CBOR text string；`content_json` 使用 CBOR byte string。
- 解码后重新编码，结果必须与原始 Wire 字节完全相同。
- `content_json` 验证后仍保留收到的原始字节。
- 未知版本、未知 Kind、错误字段数量和非法 channel 直接拒绝。
- 解码前必须检查协议定义的消息最大长度；外部连接可以使用更严格的限制。

冻结限制（Go 与 TypeScript 必须保持一致）：

| Go 常量 | TypeScript 常量 | 数值 | 中文含义 |
|---|---|---:|---|
| `MaxWireBytes` | `MAX_WIRE_BYTES` | 1048576 | 完整 Wire 最大字节数 |
| `MaxDocumentBytes` | `MAX_DOCUMENT_BYTES` | 1048512 | 内层 document 最大字节数 |
| `MaxChannelUTF8Bytes` | `MAX_CHANNEL_UTF8_BYTES` | 256 | channel UTF-8 最大字节数 |
| `MaxContentJSONBytes` | `MAX_CONTENT_JSON_BYTES` | 1048000 | JSON 原始字节最大长度 |
| `MaxSubscriptions` | `MAX_SUBSCRIPTIONS` | 1024 | 单身份订阅集合最大元素数 |

除上述明确限制外，代码不添加协议文档之外的隐式上限。

## 11. 一致性测试

SDK 必须提供：

- 每种消息的 golden fixture
- 跨版本、跨 Kind 解码失败测试
- 空 channel 和 Publish `*` 拒绝测试
- JSON 非 UTF-8、无效 JSON 和尾随内容拒绝测试
- JSON exact bytes 往返测试
- Subscribe、Unsubscribe 幂等状态转换测试
- `*` 替换、清空和不支持排除项的测试
- SubscriptionsResponse 去重、排序和非法混合拒绝测试
- 同一 Publish 对同一订阅者只匹配一次的测试
- 不允许业务报文自报身份的测试
- 严格解码和 fuzz 测试

其他语言实现必须通过相同的 fixture 和测试规则。
