# SSP SDK 使用说明

## 发布端

调用 `NewPublish(channel, contentJSON)` 创建完整 Wire。`channel` 必须是非空普通名称，
不能是 `*`；`contentJSON` 必须是一份完整、合法的 UTF-8 JSON 文本。

SDK 验证 JSON，但不会改写它。服务程序和订阅者收到的是发布端提供的 JSON exact bytes。
如果应用传入语言对象，应先使用该语言稳定的 JSON 序列化器生成字节。

Publish 成功没有 SSP 响应报文。传输成功只表示服务程序收到消息，不表示任意订阅者
已经接收。

## 订阅端

使用：

```text
NewSubscribe(channel)          订阅普通 channel
NewSubscribe("*")             订阅所有 channel
NewUnsubscribe(channel)        取消普通 channel
NewUnsubscribe("*")           清空全部订阅
NewSubscriptionsRequest()      查询已订阅列表
```

Subscribe 和 Unsubscribe 没有 SSP 响应报文。需要确认时，在同一条有序流中随后发送
SubscriptionsRequest，并使用 `ParseSubscriptionsResponse` 解析返回。

全频道订阅的列表固定为 `["*"]`。SSP 不支持从 `*` 中排除单个 channel；因此已订阅
`*` 时，`Unsubscribe(channel)` 不改变状态。

收到 Publish 后，使用 `ParsePublish` 取得 `channel` 和原始 `contentJSON`。SDK 已完成
Wire 和 JSON 格式验证，应用再按自己的 JSON 数据结构解析内容。

## 服务端

外部 bitcoin-libp2p 应用先完成长期连接和身份认证，再调用：

```text
HandleMessage(ctx, remote_public_key, payload, backend, dispatcher)
```

处理顺序：

1. 严格解析 SSP Wire；
2. 使用验签成功或安全连接提供的 `remote_public_key`；
3. Subscribe/Unsubscribe 原子更新该公钥的订阅集合；
4. SubscriptionsRequest 返回规范排序的 SubscriptionsResponse；
5. Publish 交给 dispatcher，向匹配的当前在线订阅者发送相同 Publish Wire。

不能相信业务 payload 自报身份。Publish 的 `content_json` 必须使用收到的完整字节，不能
解析后重新编码再广播。

## 与 SPI 配合

SSP Wire 中没有支付字段。服务程序可以在调用 `HandleMessage` 前后按自己的策略使用 SPI，
并以同一个已认证公钥关联账户，但不要把 SPI Wire 包进 SSP Wire。

计费失败由外部应用处理。SSP v1 没有计费错误报文。

SSP SDK 不依赖 bitcoin-libp2p，也不实现连接、Stream、分帧或实际发送。外部应用只需把
已认证公钥和完整 SSP Wire 交给 SDK，并把 SDK 返回的响应或广播事件写入自己的长期连接。

Go 对应正式 API：

```go
client.NewPublish(channel, contentJSON) ([]byte, error)          // 创建发布 Wire
client.NewSubscribe(channel) ([]byte, error)                     // 创建订阅 Wire
client.NewUnsubscribe(channel) ([]byte, error)                   // 创建取消订阅 Wire
client.NewSubscriptionsRequest() ([]byte, error)                 // 创建查询 Wire
client.ParsePublish(wireCBOR) (protocol.Publish, error)           // 解析原始 JSON 字节
client.ParseSubscriptionsResponse(wireCBOR) ([]string, error)    // 解析规范订阅列表
service.HandleMessage(ctx, remotePublicKey, wireCBOR, backend, dispatcher)
                                                               // 处理已认证连接消息
```

TypeScript 对应正式 API（npm package `@bsv8/ssp`）：

```ts
newPublish(channel, contentJson): Uint8Array                 // 创建发布 Wire
newSubscribe(channel): Uint8Array                            // 创建订阅 Wire
newUnsubscribe(channel): Uint8Array                          // 创建取消订阅 Wire
newSubscriptionsRequest(): Uint8Array                        // 创建查询 Wire
parsePublish(wireCbor): Publish                              // 解析原始 JSON 字节
parseSubscriptionsResponse(wireCbor): readonly string[]      // 解析规范订阅列表
handleMessage(remotePublicKey, wireCbor, backend, dispatcher) // 返回 { response?: Uint8Array }
```

Go `protocol` 包导出 `MaxWireBytes = 1048576`、`MaxDocumentBytes = 1048512`、
`MaxChannelUTF8Bytes = 256`、`MaxContentJSONBytes = 1048000` 和 `MaxSubscriptions = 1024`；
TypeScript `protocol` 子入口导出对应的 `MAX_WIRE_BYTES`、`MAX_DOCUMENT_BYTES`、
`MAX_CHANNEL_UTF8_BYTES`、`MAX_CONTENT_JSON_BYTES` 和 `MAX_SUBSCRIPTIONS`，以及保持跨语言命名的
`MaxWireBytes`、`MaxDocumentBytes`、`MaxChannelUTF8Bytes`、`MaxContentJSONBytes`、
`MaxSubscriptions` 别名。两端值固定一致。

Go 使用 `errors.Is` 检查 `protocol.ErrInvalidWire` 等 sentinel error；TypeScript 使用
`SSPError.code`。错误只返回本地调用者，不编码为 SSP ErrorResponse。

## 字段对照

| 线上字段 | SDK 含义 | 中文含义 |
|---|---|---|
| `wire_version` | 协议版本 | SSP Wire 版本 |
| `wire_kind` | 消息类型 | Publish、订阅操作或列表查询/返回 |
| `channel` | UTF-8 string | 精确匹配的 channel name，`*` 为全频道 |
| `content_json` | raw bytes | 一份合法 JSON 文本的原始 UTF-8 字节 |
| `channels` | string list | 规范排序的已订阅集合 |
| `remote_public_key` | 外层认证结果 | 订阅状态所属的长期身份，不进入 SSP Wire |
