/** SSP 客户端报文构造和指定报文解析 API。 */
import {
  cloneBytes,
  ErrorCode,
  Kind,
  Publish,
  SSPError,
} from "../protocol/index.js";
import {
  decodePublishDocument,
  decodeSubscriptionsResponseDocument,
  encodePublishDocument,
  encodeSubscribeDocument,
  encodeSubscriptionsRequestDocument,
  encodeUnsubscribeDocument,
  encodeWire,
  parse,
} from "../wire/index.js";

/** 创建 Publish Wire；JSON 字节会保持 exact bytes。 */
export function newPublish(channel: string, contentJson: Uint8Array): Uint8Array {
  const document = encodePublishDocument({ channel, contentJson: cloneBytes(contentJson) });
  return encodeWire(Kind.Publish, document);
}

/** 创建 Subscribe Wire，可订阅普通 channel 或精确名称 "*"。 */
export function newSubscribe(channel: string): Uint8Array {
  return encodeWire(Kind.Subscribe, encodeSubscribeDocument(channel));
}

/** 创建 Unsubscribe Wire，可取消普通 channel 或精确名称 "*"。 */
export function newUnsubscribe(channel: string): Uint8Array {
  return encodeWire(Kind.Unsubscribe, encodeUnsubscribeDocument(channel));
}

/** 创建查询订阅集合的 SubscriptionsRequest Wire。 */
export function newSubscriptionsRequest(): Uint8Array {
  return encodeWire(Kind.SubscriptionsRequest, encodeSubscriptionsRequestDocument());
}

/** 严格解析 Publish Wire，返回原始 contentJson 字节。 */
export function parsePublish(wireCbor: Uint8Array): Publish {
  const message = parse(wireCbor);
  if (message.kind !== Kind.Publish) {
    throw new SSPError(ErrorCode.INVALID_WIRE, "期望 Publish Wire");
  }
  return decodePublishDocument(message.document);
}

/** 严格解析 SubscriptionsResponse Wire，返回规范订阅列表新副本。 */
export function parseSubscriptionsResponse(wireCbor: Uint8Array): readonly string[] {
  const message = parse(wireCbor);
  if (message.kind !== Kind.SubscriptionsResponse) {
    throw new SSPError(ErrorCode.INVALID_WIRE, "期望 SubscriptionsResponse Wire");
  }
  return decodeSubscriptionsResponseDocument(message.document);
}
