# `@bsv8/ssp`

SSP V1 的无网络核心 SDK（长期连接由外部应用负责）。构建产物提供 ESM、TypeScript
声明和带内联源码的 source map；公开入口与仓库正式文档保持一致。

## 最小示例

```ts
import {
  MemoryBackend,
  RecordingDispatcher,
  handleMessage,
  newPublish,
  newSubscribe,
  newSubscriptionsRequest,
  parseSubscriptionsResponse,
} from "@bsv8/ssp";

// 必须来自外层已认证长期连接；这里仅使用 33 字节形状正确的示例值。
const remotePublicKey = new Uint8Array(33);
remotePublicKey[0] = 0x02;

const backend = new MemoryBackend();
const dispatcher = new RecordingDispatcher();

await handleMessage(remotePublicKey, newSubscribe("news"), backend, dispatcher);
const query = await handleMessage(
  remotePublicKey,
  newSubscriptionsRequest(),
  backend,
  dispatcher,
);
console.log(parseSubscriptionsResponse(query.response)); // ["news"]

const publish = newPublish("news", new TextEncoder().encode(` {"title":"hello"} `));
await handleMessage(remotePublicKey, publish, backend, dispatcher);
```

`newPublish` 保留 `content_json` 原始 UTF-8 字节，不解析后重新序列化。只有
`newSubscriptionsRequest` 会得到直接 `response`；Publish、Subscribe 和 Unsubscribe
成功时返回 `{}`。实际连接、身份认证、分帧和发送由外部应用负责。

## 公开入口

根入口以及 `@bsv8/ssp/protocol`、`@bsv8/ssp/wire`、`@bsv8/ssp/client`、
`@bsv8/ssp/subscription`、`@bsv8/ssp/service` 子入口均可用。错误使用 `SSPError.code`，
字段和错误分类的中文含义见仓库 [SDK 使用说明](../docs/SDK使用.md)。
