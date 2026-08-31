# SSP V1 Go / TypeScript 双语 SDK 施工单

> 状态：已完成，保留为初版实现记录。
>
> 后续改版：请求关联、成功响应和实际扣费金额由
> [002 施工单](./002-SSP-V1请求响应与精确计费改造施工单.md) 统一实施；如两张施工单冲突，
> 以 002 为准。
>
> 需求真值：
>
> - [需求](../../docs/需求.md)
> - [SDK 设计](../../docs/SDK设计.md)
> - [身份与长连接边界设计](../../docs/身份与传输层设计.md)
> - [SDK 使用](../../docs/SDK使用.md)

## 0. 施工目标

一次性完成 Go 与 TypeScript 两套可互操作的 SSP 核心 SDK，使第三方程序只实现订阅
持久化、在线连接管理和实际广播发送，不自行实现 Wire、订阅状态机、规范排序和 JSON
exact bytes 规则。

SSP 用于长期连接上的订阅广播。实际应用使用
`/home/david/Workspaces/bitcoin-libp2p` 通讯，但该通讯项目位于 SSP 之外。

最终必须证明：

1. Go 与 TypeScript 对相同对象产生完全相同的 CBOR；
2. 两种语言可以互相解析全部五种 SSP Wire；
3. 两端执行相同 Subscribe / Unsubscribe 序列后得到完全相同的订阅集合；
4. Publish 的 channel 和 JSON exact bytes 跨语言往返不变；
5. Service 只依赖已认证公钥、Backend 和抽象 Dispatcher；
6. SDK 可以被长期连接应用调用，但自身没有任何网络通讯依赖；
7. SPI、计费和连接实现不改变任何 SSP Wire 字节。

## 1. 硬性边界

### 必须做

- SSP Wire V1 只包含 `[wire_version, wire_kind, document_cbor]`；
- V1 只定义 Publish、Subscribe、Unsubscribe、SubscriptionsRequest、
  SubscriptionsResponse 五种 Kind；
- `document_cbor` 作为 exact bstr 放入 Wire；
- `content_json` 是合法 RFC 8259 JSON 的原始 UTF-8 字节；
- 使用 RFC 8949 deterministic CBOR 和严格 decoder；
- 普通 channel 精确匹配，区分大小写，不裁剪、不做 Unicode 归一化；
- `*` 只用于 Subscribe 和 Unsubscribe，且严格执行规范状态机；
- 订阅状态只属于外层认证得到的 `remote_public_key`；
- Go / TypeScript 共用 CDDL、fixture、错误分类和状态转换向量；
- 所有输入、输出字节切片都执行防御性复制；
- 所有公开字段、错误码和关键 API 都有中文说明。

### 不能做

- 不得新增 Ack、ErrorResponse、ChannelList 或第六种业务 Kind；
- 不得在 SSP payload 中加入公钥、签名、价格、余额或支付凭证；
- 不得把 SPI Wire 包入 SSP Wire；
- 不得加入 channel 创建、删除、所有者、元数据或配置；
- 不得加入消息 ID、时间戳、历史、离线队列、确认、重试或去重；
- 不得加入正则、前缀、层级通配或 `*` 排除列表；
- 不得解析 JSON 后重新序列化再转发；
- 不得相信 SSP payload 自报的身份；
- 不得实现 HTTP 身份外壳或 HTTP 请求处理；
- 不得实现 WebSocket、WebRTC、bitcoin-libp2p 或其他网络适配器；
- 不得在 Go module 或 npm package 中引入 bitcoin-libp2p；
- 不得定义 Stream Protocol ID、网络 frame、握手、心跳或重连；
- 不得为 Go 和 TypeScript 维护两套不同的协议常量。

## 2. 目录定稿

```text
SatSubscriptionProtocol/
  docs/
  spec/v1/
    ssp.cddl

  testdata/v1/
    wire-v1.json
    subscriptions-v1.json
    dispatch-v1.json
    invalid-v1.json

  go.mod
  protocol/                 # Go 类型、版本、Kind、错误分类
  wire/                     # Go SSP Wire 编解码
  client/                   # Go 客户端报文构造与解析
  subscription/             # Go 状态转换、排序和匹配
  service/                  # Go 服务入口、Backend、Dispatcher

  typescript/
    package.json
    package-lock.json
    tsconfig.json
    src/protocol/
    src/wire/
    src/client/
    src/subscription/
    src/service/
    test/

  integration/
    fixture/                # Go ↔ TypeScript 编解码和状态互操作

  examples/
    go/                     # 不启动网络的核心 SDK 示例
    typescript/             # 不启动网络的核心 SDK 示例

  scripts/
    test-integration.sh
```

