/** SSP 客户端报文构造和指定报文解析 API。 */
import {
  ActionResult,
  BillingResponse,
  cloneBytes,
  ErrorCode,
  Kind,
  Publish,
  RequestId,
  SSPError,
  SubscriptionsResponse,
} from "../protocol/index.js";
import {
  decodeActionResultDocument,
  decodeBillingResponseDocument,
  decodePublishDocument,
  decodeSubscriptionsResponseDocument,
  encodeActionResultDocument,
  encodeBillingRequestDocument,
  encodeBillingResponseDocument,
  encodePublishDocument,
  encodeSubscribeDocument,
  encodeSubscriptionsRequestDocument,
  encodeSubscriptionsResponseDocument,
  encodeUnsubscribeDocument,
  encodeWire,
  parse,
} from "../wire/index.js";

/** 生成一个使用 Web Crypto 的安全 request_id。 */
export { newRequestId } from "../protocol/index.js";

/** 创建 Publish Wire；JSON 字节会保持 exact bytes。 */
export function newPublish(requestId: RequestId, channel: string, contentJson: Uint8Array): Uint8Array {
  const document = encodePublishDocument({ requestId, channel, contentJson: cloneBytes(contentJson) });
  return encodeWire(Kind.Publish, document);
}

/** 创建 Subscribe Wire，可订阅普通 channel 或精确名称 "*"。 */
export function newSubscribe(requestId: RequestId, channel: string): Uint8Array {
  return encodeWire(Kind.Subscribe, encodeSubscribeDocument({ requestId, channel }));
}

/** 创建 Unsubscribe Wire，可取消普通 channel 或精确名称 "*"。 */
export function newUnsubscribe(requestId: RequestId, channel: string): Uint8Array {
  return encodeWire(Kind.Unsubscribe, encodeUnsubscribeDocument({ requestId, channel }));
}

/** 创建查询订阅集合的 SubscriptionsRequest Wire。 */
export function newSubscriptionsRequest(requestId: RequestId): Uint8Array {
  return encodeWire(Kind.SubscriptionsRequest, encodeSubscriptionsRequestDocument({ requestId }));
}

/** 创建账单分页查询 Wire；时间使用 Unix 毫秒 bigint，第一页 cursor 为空。 */
export function newBillingRequest(
  requestId: RequestId,
  fromMs: bigint,
  toMs: bigint,
  limit: number,
  cursor: string,
): Uint8Array {
  return encodeWire(Kind.BillingRequest, encodeBillingRequestDocument({ requestId, fromMs, toMs, limit, cursor }));
}

/** 创建 ActionResult Wire，供服务实现或测试构造响应。 */
export function newActionResult(result: ActionResult): Uint8Array {
  return encodeWire(Kind.ActionResult, encodeActionResultDocument(result));
}

/** 创建 SubscriptionsResponse Wire，供服务实现或测试构造响应。 */
export function newSubscriptionsResponse(result: SubscriptionsResponse): Uint8Array {
  return encodeWire(Kind.SubscriptionsResponse, encodeSubscriptionsResponseDocument(result));
}

/** 创建 BillingResponse Wire，供服务实现或测试构造响应。 */
export function newBillingResponse(result: BillingResponse): Uint8Array {
  return encodeWire(Kind.BillingResponse, encodeBillingResponseDocument(result));
}

/** 严格解析 Publish Wire，返回 request_id 和原始 contentJson 字节。 */
export function parsePublish(wireCbor: Uint8Array): Publish {
  const message = parse(wireCbor);
  if (message.kind !== Kind.Publish) {
    throw new SSPError(ErrorCode.INVALID_WIRE, "期望 Publish Wire");
  }
  return decodePublishDocument(message.document);
}

/** 严格解析 ActionResult Wire。 */
export function parseActionResult(wireCbor: Uint8Array): ActionResult {
  const message = parse(wireCbor);
  if (message.kind !== Kind.ActionResult) {
    throw new SSPError(ErrorCode.INVALID_WIRE, "期望 ActionResult Wire");
  }
  return decodeActionResultDocument(message.document);
}

/** 严格解析 SubscriptionsResponse Wire。 */
export function parseSubscriptionsResponse(wireCbor: Uint8Array): SubscriptionsResponse {
  const message = parse(wireCbor);
  if (message.kind !== Kind.SubscriptionsResponse) {
    throw new SSPError(ErrorCode.INVALID_WIRE, "期望 SubscriptionsResponse Wire");
  }
  return decodeSubscriptionsResponseDocument(message.document);
}

/** 严格解析 BillingResponse Wire。 */
export function parseBillingResponse(wireCbor: Uint8Array): BillingResponse {
  const message = parse(wireCbor);
  if (message.kind !== Kind.BillingResponse) {
    throw new SSPError(ErrorCode.INVALID_WIRE, "期望 BillingResponse Wire");
  }
  return decodeBillingResponseDocument(message.document);
}
