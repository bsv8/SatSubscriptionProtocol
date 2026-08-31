/** 无网络 SSP 服务入口、Session 和窄 Dispatcher 抽象。 */
import {
  ActionErrorCode,
  Amount,
  cloneBytes,
  ErrorCode,
  Kind,
  MAX_REQUEST_IDS_PER_SESSION,
  PublicKey,
  RequestId,
  SSPError,
  validateAmount,
  validatePublicKey,
} from "../protocol/index.js";
import {
  decodeRequest,
  encodeActionResultDocument,
  encodeSubscriptionsResponseDocument,
  encodeWire,
  parseRequestEnvelope,
} from "../wire/index.js";
import {
  Backend,
  normalizeSubscriptions,
  SubscriptionMutationResult,
  SubscriptionQueryResult,
} from "../subscription/index.js";

/** 交给外部 Dispatcher 的完整 Publish 事件。 */
export interface PublishEvent {
  /** 发布请求编号；订阅者不应把它当作广播消息 ID。 */
  readonly requestId: RequestId;
  /** 外层认证得到的发布者公钥，不来自 SSP payload。 */
  readonly publisherPublicKey: Uint8Array;
  /** Publish 的普通 channel。 */
  readonly channel: string;
  /** Publish 的 JSON exact bytes 防御性副本。 */
  readonly contentJson: Uint8Array;
  /** 收到的完整 Publish Wire exact bytes 防御性副本。 */
  readonly wire: Uint8Array;
}

/** Dispatcher 的结果形状；成功值必须是已实际扣除的金额。 */
export type DispatchResult = Amount | { readonly chargedAmount: Amount };

/** 无网络 Dispatcher 接口。 */
export interface Dispatcher {
  /** 接收已认证发布者和完整 Publish 事件，返回实际扣费金额。 */
  dispatch(event: PublishEvent): DispatchResult | Promise<DispatchResult>;
}

/** 单次便捷入口的返回值；仅成功响应时包含 response。 */
export interface HandleMessageResult {
  /** 需要写回发起请求的同一连接的 Response Wire。 */
  readonly response?: Uint8Array;
  /** 本地诊断用的稳定错误；不进入 response Wire。 */
  readonly cause?: SSPError;
}

/** Session 的返回值；suppressed 始终表示本次是否因重复编号而静默丢弃。 */
export interface SessionResult {
  /** 需要外部 writer 写回原 Stream 的完整 SSP Wire。 */
  readonly response?: Uint8Array;
  /** 相同 request_id 已登记时为 true，此时没有响应。 */
  readonly suppressed: boolean;
  /** 本地诊断用的稳定错误；不进入 response Wire。 */
  readonly cause?: SSPError;
}

/** 每条 Stream 独立的 request_id 登记与业务处理 Session。 */
export class Session {
  private readonly remotePublicKey: Uint8Array;
  private readonly backend: Backend | undefined;
  private readonly dispatcher: Dispatcher | undefined;
  private readonly seen = new Set<string>();
  private closed = false;

  /** 创建一个 Session；编号集合只在此 Session 生命周期内有效。 */
  constructor(remotePublicKey: PublicKey, backend?: Backend, dispatcher?: Dispatcher) {
    this.remotePublicKey = validatePublicKey(remotePublicKey);
    this.backend = backend;
    this.dispatcher = dispatcher;
  }

  /** 关闭 Session、释放编号集合；不会强制取消已经开始的异步业务调用。 */
  close(): void {
    this.closed = true;
    this.seen.clear();
  }

  /** 大写命名兼容别名。 */
  Close(): void {
    this.close();
  }

  /**
   * 解析并处理一个完整 SSP Wire。
   * 解析与编号登记在第一次 await 前同步完成，因此不同编号可以并行，重复编号只执行一次。
   */
  async handleMessage(wireCbor: Uint8Array): Promise<SessionResult> {
    this.ensureOpen();
    const envelope = parseRequestEnvelope(wireCbor);
    const requestKey = toHex(envelope.requestId);
    if (this.seen.has(requestKey)) {
      return { suppressed: true };
    }
    if (this.seen.size >= MAX_REQUEST_IDS_PER_SESSION) {
      throw new SSPError(ErrorCode.SESSION_LIMIT, `Session request_id 数量已达到 ${MAX_REQUEST_IDS_PER_SESSION}`);
    }
    // 登记在任何 Backend/Dispatcher await 之前完成；只保护 Set 操作，不锁住业务处理。
    this.seen.add(requestKey);

    let request;
    try {
      request = decodeRequest(envelope);
    } catch {
      return { response: actionResult(envelope.requestId, false, "0", ActionErrorCode.INVALID_REQUEST), suppressed: false };
    }

    switch (request.kind) {
      case Kind.Subscribe:
        return this.handleSubscribe(request.subscribe!, envelope.requestId);
      case Kind.Unsubscribe:
        return this.handleUnsubscribe(request.unsubscribe!, envelope.requestId);
      case Kind.SubscriptionsRequest:
        return this.handleSubscriptions(request.subscriptionsRequest!, envelope.requestId);
      case Kind.Publish:
        return this.handlePublish(request.publish!, envelope.requestId, wireCbor);
      default:
        throw new SSPError(ErrorCode.UNSUPPORTED_KIND, "不是客户端请求 Kind");
    }
  }