目标包名：

```text
Go module: github.com/bsv8/SatSubscriptionProtocol
npm package: sat-subscription-protocol
```

如果 npm scope 或包名无法发布，停止并确认新名称，不得静默改名。

## 3. A01：冻结 V1 Wire 与限制常量

### 施工

1. 新增 `spec/v1/ssp.cddl`，固定五种 Kind、外层和所有文档数组顺序；
2. 固定 Wire：

```text
Kind 1 = [1, 1, publish_cbor]
Kind 2 = [1, 2, subscribe_cbor]
Kind 3 = [1, 3, unsubscribe_cbor]
Kind 4 = [1, 4, subscriptions_request_cbor]
Kind 5 = [1, 5, subscriptions_response_cbor]
```

3. `publish_cbor` 等内层文档先独立编码为 deterministic CBOR，再作为 exact bstr 放入
   外层 Wire；
4. 固定并命名以下跨语言一致的限制常量：

```text
MaxWireBytes
MaxDocumentBytes
MaxChannelUTF8Bytes
MaxContentJSONBytes
MaxSubscriptions
```

5. 固定以下字段类型：

```text
wire_version       CBOR uint，V1 固定为 1
wire_kind          CBOR uint，V1 固定为 1..5
channel            非空 CBOR tstr
content_json       包含一个完整 JSON 文本的 CBOR bstr
channels           无重复、规范排序的 channel 数组
remote_public_key  外部传入的 33 字节压缩公钥，不进入 Wire
```

6. 普通 channel 允许除精确字符串 `*` 外的任何非空 UTF-8 文本；
7. Go 与 TypeScript 对 JSON 都先验证 UTF-8，再验证单个完整 RFC 8259 JSON 值；允许前后
   JSON 空白并保留 exact bytes，拒绝尾随非空白内容；
8. 将冻结的数值常量同步写回正式文档，不允许代码出现文档外的隐式上限。

### 验收

- CDDL、文档和代码常量一一对应；
- 未知版本、未知 Kind、错误数组长度、尾随字段全部拒绝；
- Publish `*`、空 channel、超限 channel 和超限 JSON 全部拒绝；
- V1 decoder 不包含兼容猜测、map 结构或可选字段；
- spec 中不存在身份外壳、网络 frame 或传输字段。

## 4. A02：共享 fixture 与错误分类

### 施工

提交人工审阅后的固定向量：

- 五种 SSP Wire；
- 空对象、嵌套对象、数组、字符串、数字、布尔值和 null JSON；
- 包含中文、emoji、转义字符、前后空白的 JSON exact bytes；
- 普通多 channel 列表、空列表和 `["*"]`；
- Subscribe / Unsubscribe 完整状态转换表；
- 普通订阅、`*` 订阅、未匹配和重复身份的 dispatch 向量；
- 非规范 CBOR、错误 bstr、坏 UTF-8、无效 JSON、Publish `*`、重复或乱序列表、
  `*` 与普通 channel 混合、超长消息。

固定本地 SDK 错误分类，至少包括：

```text
INVALID_WIRE
UNSUPPORTED_VERSION
UNSUPPORTED_KIND
NON_CANONICAL_CBOR
MESSAGE_TOO_LARGE
INVALID_CHANNEL
INVALID_JSON
INVALID_SUBSCRIPTIONS
INVALID_PUBLIC_KEY
BACKEND_FAILURE
DISPATCH_FAILURE
```

这些错误码只用于 SDK、本地日志和外部调用者，不得编码成 SSP ErrorResponse。

### 验收

- Go 和 TypeScript 独立读取相同 fixture；
- 两端编码结果逐字节等于 fixture；
- 两端状态转换和 dispatch 结果逐项等于 fixture；
- 两端对每个 invalid case 返回相同稳定错误分类；
- fixture 不包含私钥、签名、网络地址或连接数据。

## 5. A03：Go 核心 SDK

### `protocol`

- 定义唯一 `WireVersion = 1` 和五种 Kind；
- `PublicKey` 使用 33 字节 named type，只作为外部已认证身份；
- Publish 类型只保存 `Channel` 和 `ContentJSON`；
- 所有构造函数复制输入 slice；
- 定义稳定 sentinel error，调用方可以使用 `errors.Is`；
- 错误包装保留稳定分类，不把原始 JSON 内容写入错误字符串。

