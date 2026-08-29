# SSP V1

SSP（Sat Subscription Protocol）是运行在已认证长期连接上的最小订阅广播协议：客户端按
channel 订阅，发布端发送一条合法 JSON 广播，服务程序把原始 Publish Wire 交给外部
Dispatcher。Go 与 TypeScript SDK 只负责协议、订阅状态和调用边界，不建立任何网络连接。

实际通讯由外部 `/home/david/Workspaces/bitcoin-libp2p` 应用负责，包括长期连接、认证、分帧、
写回响应、在线连接注册和实际广播发送。本仓库不引用该项目，也不提供 HTTP、WebSocket、
WebRTC、SPI 或计费实现。

## 固定 Wire

完整 Wire 只有一个 deterministic CBOR 定长数组：`[wire_version, wire_kind, document_cbor]`。
`document_cbor` 是对应内层文档的完整 CBOR 字节，并作为 exact bstr 放入外层。

| `wire_kind` | English API | 中文含义 |
|---:|---|---|
| 1 | `Publish` | 发布/广播 channel 与 JSON 原始字节 |
| 2 | `Subscribe` | 订阅普通 channel 或 `*` 全频道 |
| 3 | `Unsubscribe` | 取消普通 channel 或 `*` 清空全部 |
| 4 | `SubscriptionsRequest` | 查询当前已订阅集合 |
| 5 | `SubscriptionsResponse` | 返回规范排序的订阅集合 |

V1 没有 Ack、ErrorResponse、ChannelList 或其他第六种 Kind。Wire 不包含公钥、签名、SPI、
价格、余额、支付凭证、消息 ID、时间戳、历史、重试或连接字段。

冻结限制（Go `protocol` 与 TypeScript `protocol` 同值）：

- `MaxWireBytes` / `MAX_WIRE_BYTES`：`1048576`，完整 Wire 最大 1 MiB；
- `MaxDocumentBytes` / `MAX_DOCUMENT_BYTES`：`1048512`，内层 document 最大字节数；
- `MaxChannelUTF8Bytes` / `MAX_CHANNEL_UTF8_BYTES`：`256`，channel UTF-8 最大字节数；
- `MaxContentJSONBytes` / `MAX_CONTENT_JSON_BYTES`：`1048000`，JSON 原始字节最大长度；
- `MaxSubscriptions` / `MAX_SUBSCRIPTIONS`：`1024`，单身份订阅集合最大元素数。

普通 channel 精确匹配、区分大小写，不裁剪、不做 Unicode 归一化。精确字符串 `*` 只允许
出现在 Subscribe、Unsubscribe，或作为唯一的 `SubscriptionsResponse` 列表元素。Publish
永远不能使用 `*`。

`content_json` 会先验证合法 UTF-8，再验证一份完整 RFC 8259 JSON 值；前后 JSON 空白保留，
服务程序不得解析后重新序列化。Publish 不是幂等操作；Subscribe 和 Unsubscribe 是幂等操作。

## SDK 入口

Go module：`github.com/bsv8/SatSubscriptionProtocol`
SDK 版本：`0.1.0`（Go `protocol.SDKVersion` 与 npm `SDK_VERSION` / `SDKVersion` 一致）

```go
import (
    "context"

    "github.com/bsv8/SatSubscriptionProtocol/client"
    "github.com/bsv8/SatSubscriptionProtocol/service"
    "github.com/bsv8/SatSubscriptionProtocol/subscription"
)

wireCBOR, err := client.NewPublish("news", []byte(` {"title":"hello"} `)) // 创建发布 Wire
backend := subscription.NewMemoryBackend()                                 // 内存 Backend，仅测试/示例
dispatcher := service.NewRecordingDispatcher()                             // 记录 Dispatcher，仅测试/示例
response, hasResponse, err := service.HandleMessage(
    context.Background(), remotePublicKey, wireCBOR, backend, dispatcher,
) // 使用外层已认证公钥处理
_ = response; _ = hasResponse; _ = err
```

TypeScript package：`@bsv8/ssp`。根入口以及 `protocol`、`wire`、`client`、`subscription`、
`service` 子入口均可用；消费方只应使用 package exports，不应导入 `src/`。

```ts
import {
  handleMessage, newPublish, MemoryBackend, RecordingDispatcher,
} from "@bsv8/ssp";

const wire = newPublish("news", new TextEncoder().encode(` {"title":"hello"} `)); // 创建发布 Wire
const result = await handleMessage(remotePublicKey, wire, new MemoryBackend(), new RecordingDispatcher());
// result.response 只在 SubscriptionsRequest 成功时存在
```

Go 错误使用 `errors.Is(err, protocol.ErrInvalidWire)` 等 sentinel error；TypeScript 错误使用
`SSPError.code`。稳定分类包括 `INVALID_WIRE`（Wire 无效）、`UNSUPPORTED_VERSION`（版本不支持）、
`UNSUPPORTED_KIND`（Kind 不支持）、`NON_CANONICAL_CBOR`（CBOR 非规范）、`MESSAGE_TOO_LARGE`（超限）、
`INVALID_CHANNEL`（channel 无效）、`INVALID_JSON`（JSON 无效）、`INVALID_SUBSCRIPTIONS`（订阅集合无效）、
`INVALID_PUBLIC_KEY`（公钥无效）、`BACKEND_FAILURE`（Backend 失败）和 `DISPATCH_FAILURE`（Dispatcher 失败）。
这些错误不会编码成 SSP Wire。

## 外部长期连接调用契约

```text
已认证长期连接收到一个完整 SSP Wire
  → 从该连接取得 remote_public_key（外层已认证的 33 字节压缩公钥）
  → 可选执行外部 SPI / 计费策略
  → HandleMessage(remote_public_key, wire)
      ├── response 存在：写回发起查询的同一连接
      └── response 不存在：不发送伪造空报文

Dispatcher 收到 Publish 事件
  → 外部应用读取各在线身份的订阅集合
  → 使用 SDK 的 Matches / matches 做精确匹配
  → 以身份去重并选择在线连接
  → 外部应用发送 Dispatcher 提供的完整 Publish Wire
```

Publish、Subscribe、Unsubscribe 成功时没有直接 SSP 响应；只有 SubscriptionsRequest 产生
SubscriptionsResponse。连接认证失败、SPI/计费拒绝、分帧、发送失败和断线都由外部应用处理，
不会修改 SSP Wire。

SDK 对 `remote_public_key` 只检查 33 字节长度和 `02/03` 压缩前缀，不验证 secp256k1 曲线点；
它必须来自外层已认证长期连接，不能由 SSP payload 自报。

## 目录与验收

- `spec/v1/ssp.cddl`：V1 CDDL 和冻结限制；
- `testdata/v1/`：Go 与 TypeScript 共用的 Wire、状态、dispatch、invalid fixture；
- `protocol/`、`wire/`、`client/`、`subscription/`、`service/`：Go SDK；
- `typescript/`：`@bsv8/ssp` TypeScript SDK；
- `integration/fixture/`、`scripts/test-integration.sh`：无网络跨语言集成检查；
- `scripts/validate-cddl.sh`：RFC 8610 CDDL 语法校验门禁；
- `examples/go/`、`examples/typescript/`：不启动网络的调用示例，TypeScript 示例作为独立 consumer 验证 npm exports。

```text
go test ./...
go test -race ./...
go vet ./...
cd typescript && npm ci && npm test && npm run build && npm pack --dry-run
./scripts/test-integration.sh
# test-integration.sh 会继续执行 TypeScript 独立 consumer 示例
```