  private async handleSubscribe(request: { channel: string; requestId: RequestId }, requestId: RequestId): Promise<SessionResult> {
    if (!this.backend || typeof this.backend.subscribe !== "function") {
      return this.failureWithCause(requestId, ActionErrorCode.INTERNAL_ERROR,
        new SSPError(ErrorCode.BACKEND_FAILURE, "Backend 未配置"), ErrorCode.BACKEND_FAILURE);
    }
    try {
      const returned = await this.backend.subscribe(this.remotePublicKey, request.requestId, request.channel);
      const amount = mutationAmount(returned);
      validateAmount(amount);
      return { response: actionResult(requestId, true, amount, ""), suppressed: false };
    } catch (error) {
      return this.failureWithCause(requestId, isRejected(error) ? ActionErrorCode.REJECTED : ActionErrorCode.INTERNAL_ERROR,
        error, ErrorCode.BACKEND_FAILURE);
    }
  }

  private async handleUnsubscribe(request: { channel: string; requestId: RequestId }, requestId: RequestId): Promise<SessionResult> {
    if (!this.backend || typeof this.backend.unsubscribe !== "function") {
      return this.failureWithCause(requestId, ActionErrorCode.INTERNAL_ERROR,
        new SSPError(ErrorCode.BACKEND_FAILURE, "Backend 未配置"), ErrorCode.BACKEND_FAILURE);
    }
    try {
      const returned = await this.backend.unsubscribe(this.remotePublicKey, request.requestId, request.channel);
      const amount = mutationAmount(returned);
      validateAmount(amount);
      return { response: actionResult(requestId, true, amount, ""), suppressed: false };
    } catch (error) {
      return this.failureWithCause(requestId, isRejected(error) ? ActionErrorCode.REJECTED : ActionErrorCode.INTERNAL_ERROR,
        error, ErrorCode.BACKEND_FAILURE);
    }
  }

  private async handleSubscriptions(request: { requestId: RequestId }, requestId: RequestId): Promise<SessionResult> {
    if (!this.backend || typeof this.backend.subscriptions !== "function") {
      return this.failureWithCause(requestId, ActionErrorCode.INTERNAL_ERROR,
        new SSPError(ErrorCode.BACKEND_FAILURE, "Backend 未配置"), ErrorCode.BACKEND_FAILURE);
    }
    try {
      const returned = await this.backend.subscriptions(this.remotePublicKey, request.requestId);
      const { channels, chargedAmount } = queryResult(returned);
      const normalized = normalizeSubscriptions(channels);
      validateAmount(chargedAmount);
      const document = encodeSubscriptionsResponseDocument({
        requestId,
        chargedAmount,
        channels: normalized,
      });
      return { response: encodeWire(Kind.SubscriptionsResponse, document), suppressed: false };
    } catch (error) {
      return this.failureWithCause(requestId, isRejected(error) ? ActionErrorCode.REJECTED : ActionErrorCode.INTERNAL_ERROR,
        error, ErrorCode.BACKEND_FAILURE);
    }
  }

  private async handlePublish(request: { requestId: RequestId; channel: string; contentJson: Uint8Array }, requestId: RequestId, wireCbor: Uint8Array): Promise<SessionResult> {
    if (!this.dispatcher || typeof this.dispatcher.dispatch !== "function") {
      return this.failureWithCause(requestId, ActionErrorCode.INTERNAL_ERROR,
        new SSPError(ErrorCode.DISPATCH_FAILURE, "Dispatcher 未配置"), ErrorCode.DISPATCH_FAILURE);
    }
    const event: PublishEvent = {
      requestId: cloneBytes(request.requestId),
      publisherPublicKey: cloneBytes(this.remotePublicKey),
      channel: request.channel,
      contentJson: cloneBytes(request.contentJson),
      wire: cloneBytes(wireCbor),
    };
    try {
      const returned = await this.dispatcher.dispatch(event);
      const amount = dispatchAmount(returned);
      validateAmount(amount);
      return { response: actionResult(requestId, true, amount, ""), suppressed: false };
    } catch (error) {
      return this.failureWithCause(requestId, isRejected(error) ? ActionErrorCode.REJECTED : ActionErrorCode.INTERNAL_ERROR,
        error, ErrorCode.DISPATCH_FAILURE);
    }
  }