### `wire`

- 使用严格 deterministic CBOR mode；
- 实现五种显式 Encoder / Decoder 和一个只负责路由的 `Parse`；
- 先编码内层 document，再作为 bstr 编码外层 Wire；
- 解码后按唯一 encoder 重编码并逐字节比较；
- 分配和 JSON 校验前检查长度上限；
- 固定结构禁止 map、float、tag、不定长项和尾随数据；
- `content_json` 使用 `utf8.Valid` 和 `json.Valid` 验证，但始终保留原始字节。

### `client`

```go
NewPublish(channel string, contentJSON []byte) ([]byte, error)
NewSubscribe(channel string) ([]byte, error)
NewUnsubscribe(channel string) ([]byte, error)
NewSubscriptionsRequest() ([]byte, error)
ParsePublish(wireCBOR []byte) (Publish, error)
ParseSubscriptionsResponse(wireCBOR []byte) ([]string, error)
```

- 返回值不与输入共享可变内存；
- ParsePublish 不把 JSON 自动解码成 `map[string]any`；
- ParseSubscriptionsResponse 返回新 slice，并保持线上规范顺序。

### 依赖检查

- `go.mod` 不包含 bitcoin-libp2p、WebSocket、HTTP router 或 SPI SDK；
- 核心包不 import `net/http`；
- 普通测试不打开监听端口、不建立网络连接。

### 验收

```text
go test ./...
go test -race ./...
go vet ./...
```

## 6. A04：TypeScript 核心 SDK

实现与 Go 同义的 `protocol`、`wire` 和 `client`：

```ts
newPublish(channel: string, contentJson: Uint8Array): Uint8Array
newSubscribe(channel: string): Uint8Array
newUnsubscribe(channel: string): Uint8Array
newSubscriptionsRequest(): Uint8Array
parsePublish(wire: Uint8Array): Publish
parseSubscriptionsResponse(wire: Uint8Array): readonly string[]
```

规则：

- 输入和返回的 `Uint8Array` 必须复制；
- 使用 fatal UTF-8 decoder 验证字节，再以 `JSON.parse` 验证单个 JSON 值；
- JSON 只用于校验，ParsePublish 返回原始 `Uint8Array`，不返回解析后的可变对象；
- 源码不依赖 Node-only `Buffer` 语义；
- 公开 API 导出明确 readonly 类型，不暴露 CBOR 库内部类型；
- 使用稳定 `SSPError.code` 与 Go 错误分类对齐；
- 构建产物包含 ESM、`.d.ts` 和 source map；
- npm exports 至少提供根入口以及 `protocol`、`wire`、`client`、`subscription`、
  `service` 子入口。

### 依赖检查

- `package.json` 不包含 bitcoin-libp2p、WebSocket、HTTP server 或 SPI SDK；
- 源码不打开 socket、不发起 fetch、不创建网络节点；
- 普通测试不启动 Go、浏览器或监听服务。

### 验收

```text
cd typescript
npm ci
npm test
npm run build
npm pack --dry-run
```

## 7. A05：订阅状态与持久化 Backend

Go 与 TypeScript 实现同义的纯状态转换函数：

```text
Subscribe(current, "*")       -> ["*"]
Subscribe(["*"], channel)     -> ["*"]
Subscribe(current, channel)    -> 加入并规范排序
Unsubscribe(["*"], channel)   -> ["*"]
Unsubscribe(current, channel)  -> 移除
Unsubscribe(current, "*")     -> []
```

并实现：

```text
NormalizeSubscriptions(channels)
Matches(channels, publishChannel)
```

`Matches` 对 `["*"]` 返回 true；对普通列表只做精确匹配；非法集合必须先拒绝，不能边修复
边继续。

两种语言定义同义 Backend：

```text
Subscribe(remote_public_key, channel)
Unsubscribe(remote_public_key, channel)
Subscriptions(remote_public_key) -> channels
```

Backend 契约：

- 每次 Subscribe / Unsubscribe 是一次原子、幂等状态转换；
- 并发添加两个不同普通 channel 不能丢失更新；
- 写入前和读出后都验证规范状态；
- 持久化 key 是 33 字节 `remote_public_key` exact bytes；
- 不按连接 ID、IP、cookie 或 payload 字段保存状态；
- SDK 提供线程安全内存 Backend，用于测试和示例，不宣称适合生产；
- 生产数据库实现必须自行保证事务和持久化。

