/** 无网络 SSP 服务入口和窄 Dispatcher 抽象。 */
import {
  cloneBytes,
  ErrorCode,
  Kind,
  PublicKey,
  SSPError,
  validatePublicKey,
} from "../protocol/index.js";
import {
  decodePublishDocument,
  decodeSubscribeDocument,
  decodeSubscriptionsRequestDocument,
  decodeUnsubscribeDocument,
  encodeSubscriptionsResponseDocument,
  encodeWire,
  parse,
} from "../wire/index.js";
import {
  Backend,
  normalizeSubscriptions,
} from "../subscription/index.js";

/** 交给外部 Dispatcher 的完整 Publish 事件。 */
export interface PublishEvent {
  /** 外层认证得到的发布者公钥，不来自 SSP payload。 */
  readonly publisherPublicKey: Uint8Array;
  /** Publish 的普通 channel。 */
  readonly channel: string;
  /** Publish 的 JSON exact bytes 防御性副本。 */
  readonly contentJson: Uint8Array;
  /** 收到的完整 Publish Wire exact bytes 防御性副本。 */
  readonly wire: Uint8Array;
}

/** 无网络 Dispatcher 接口。 */
export interface Dispatcher {
  /** 接收已认证发布者和完整 Publish 事件。 */
  dispatch(event: PublishEvent): void | Promise<void>;
}

/** HandleMessage 的返回值；仅查询请求成功时包含 response。 */
export interface HandleMessageResult {
  /** 需要写回发起查询的同一连接的 Response Wire。 */
  readonly response?: Uint8Array;
}

/**
 * 处理来自已认证长期连接的完整 SSP Wire。
 * Backend 和 Dispatcher 使用明确的四参数无网络调用边界。
 */
export function handleMessage(
  remotePublicKey: PublicKey,
  wireCbor: Uint8Array,
  backend: Backend,
  dispatcher: Dispatcher,
): Promise<HandleMessageResult>;
export async function handleMessage(
  remotePublicKey: PublicKey,
  wireCbor: Uint8Array,
  backend: Backend,
  dispatcher: Dispatcher,
): Promise<HandleMessageResult> {
  const key = validatePublicKey(remotePublicKey);
  const message = parse(wireCbor);

  switch (message.kind) {
    case Kind.Subscribe: {
      if (!backend || typeof backend.subscribe !== "function") {
        throw new SSPError(ErrorCode.BACKEND_FAILURE, "必须提供订阅 Backend");
      }
      const channel = decodeSubscribeDocument(message.document);
      try {
        await backend.subscribe(key, channel);
      } catch {
        throw new SSPError(ErrorCode.BACKEND_FAILURE, "Backend Subscribe 失败");
      }
      return {};
    }
    case Kind.Unsubscribe: {
      if (!backend || typeof backend.unsubscribe !== "function") {
        throw new SSPError(ErrorCode.BACKEND_FAILURE, "必须提供订阅 Backend");
      }
      const channel = decodeUnsubscribeDocument(message.document);
      try {
        await backend.unsubscribe(key, channel);
      } catch {
        throw new SSPError(ErrorCode.BACKEND_FAILURE, "Backend Unsubscribe 失败");
      }
      return {};
    }
    case Kind.SubscriptionsRequest: {
      if (!backend || typeof backend.subscriptions !== "function") {
        throw new SSPError(ErrorCode.BACKEND_FAILURE, "必须提供订阅 Backend");
      }
      decodeSubscriptionsRequestDocument(message.document);
      let channels: readonly string[];
      try {
        channels = await backend.subscriptions(key);
      } catch {
        throw new SSPError(ErrorCode.BACKEND_FAILURE, "Backend Subscriptions 失败");
      }
      const normalized = normalizeSubscriptions(channels);
      const document = encodeSubscriptionsResponseDocument(normalized);
      return { response: encodeWire(Kind.SubscriptionsResponse, document) };
    }
    case Kind.Publish: {
      if (!dispatcher || typeof dispatcher.dispatch !== "function") {
        throw new SSPError(ErrorCode.DISPATCH_FAILURE, "必须提供 Publish Dispatcher");
      }
      const publish = decodePublishDocument(message.document);
      const event: PublishEvent = {
        publisherPublicKey: cloneBytes(key),
        channel: publish.channel,
        contentJson: cloneBytes(publish.contentJson),
        wire: cloneBytes(wireCbor),
      };
      try {
        await dispatcher.dispatch(event);
      } catch {
        throw new SSPError(ErrorCode.DISPATCH_FAILURE, "Dispatcher 处理 Publish 失败");
      }
      return {};
    }
    case Kind.SubscriptionsResponse:
      throw new SSPError(ErrorCode.INVALID_WIRE, "SubscriptionsResponse 不能作为客户端命令");
    default:
      throw new SSPError(ErrorCode.UNSUPPORTED_KIND, "Kind 不属于 SSP V1");
  }
}

/** 只记录事件的线程安全语义 Dispatcher，仅用于测试和示例。 */
export class RecordingDispatcher implements Dispatcher {
  private readonly recorded: PublishEvent[] = [];

  /** 记录事件并复制所有可变字节切片。 */
  dispatch(event: PublishEvent): void {
    this.recorded.push({
      publisherPublicKey: cloneBytes(event.publisherPublicKey),
      channel: event.channel,
      contentJson: cloneBytes(event.contentJson),
      wire: cloneBytes(event.wire),
    });
  }

  /** 返回已记录事件的深度副本。 */
  events(): readonly PublishEvent[] {
    return this.recorded.map((event) => ({
      publisherPublicKey: cloneBytes(event.publisherPublicKey),
      channel: event.channel,
      contentJson: cloneBytes(event.contentJson),
      wire: cloneBytes(event.wire),
    }));
  }

  /** 清空已记录事件。 */
  reset(): void {
    this.recorded.length = 0;
  }
}