  private failureWithCause(
    requestId: RequestId,
    errorCode: typeof ActionErrorCode[keyof typeof ActionErrorCode],
    cause: unknown,
    diagnosticCode: typeof ErrorCode.BACKEND_FAILURE | typeof ErrorCode.DISPATCH_FAILURE,
  ): SessionResult {
    const response = actionResult(requestId, false, "0", errorCode);
    return {
      response,
      suppressed: false,
      cause: new SSPError(diagnosticCode, "业务 Backend/Dispatcher 处理失败", { cause }),
    };
  }

  private ensureOpen(): void {
    if (this.closed) {
      throw new SSPError(ErrorCode.SESSION_CLOSED, "Session 已关闭");
    }
  }
}

/** 创建 Session 的函数式入口。 */
export function newSession(remotePublicKey: PublicKey, backend?: Backend, dispatcher?: Dispatcher): Session {
  return new Session(remotePublicKey, backend, dispatcher);
}

/**
 * 单次便捷入口。长期 Stream 应复用 Session，以启用重复 request_id 抑制和数量上限。
 */
export async function handleMessage(
  remotePublicKey: PublicKey,
  wireCbor: Uint8Array,
  backend: Backend,
  dispatcher: Dispatcher,
): Promise<HandleMessageResult> {
  const session = new Session(remotePublicKey, backend, dispatcher);
  try {
    const result = await session.handleMessage(wireCbor);
    return result.response
      ? { response: cloneBytes(result.response), ...(result.cause ? { cause: result.cause } : {}) }
      : {};
  } finally {
    session.close();
  }
}

/** 只记录事件并返回免费金额的线程/事件循环安全 Dispatcher，仅用于测试和示例。 */
export class RecordingDispatcher implements Dispatcher {
  private readonly recorded: PublishEvent[] = [];

  /** 记录事件并复制所有可变字节切片。 */
  dispatch(event: PublishEvent): Amount {
    this.recorded.push({
      requestId: cloneBytes(event.requestId),
      publisherPublicKey: cloneBytes(event.publisherPublicKey),
      channel: event.channel,
      contentJson: cloneBytes(event.contentJson),
      wire: cloneBytes(event.wire),
    });
    return "0";
  }

  /** 返回已记录事件的深度副本。 */
  events(): readonly PublishEvent[] {
    return this.recorded.map((event) => ({
      requestId: cloneBytes(event.requestId),
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

function actionResult(requestId: RequestId, success: boolean, chargedAmount: Amount, errorCode: "" | typeof ActionErrorCode[keyof typeof ActionErrorCode]): Uint8Array {
  const document = encodeActionResultDocument({
    requestId: cloneBytes(requestId),
    success,
    chargedAmount,
    errorCode,
  });
  return encodeWire(Kind.ActionResult, document);
}

function mutationAmount(value: SubscriptionMutationResult): Amount {
  if (typeof value === "string") {
    return value;
  }
  if (value && typeof value === "object" && typeof value.chargedAmount === "string") {
    return value.chargedAmount;
  }
  throw new SSPError(ErrorCode.BACKEND_FAILURE, "Backend 未返回 charged_amount");
}

function queryResult(value: SubscriptionQueryResult): { channels: readonly string[]; chargedAmount: Amount } {
  if (value && typeof value === "object" && !Array.isArray(value) && "channels" in value && "chargedAmount" in value) {
    const result = value as SubscriptionQueryResult;
    return { channels: result.channels, chargedAmount: result.chargedAmount };
  }
  throw new SSPError(ErrorCode.BACKEND_FAILURE, "Backend 未返回 channels 和 charged_amount");
}

function dispatchAmount(value: DispatchResult): Amount {
  if (typeof value === "string") {
    return value;
  }
  if (value && typeof value === "object" && typeof value.chargedAmount === "string") {
    return value.chargedAmount;
  }
  throw new SSPError(ErrorCode.DISPATCH_FAILURE, "Dispatcher 未返回 charged_amount");
}

function isRejected(error: unknown): boolean {
  if (error instanceof Error && error.message === ActionErrorCode.REJECTED) {
    return true;
  }
  if (typeof error === "object" && error !== null) {
    const code = (error as { code?: unknown }).code;
    return code === ActionErrorCode.REJECTED;
  }
  return false;
}

function toHex(input: Uint8Array): string {
  let result = "";
  for (const byte of input) {
    result += byte.toString(16).padStart(2, "0");
  }
  return result;
}
