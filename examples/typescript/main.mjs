// 示例只调用进程内 SSP SDK，不建立网络连接。
import {
  MemoryBackend,
  RecordingDispatcher,
  Session,
  newPublish,
  newRequestId,
  newSubscribe,
  newSubscriptionsRequest,
  parseActionResult,
  parseSubscriptionsResponse,
} from "@bsv8/ssp";

// remotePublicKey 必须来自外部已认证长期连接；示例使用固定测试公钥。
const remotePublicKey = new Uint8Array(33);
remotePublicKey[0] = 0x02;
const backend = new MemoryBackend();
const dispatcher = new RecordingDispatcher();
const session = new Session(remotePublicKey, backend, dispatcher);

const newsId = newRequestId();
const alertsId = newRequestId();
const [newsResult, alertsResult] = await Promise.all([
  session.handleMessage(newSubscribe(newsId, "news")),
  session.handleMessage(newSubscribe(alertsId, "alerts")),
]);
// 响应可乱序，按 ActionResult.requestId 关联原请求。
for (const result of [newsResult, alertsResult]) {
  const action = parseActionResult(result.response);
  console.log("action result（动作结果）:", action.requestId, action.chargedAmount);
}

const queryId = newRequestId();
const queryResult = await session.handleMessage(newSubscriptionsRequest(queryId));
const subscriptions = parseSubscriptionsResponse(queryResult.response);
console.log("subscriptions（订阅列表）:", subscriptions.channels, "charged_amount（实际扣费）:", subscriptions.chargedAmount);

// 同一编号重复到达会静默丢弃，不执行、不扣费、不返回历史结果。
const duplicate = await session.handleMessage(newSubscribe(newsId, "news"));
console.log("duplicate suppressed（重复编号已丢弃）:", duplicate.suppressed);

const publishId = newRequestId();
const publishResult = await session.handleMessage(newPublish(publishId, "news", new TextEncoder().encode(` {"title":"hello"} `)));
console.log("publish accepted（发布已接受）:", parseActionResult(publishResult.response).success, "events（广播事件）:", dispatcher.events().length);
// Publish 成功表示服务端已接受、扣费并开始广播，不代表订阅者已经收到。
