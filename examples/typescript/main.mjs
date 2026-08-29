// 示例只调用进程内 SSP SDK，不建立网络连接。
import {
  MemoryBackend,
  RecordingDispatcher,
  handleMessage,
  newPublish,
  newSubscribe,
  newSubscriptionsRequest,
  parseSubscriptionsResponse,
} from "@bsv8/ssp";

// remotePublicKey 必须来自外部已认证长期连接；示例使用固定测试公钥。
const remotePublicKey = new Uint8Array(33);
remotePublicKey[0] = 0x02;
const backend = new MemoryBackend();
const dispatcher = new RecordingDispatcher();

await handleMessage(remotePublicKey, newSubscribe("news"), backend, dispatcher); // 订阅
const queryResult = await handleMessage(remotePublicKey, newSubscriptionsRequest(), backend, dispatcher); // 查询
console.log("subscriptions（订阅列表）:", parseSubscriptionsResponse(queryResult.response));

const publishWire = newPublish("news", new TextEncoder().encode(` {"title":"hello"} `)); // JSON exact bytes
await handleMessage(remotePublicKey, publishWire, backend, dispatcher); // 广播事件交给 Dispatcher
console.log("recorded events（记录的广播事件）:", dispatcher.events().length);
