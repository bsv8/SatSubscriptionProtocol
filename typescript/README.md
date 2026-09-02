# sat-subscription-protocol

SSP V1 的无网络核心 SDK。长期连接、身份认证、分帧和实际发送由外部应用负责；本包
提供六种 deterministic CBOR Wire、订阅状态、精确金额和每 Stream 的 Session。

## 最小并行示例

~~~ts
import {
  MemoryBackend,
  RecordingDispatcher,
  Session,
  newPublish,
  newRequestId,
  newSubscribe,
  parseActionResult,
} from "sat-subscription-protocol";

// 必须来自外层已认证连接；这里仅使用形状正确的示例公钥。
const remotePublicKey = new Uint8Array(33);
remotePublicKey[0] = 0x02;
const session = new Session(
  remotePublicKey,
  new MemoryBackend(),
  new RecordingDispatcher(),
);

const subscribeId = newRequestId();
const publishId = newRequestId();
const [subscribeResult, publishResult] = await Promise.all([
  session.handleMessage(newSubscribe(subscribeId, "news")),
  session.handleMessage(newPublish(
    publishId,
    "news",
    new TextEncoder().encode(' {"title":"hello"} '),
  )),
]);

// 响应可以乱序，按 requestId 关联；chargedAmount 是精确字符串。
const subscribeAction = parseActionResult(subscribeResult.response);
const publishAction = parseActionResult(publishResult.response);
console.log(subscribeAction.requestId, subscribeAction.chargedAmount);
console.log(publishAction.requestId, publishAction.chargedAmount);
session.close();
~~~

Publish、Subscribe、Unsubscribe 成功都返回 ActionResult；查询成功返回
SubscriptionsResponse。同一 Session 重复 requestId 会返回 { suppressed: true }，
不执行、不扣费、不响应、不返回历史结果。Session 最多登记 65536 个不同编号。

## API 与错误

构造函数需要调用方提供 requestId：

~~~ts
newPublish(requestId, channel, contentJson): Uint8Array
newSubscribe(requestId, channel): Uint8Array
newUnsubscribe(requestId, channel): Uint8Array
newSubscriptionsRequest(requestId): Uint8Array
parsePublish(wireCbor): Publish
parseActionResult(wireCbor): ActionResult
parseSubscriptionsResponse(wireCbor): SubscriptionsResponse
~~~

newRequestId() 使用 Web Crypto/Node 安全随机源。Amount 是规范十进制字符串，最多
64 字节、18 位小数，不转换成 JavaScript number。服务业务失败仍返回失败 ActionResult；
TypeScript SessionResult.cause 是本地 SSPError，原始异常位于标准 Error.cause，不会进入 Wire。

Go 的对应 Result 在业务失败时仍保留 Response 和 Cause，但返回 error 为 nil；连接层应
先写响应，再记录 Cause。SPI 整数余额与 SSP 小数金额的应用层换算见仓库
[SPI 集成与金额换算](../docs/SPI集成与金额换算.md)。

Session 的重复抑制只覆盖当前 Stream。生产 Backend/Dispatcher 必须按已认证的
`(payer_id, request_id)` 持久化收费幂等；相同付款人重试返回原收费结果，不同付款人即使
使用相同 request_id 也分别处理。

根入口以及 sat-subscription-protocol/protocol、sat-subscription-protocol/wire、sat-subscription-protocol/client、
sat-subscription-protocol/subscription、sat-subscription-protocol/service 子入口均可用。字段、错误分类和 Go 对照
见仓库 [SDK 使用说明](../docs/SDK使用.md)。