### 验收

- 对每个状态转换向量，两种语言结果完全一致；
- 相同操作顺序和并发执行 100 次都保持幂等；
- 并发订阅不同 channel 不丢失任一 channel；
- 返回列表始终按 UTF-8 字节升序且无重复；
- `["*"]` 永远不与普通 channel 同时出现；
- 重启内存 Backend 后丢失数据的限制在文档中明确说明。

## 8. A06：Service 与 Dispatcher 抽象

服务入口固定为：

```go
HandleMessage(
    ctx,
    remotePublicKey,
    wireCBOR,
    backend,
    dispatcher,
) (responseWireCBOR []byte, hasResponse bool, err error)
```

TypeScript 使用同义返回结构：

```ts
{ response?: Uint8Array }
```

处理规则：

- Subscribe：原子更新 Backend，成功时无 SSP 响应；
- Unsubscribe：原子更新 Backend，成功时无 SSP 响应；
- SubscriptionsRequest：读取 Backend 并返回 SubscriptionsResponse；
- Publish：把原始 Publish Wire 和已认证发布者公钥交给 Dispatcher，成功时无 SSP 响应；
- 服务入口不接受 SubscriptionsResponse 作为客户端命令；
- 外部长期连接应用负责按接收顺序调用同一连接的 HandleMessage。

Dispatcher 的窄接口只接收：

```text
authenticated publisher public key
channel
content_json exact bytes
完整 Publish Wire exact bytes
```

规则：

- Dispatcher 是调用者实现的普通接口，不包含任何网络类型；
- SSP SDK 提供 `Matches`，供调用者按订阅状态选择接收身份；
- SDK 提供 RecordingDispatcher，仅用于单元测试和示例；
- 本项目不实现在线连接注册、实际发送、慢消费者处理或断线清理；
- Dispatcher 失败返回稳定本地错误，不生成 SSP ErrorResponse。

### 验收

- Publish 传给 Dispatcher 的 Wire 与输入逐字节相同；
- Subscribe → SubscriptionsRequest 严格观察到新状态；
- Unsubscribe → SubscriptionsRequest 严格观察到移除后的状态；
- 非法客户端 SubscriptionsResponse 被拒绝；
- Backend 和 Dispatcher 错误不会被编码成协议报文；
- Service 测试全部使用进程内假实现，不打开网络连接。

## 9. A07：外部长期连接调用契约

本阶段只固定调用边界和文档，不实施通讯代码。

外部 bitcoin-libp2p 应用的调用流程：

```text
从已认证长期连接收到一个完整 SSP Wire
  → 从连接取得 remote_public_key
  → 可选 SPI / 计费策略
  → HandleMessage(remote_public_key, wire)
      ├── response 存在：外部应用写回原连接
      └── response 不存在：不写伪造的空报文

Dispatcher 收到 Publish 事件
  → 外部应用读取订阅状态并使用 Matches
  → 在自己的在线连接注册表中选择接收者
  → 外部应用发送原始 Publish Wire
```

施工内容：

- 在 README 和 SDK 使用文档中提供上述伪代码；
- 明确 `remote_public_key` 只能来自已认证长期连接；
- 明确 SubscriptionsResponse 写回发起查询的同一连接；
- 明确 Publish、Subscribe 和 Unsubscribe 没有直接 SSP 响应；
- 明确 SDK 不规定 bitcoin-libp2p Stream ID、分帧、握手或发送 API；
- 明确 SSP 仓库不引用 `/home/david/Workspaces/bitcoin-libp2p` 的 module 或 npm package；
- 明确连接与计费拒绝只阻止调用 SSP，不改变 SSP Wire。

### 验收

- Go 和 TypeScript 公开 API 中没有网络连接类型；
- `go list -deps ./...` 不包含 bitcoin-libp2p 或 HTTP server 包；
- npm dependency tree 不包含 bitcoin-libp2p、WebSocket 或 HTTP server；
- 仓库中不存在网络适配器目录、Stream handler 和连接测试；
- 外部调用者只凭文档即可正确映射直接响应与广播事件。

## 10. A08：纯核心跨语言集成

`scripts/test-integration.sh` 不启动网络，只通过 fixture、临时文件或标准输入输出执行：

