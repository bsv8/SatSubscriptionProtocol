/** 无网络 SSP 服务入口、Session 和窄 Dispatcher 抽象。 */
import {
	ActionErrorCode,
	Amount,
	BillingPage,
	BillingRequest,
	cloneBytes,
  ErrorCode,
	Kind,
  MAX_REQUEST_IDS_PER_SESSION,
  PublicKey,
  RequestId,
	SSPError,
	validateBillingPage,
  validateAmount,
  validatePublicKey,
} from "../protocol/index.js";
import {
	decodeRequest,
	encodeActionResultDocument,
	encodeBillingResponseDocument,
  encodeSubscriptionsResponseDocument,
  encodeWire,
  parseRequestEnvelope,
} from "../wire/index.js";
import {
  Backend,
  normalizeSubscriptions,
  SubscriptionQueryResult,
} from "../subscription/index.js";

/** 交给外部 Dispatcher 的精简 Publish 事件。 */
export interface PublishEvent {
  /** 发布请求编号；订阅者不应把它当作广播消息 ID。 */
  readonly requestId: RequestId;
  /** 外层已认证连接提供的付款人身份，不来自 SSP payload。 */
  readonly payerId: Uint8Array;
  /** Publish 的普通 channel。 */
  readonly channel: string;
  /** Publish 的 JSON exact bytes 防御性副本。 */
  readonly contentJson: Uint8Array;
}

/** 无网络 Dispatcher 接口。 */
export interface Dispatcher {
  /** 接收已认证付款人和精简 Publish 事件，返回实际扣费金额。 */
  dispatch(event: PublishEvent): Amount | Promise<Amount>;
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
		if (envelope.message.kind === Kind.BillingRequest) {
			return { response: billingResult(envelope.requestId, false, "", "", [], "", ActionErrorCode.INVALID_REQUEST), suppressed: false };
		}
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
        return this.handlePublish(request.publish!, envelope.requestId);
      case Kind.BillingRequest:
        return this.handleBilling(request.billingRequest!, envelope.requestId);
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
      const amount = returned;
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
      const amount = returned;
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

  private async handlePublish(request: { requestId: RequestId; channel: string; contentJson: Uint8Array }, requestId: RequestId): Promise<SessionResult> {
    if (!this.dispatcher || typeof this.dispatcher.dispatch !== "function") {
      return this.failureWithCause(requestId, ActionErrorCode.INTERNAL_ERROR,
        new SSPError(ErrorCode.DISPATCH_FAILURE, "Dispatcher 未配置"), ErrorCode.DISPATCH_FAILURE);
    }
    const event: PublishEvent = {
      requestId: cloneBytes(request.requestId),
      payerId: cloneBytes(this.remotePublicKey),
      channel: request.channel,
      contentJson: cloneBytes(request.contentJson),
    };
    try {
      const returned = await this.dispatcher.dispatch(event);
      const amount = returned;
      validateAmount(amount);
      return { response: actionResult(requestId, true, amount, ""), suppressed: false };
    } catch (error) {
      return this.failureWithCause(requestId, isRejected(error) ? ActionErrorCode.REJECTED : ActionErrorCode.INTERNAL_ERROR,
        error, ErrorCode.DISPATCH_FAILURE);
    }
  }

  private async handleBilling(request: BillingRequest, requestId: RequestId): Promise<SessionResult> {
    if (!this.backend || typeof this.backend.listChargeRecords !== "function") {
      return this.billingFailureWithCause(requestId, ActionErrorCode.INTERNAL_ERROR,
        new SSPError(ErrorCode.BACKEND_FAILURE, "Backend 未配置账单查询能力"));
    }
    try {
      const page = await this.backend.listChargeRecords(this.remotePublicKey, request);
      validateBillingPage(page);
      if (page.records.length > request.limit) {
        throw new SSPError(ErrorCode.BACKEND_FAILURE, "Backend 返回的账单记录超过请求 limit");
      }
      const response = billingResult(requestId, true, page.currency, page.network, page.records, page.nextCursor, "");
      return { response, suppressed: false };
    } catch (error) {
      return this.billingFailureWithCause(requestId, isRejected(error) ? ActionErrorCode.REJECTED : ActionErrorCode.INTERNAL_ERROR, error);
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

  private billingFailureWithCause(
    requestId: RequestId,
    errorCode: typeof ActionErrorCode[keyof typeof ActionErrorCode],
    cause: unknown,
  ): SessionResult {
    return {
      response: billingResult(requestId, false, "", "", [], "", errorCode),
      suppressed: false,
      cause: new SSPError(ErrorCode.BACKEND_FAILURE, "账单 Backend 处理失败", { cause }),
    };
  }

  private ensureOpen(): void {
    if (this.closed) {
      throw new SSPError(ErrorCode.SESSION_CLOSED, "Session 已关闭");
    }
  }
}

/** 只记录事件并返回免费金额的线程/事件循环安全 Dispatcher，仅用于测试和示例。 */
export class RecordingDispatcher implements Dispatcher {
  private readonly recorded: PublishEvent[] = [];

  /** 记录事件并复制所有可变字节切片。 */
  dispatch(event: PublishEvent): Amount {
    this.recorded.push({
      requestId: cloneBytes(event.requestId),
      payerId: cloneBytes(event.payerId),
      channel: event.channel,
      contentJson: cloneBytes(event.contentJson),
    });
    return "0";
  }

  /** 返回已记录事件的深度副本。 */
  events(): readonly PublishEvent[] {
    return this.recorded.map((event) => ({
      requestId: cloneBytes(event.requestId),
      payerId: cloneBytes(event.payerId),
      channel: event.channel,
      contentJson: cloneBytes(event.contentJson),
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

function billingResult(
  requestId: RequestId,
  success: boolean,
  currency: string,
  network: string,
  records: BillingPage["records"],
  nextCursor: string,
  errorCode: "" | typeof ActionErrorCode[keyof typeof ActionErrorCode],
): Uint8Array {
  return encodeWire(Kind.BillingResponse, encodeBillingResponseDocument({
    requestId: cloneBytes(requestId),
    success,
    currency,
    network,
    records,
    nextCursor,
    errorCode,
  }));
}

function queryResult(value: SubscriptionQueryResult): { channels: readonly string[]; chargedAmount: Amount } {
  if (value && typeof value === "object" && !Array.isArray(value) && "channels" in value && "chargedAmount" in value) {
    const result = value as SubscriptionQueryResult;
    return { channels: result.channels, chargedAmount: result.chargedAmount };
  }
  throw new SSPError(ErrorCode.BACKEND_FAILURE, "Backend 未返回 channels 和 charged_amount");
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