1. Go 五种 Wire 编码 → TypeScript 解码；
2. TypeScript 五种 Wire 编码 → Go 解码；
3. Go 解码后重编码 → 与 TypeScript 原始 Wire 逐字节比较；
4. TypeScript 解码后重编码 → 与 Go 原始 Wire 逐字节比较；
5. 两种语言分别执行相同订阅状态向量并比较结果；
6. JSON exact bytes 双向往返；
7. 普通 channel、`*`、取消和列表规范排序；
8. Service 使用固定公钥、MemoryBackend 和 RecordingDispatcher 的结果对比；
9. 所有 invalid fixture 的稳定错误分类对比；
10. 启用或禁用外部计费布尔门禁后，允许路径中的 SSP Wire 保持不变。

测试约束：

- 使用临时目录保存跨进程结果，退出后删除；
- 不访问互联网和生产服务；
- 不打开端口、不启动浏览器、不创建长期连接；
- 不输出完整用户 JSON 内容；
- Go 与 TypeScript 不能在测试中调用对方源码，只能交换 fixture 或完整 Wire 字节。

### 验收

- 脚本在干净环境一次执行成功；
- 任一字节差异、错误分类差异或状态差异都使脚本失败；
- 测试结束后无临时文件、后台进程和生成物残留。

## 11. A09：文档、示例和发布门禁

### 文档与示例

- README 第一屏说明 SSP 是长期连接上的最小订阅广播协议；
- README 明确实际通讯由外部 bitcoin-libp2p 应用负责；
- Go 与 TypeScript 各提供报文构造、状态 Backend、Service 和 RecordingDispatcher 示例；
- 示例只在进程内调用 SDK，不建立真实网络连接；
- 所有英文 API、字段和错误分类旁边提供中文解释；
- 明确 `*` 规范状态、无排除项、无历史、Publish 非幂等和 JSON exact bytes；
- 正式文档中的 API 名称必须与实际导出完全一致。

### 发布检查

- Go / TypeScript 版本保持一致，从 `0.1.0` 开始；
- npm 正式包不包含 integration、临时产物和测试日志；
- Go consumer 只 import 正式模块路径；
- npm consumer 只 import package exports，不 import `src/`；
- `go mod tidy` 和 `npm ci` 后锁定依赖；
- dependency audit 证明没有 bitcoin-libp2p、HTTP 或 WebSocket 实现依赖；
- 未完成全部验收前不打 tag、不 publish。

## 12. 实施顺序与提交边界

严格按以下依赖顺序施工：

```text
A01 Wire 冻结
  → A02 fixture
      → A03 Go 核心 ─────────┐
      → A04 TypeScript 核心 ─┤
                              ├→ A05 状态 Backend
                              └→ A06 Service / Dispatcher
                                      → A07 外部长连接契约
                                      → A08 纯核心跨语言集成
                                      → A09 文档与发布门禁
```

建议提交边界与 A01..A09 一一对应。每个提交必须：

- 只包含当前阶段及必要的前置修正；
- 同时更新 Go、TypeScript、fixture 和文档中受影响的部分；
- 保持已有测试通过；
- 不提交生成目录、临时日志或后台服务状态。

如果实现发现必须增加第六种 Kind、请求 ID、消息 ID、排除列表或可靠队列，立即停止，
先修改需求和正式设计并取得确认，不能在代码中先行扩展协议。

如果实现需要 bitcoin-libp2p、HTTP、WebSocket、Stream frame 或连接注册表类型，也立即停止；
这些属于外部应用，不得为了示例方便引入 SSP 仓库。

## 13. 最终验收

必须全部通过：

```text
go test ./...
go test -race ./...
go vet ./...

cd typescript
npm ci
npm test
npm run build
npm pack --dry-run

./scripts/test-integration.sh
```

完成标准：

- CDDL、fixture、Go、TypeScript 四者一致；
- 两种语言公开 API、错误分类和状态转换语义一致；
- 五种 Wire 之外没有隐藏 Ack、ErrorResponse 或 ChannelList；
- Publish 的 channel 和 JSON exact bytes 跨语言往返不变；
- `*`、幂等、列表排序和无历史行为全部有共享测试证明；
- SSP payload 中不存在身份公钥、签名、SPI 或计费字段；
- Go 和 TypeScript 依赖树中不存在 bitcoin-libp2p、HTTP 或 WebSocket 实现；
- 仓库中不存在网络适配器、连接管理、Stream handler 或网络 frame；
- 所有测试只运行核心 SDK 和进程内假实现，不打开网络连接；
- 工作区无生成物、临时文件和后台进程残留；
- README、正式文档、CDDL、fixture 和实际导出 API 完全一致。
