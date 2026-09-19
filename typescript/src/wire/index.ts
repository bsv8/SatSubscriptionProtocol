/** SSP V1 deterministic CBOR Wire 编解码。 */
import {
	ActionResult,
	BillingRequest,
	BillingResponse,
	ChargeRecord,
	cloneBytes,
  compareChannelsUtf8,
  ErrorCode,
  isKind,
	Kind,
	MAX_AMOUNT_BYTES,
	MAX_BILLING_CURSOR_BYTES,
	MAX_BILLING_RECORDS,
	MAX_BILLING_TEXT_BYTES,
  MAX_CHANNEL_UTF8_BYTES,
  MAX_CONTENT_JSON_BYTES,
  MAX_DOCUMENT_BYTES,
  MAX_SUBSCRIPTIONS,
  MAX_WIRE_BYTES,
  Publish,
  RequestId,
  REQUEST_ID_BYTES,
  SSPError,
  Subscribe,
  SubscriptionsRequest,
  SubscriptionsResponse,
  Unsubscribe,
	validateActionResult,
	validateAmount,
	validateBillingRequest,
	validateBillingResponse,
	validateChargeRecord,
  validateChannel,
  validateJson,
  validateRequestId,
  WireKind,
  WIRE_VERSION,
} from "../protocol/index.js";
import { concatBytes, Decoder, encodeHead, equalBytes } from "./cbor.js";

/** 严格解析后的外层 SSP Wire。 */
export interface Message {
  /** Wire 版本，V1 固定为 1。 */
  readonly version: number;
	/** 八种固定 SSP Kind 之一。 */
  readonly kind: WireKind;
  /** 内层 document_cbor 的 exact bytes 防御性副本。 */
  readonly document: Uint8Array;
}

/** 只完成外层和 request_id 解析的请求包络。 */
export interface RequestEnvelope {
  /** 已通过外层 canonical 校验的消息。 */
  readonly message: Message;
  /** 请求文档首字段中的 32 字节 request_id 副本。 */
  readonly requestId: RequestId;
}

/** 具体业务字段校验后的请求联合结果。 */
export interface RequestDocument {
  /** 请求 Kind。 */
  readonly kind: WireKind;
  /** 请求编号。 */
  readonly requestId: RequestId;
  /** Publish 字段；仅 Publish Kind 有值。 */
  readonly publish?: Publish;
  /** Subscribe 字段；仅 Subscribe Kind 有值。 */
  readonly subscribe?: Subscribe;
  /** Unsubscribe 字段；仅 Unsubscribe Kind 有值。 */
  readonly unsubscribe?: Unsubscribe;
	/** SubscriptionsRequest 字段；仅查询 Kind 有值。 */
	readonly subscriptionsRequest?: SubscriptionsRequest;
	/** BillingRequest 字段；仅账单查询 Kind 有值。 */
	readonly billingRequest?: BillingRequest;
}

/** 将已验证内层文档编码为 [version, kind, document_cbor]。 */
export function encodeWire(kind: WireKind, document: Uint8Array): Uint8Array {
  if (!isKind(kind)) {
		throw new SSPError(ErrorCode.UNSUPPORTED_KIND, "Kind 不属于 SSP V1 八种固定类型");
  }
  ensureDocumentSize(document);
  const documentCopy = cloneBytes(document);
  validateDocument(kind, documentCopy);
  return encodeWireUnchecked(kind, documentCopy);
}

/** 严格解析完整 Wire，并校验对应内层文档和 canonical bytes。 */
export function parse(input: Uint8Array): Message {
  const message = parseOuter(input);
  validateDocument(message.kind, message.document);
  return message;
}

/**
 * 只解析外层和 request_id，不校验后续业务字段。
 * Session 使用它在业务字段错误时仍然返回 INVALID_REQUEST。
 */
export function parseRequestEnvelope(input: Uint8Array): RequestEnvelope {
  const message = parseOuter(input);
  if (!isRequestKind(message.kind)) {
		throw new SSPError(ErrorCode.INVALID_WIRE, "只有五种客户端请求包含 request_id");
  }
  const decoder = new Decoder(message.document);
  // 前缀阶段只需要确认“有首字段”，不能因为后续多字段而提前丢失
  // request_id；具体数组长度由 decodeRequest 的完整校验决定。
  const count = decoder.readHead(4);
  if (count < 1) {
    throw new SSPError(ErrorCode.INVALID_WIRE, "请求文档缺少 request_id");
  }
  const requestId = validateRequestId(decoder.readFixedBytes(REQUEST_ID_BYTES));
  return { message, requestId };
}

/** 校验 RequestEnvelope 中 request_id 之后的全部业务字段。 */
export function decodeRequest(envelope: RequestEnvelope): RequestDocument {
  const requestId = validateRequestId(envelope.requestId);
  switch (envelope.message.kind) {
    case Kind.Publish: {
      const publish = decodePublishDocument(envelope.message.document);
      ensureSameRequestId(publish.requestId, requestId);
      return { kind: Kind.Publish, requestId, publish };
    }
    case Kind.Subscribe: {
      const subscribe = decodeSubscribeDocument(envelope.message.document);
      ensureSameRequestId(subscribe.requestId, requestId);
      return { kind: Kind.Subscribe, requestId, subscribe };
    }
    case Kind.Unsubscribe: {
      const unsubscribe = decodeUnsubscribeDocument(envelope.message.document);
      ensureSameRequestId(unsubscribe.requestId, requestId);
      return { kind: Kind.Unsubscribe, requestId, unsubscribe };
    }
    case Kind.SubscriptionsRequest: {
		const subscriptionsRequest = decodeSubscriptionsRequestDocument(envelope.message.document);
		ensureSameRequestId(subscriptionsRequest.requestId, requestId);
		return { kind: Kind.SubscriptionsRequest, requestId, subscriptionsRequest };
	}
	case Kind.BillingRequest: {
		const billingRequest = decodeBillingRequestDocument(envelope.message.document);
		ensureSameRequestId(billingRequest.requestId, requestId);
		return { kind: Kind.BillingRequest, requestId, billingRequest };
	}
    default:
      throw new SSPError(ErrorCode.INVALID_WIRE, "不是客户端请求 Kind");
  }
}

/** 编码 Publish 内层文档 [request_id, channel, content_json]。 */
export function encodePublishDocument(publish: Publish): Uint8Array {
  const requestId = validateRequestId(publish.requestId);
  validateChannel(publish.channel, false);
  validateJson(publish.contentJson);
  const channel = new TextEncoder().encode(publish.channel);
  const result = concatBytes([
    encodeHead(4, 3),
    encodeHead(2, requestId.byteLength),
    requestId,
    encodeHead(3, channel.byteLength),
    channel,
    encodeHead(2, publish.contentJson.byteLength),
    cloneBytes(publish.contentJson),
  ]);
  if (result.byteLength > MAX_DOCUMENT_BYTES) {
    throw new SSPError(ErrorCode.MESSAGE_TOO_LARGE, "Publish document 超过长度限制");
  }
  return result;
}

/** 严格解析 Publish 内层文档，并复制 request_id/contentJson。 */
export function decodePublishDocument(document: Uint8Array): Publish {
  ensureDocumentSize(document);
  const decoder = new Decoder(document);
  decoder.readArrayExact(3);
  const requestId = validateRequestId(decoder.readFixedBytes(REQUEST_ID_BYTES));
  const channel = decoder.readText(MAX_CHANNEL_UTF8_BYTES, ErrorCode.INVALID_CHANNEL);
  validateChannel(channel, false);
  const contentJson = decoder.readBytes(MAX_CONTENT_JSON_BYTES, ErrorCode.MESSAGE_TOO_LARGE);
  if (!decoder.atEnd()) {
    throw new SSPError(ErrorCode.INVALID_WIRE, "Publish document 存在尾随字节");
  }
  validateJson(contentJson);
  const candidate = { requestId, channel, contentJson: cloneBytes(contentJson) };
  const canonical = encodePublishDocument(candidate);
  if (!equalBytes(canonical, document)) {
    throw new SSPError(ErrorCode.NON_CANONICAL_CBOR, "Publish document 不是 deterministic CBOR");
  }
  return candidate;
}

/** 编码 Subscribe 内层文档 [request_id, channel]。 */
export function encodeSubscribeDocument(subscribe: Subscribe): Uint8Array {
  return encodeChannelRequestDocument(subscribe.requestId, subscribe.channel);
}

/** 严格解析 Subscribe 内层文档。 */
export function decodeSubscribeDocument(document: Uint8Array): Subscribe {
  const { requestId, channel } = decodeChannelRequestDocument(document);
  return { requestId, channel };
}

/** 编码 Unsubscribe 内层文档 [request_id, channel]。 */
export function encodeUnsubscribeDocument(unsubscribe: Unsubscribe): Uint8Array {
  return encodeChannelRequestDocument(unsubscribe.requestId, unsubscribe.channel);
}

/** 严格解析 Unsubscribe 内层文档。 */
export function decodeUnsubscribeDocument(document: Uint8Array): Unsubscribe {
  const { requestId, channel } = decodeChannelRequestDocument(document);
  return { requestId, channel };
}

/** 编码 SubscriptionsRequest 文档 [request_id]。 */
export function encodeSubscriptionsRequestDocument(request: SubscriptionsRequest): Uint8Array {
  const requestId = validateRequestId(request.requestId);
  return concatBytes([encodeHead(4, 1), encodeHead(2, requestId.byteLength), requestId]);
}

/** 严格解析 SubscriptionsRequest 文档。 */
export function decodeSubscriptionsRequestDocument(document: Uint8Array): SubscriptionsRequest {
  ensureDocumentSize(document);
  const decoder = new Decoder(document);
  decoder.readArrayExact(1);
  const requestId = validateRequestId(decoder.readFixedBytes(REQUEST_ID_BYTES));
  if (!decoder.atEnd()) {
    throw new SSPError(ErrorCode.INVALID_WIRE, "SubscriptionsRequest document 存在尾随字节");
  }
  const candidate = { requestId };
  if (!equalBytes(encodeSubscriptionsRequestDocument(candidate), document)) {
    throw new SSPError(ErrorCode.NON_CANONICAL_CBOR, "SubscriptionsRequest document 不是 deterministic CBOR");
  }
	return candidate;
}

/** 编码 BillingRequest 文档 [request_id, from_ms, to_ms, limit, cursor]。 */
export function encodeBillingRequestDocument(request: BillingRequest): Uint8Array {
	validateBillingRequest(request);
	const requestId = validateRequestId(request.requestId);
	const cursor = new TextEncoder().encode(request.cursor);
	const result = concatBytes([
		encodeHead(4, 5),
		encodeHead(2, requestId.byteLength),
		requestId,
		encodeHead(0, request.fromMs),
		encodeHead(0, request.toMs),
		encodeHead(0, request.limit),
		encodeHead(3, cursor.byteLength),
		cursor,
	]);
	if (result.byteLength > MAX_DOCUMENT_BYTES) {
		throw new SSPError(ErrorCode.MESSAGE_TOO_LARGE, "BillingRequest document 超过长度限制");
	}
	return result;
}

/** 严格解析 BillingRequest 文档。 */
export function decodeBillingRequestDocument(document: Uint8Array): BillingRequest {
	ensureDocumentSize(document);
	const decoder = new Decoder(document);
	decoder.readArrayExact(5);
	const requestId = validateRequestId(decoder.readFixedBytes(REQUEST_ID_BYTES));
	const fromMs = decoder.readUint();
	const toMs = decoder.readUint();
	const limitValue = decoder.readUint();
	if (limitValue > BigInt(Number.MAX_SAFE_INTEGER)) {
		throw new SSPError(ErrorCode.INVALID_WIRE, "billing limit 超过安全整数范围");
	}
	const cursor = decoder.readTextAllowEmpty(MAX_BILLING_CURSOR_BYTES, ErrorCode.INVALID_WIRE);
	if (!decoder.atEnd()) {
		throw new SSPError(ErrorCode.INVALID_WIRE, "BillingRequest document 存在尾随字节");
	}
	const candidate: BillingRequest = {
		requestId,
		fromMs,
		toMs,
		limit: Number(limitValue),
		cursor,
	};
	validateBillingRequest(candidate);
	if (!equalBytes(encodeBillingRequestDocument(candidate), document)) {
		throw new SSPError(ErrorCode.NON_CANONICAL_CBOR, "BillingRequest document 不是 deterministic CBOR");
	}
	return { ...candidate, requestId: cloneBytes(requestId) };
}

/** 编码订阅列表响应文档 [request_id, charged_amount, channels]。 */
export function encodeSubscriptionsResponseDocument(response: SubscriptionsResponse): Uint8Array {
  const requestId = validateRequestId(response.requestId);
  validateAmount(response.chargedAmount);
  validateCanonicalChannels(response.channels);
  const amount = new TextEncoder().encode(response.chargedAmount);
  const parts: Uint8Array[] = [
    encodeHead(4, 3),
    encodeHead(2, requestId.byteLength),
    requestId,
    encodeHead(3, amount.byteLength),
    amount,
    encodeHead(4, response.channels.length),
  ];
  for (const channel of response.channels) {
    const channelBytes = new TextEncoder().encode(channel);
    parts.push(encodeHead(3, channelBytes.byteLength), channelBytes);
  }
  const result = concatBytes(parts);
  if (result.byteLength > MAX_DOCUMENT_BYTES) {
    throw new SSPError(ErrorCode.MESSAGE_TOO_LARGE, "SubscriptionsResponse document 超过长度限制");
  }
  return result;
}

/** 严格解析并返回订阅列表响应的新副本。 */
export function decodeSubscriptionsResponseDocument(document: Uint8Array): SubscriptionsResponse {
  ensureDocumentSize(document);
  const decoder = new Decoder(document);
  decoder.readArrayExact(3);
  const requestId = validateRequestId(decoder.readFixedBytes(REQUEST_ID_BYTES));
  const chargedAmount = decoder.readText(MAX_AMOUNT_BYTES, ErrorCode.INVALID_WIRE);
  validateAmount(chargedAmount);
  const count = decoder.readArrayCount(MAX_SUBSCRIPTIONS, ErrorCode.INVALID_SUBSCRIPTIONS);
  const channels: string[] = [];
  for (let index = 0; index < count; index += 1) {
    const channel = decoder.readText(MAX_CHANNEL_UTF8_BYTES, ErrorCode.INVALID_SUBSCRIPTIONS);
    channels.push(channel);
  }
  if (!decoder.atEnd()) {
    throw new SSPError(ErrorCode.INVALID_WIRE, "SubscriptionsResponse document 存在尾随字节");
  }
  validateCanonicalChannels(channels);
  const candidate: SubscriptionsResponse = {
    requestId,
    chargedAmount,
    channels: channels.slice(),
  };
  const canonical = encodeSubscriptionsResponseDocument(candidate);
  if (!equalBytes(canonical, document)) {
    throw new SSPError(ErrorCode.NON_CANONICAL_CBOR, "SubscriptionsResponse document 不是 deterministic CBOR");
  }
  return { ...candidate, requestId: cloneBytes(requestId), channels: channels.slice() };
}

/** 编码 ActionResult 文档 [request_id, success, charged_amount, error_code]。 */
export function encodeActionResultDocument(result: ActionResult): Uint8Array {
  const requestId = validateRequestId(result.requestId);
  validateActionResult(result);
  const amount = new TextEncoder().encode(result.chargedAmount);
  const errorCode = new TextEncoder().encode(result.errorCode);
  const encoded = concatBytes([
    encodeHead(4, 4),
    encodeHead(2, requestId.byteLength),
    requestId,
    new Uint8Array([result.success ? 0xf5 : 0xf4]),
    encodeHead(3, amount.byteLength),
    amount,
    encodeHead(3, errorCode.byteLength),
    errorCode,
  ]);
  if (encoded.byteLength > MAX_DOCUMENT_BYTES) {
    throw new SSPError(ErrorCode.MESSAGE_TOO_LARGE, "ActionResult document 超过长度限制");
  }
  return encoded;
}

/** 严格解析 ActionResult 文档并校验字段组合。 */
export function decodeActionResultDocument(document: Uint8Array): ActionResult {
  ensureDocumentSize(document);
  const decoder = new Decoder(document);
  decoder.readArrayExact(4);
  const requestId = validateRequestId(decoder.readFixedBytes(REQUEST_ID_BYTES));
  const success = decoder.readBool();
  const chargedAmount = decoder.readText(MAX_AMOUNT_BYTES, ErrorCode.INVALID_WIRE);
  validateAmount(chargedAmount);
  const errorCode = decoder.readTextAllowEmpty(MAX_AMOUNT_BYTES, ErrorCode.INVALID_WIRE);
  if (!decoder.atEnd()) {
    throw new SSPError(ErrorCode.INVALID_WIRE, "ActionResult document 存在尾随字节");
  }
  const candidate = { requestId, success, chargedAmount, errorCode } as ActionResult;
  validateActionResult(candidate);
  const canonical = encodeActionResultDocument(candidate);
  if (!equalBytes(canonical, document)) {
    throw new SSPError(ErrorCode.NON_CANONICAL_CBOR, "ActionResult document 不是 deterministic CBOR");
  }
	return { ...candidate, requestId: cloneBytes(requestId) };
}

/** 编码 BillingResponse 文档 [request_id, success, currency, network, records, next_cursor, error_code]。 */
export function encodeBillingResponseDocument(response: BillingResponse): Uint8Array {
	validateBillingResponse(response);
	const requestId = validateRequestId(response.requestId);
	const currency = new TextEncoder().encode(response.currency);
	const network = new TextEncoder().encode(response.network);
	const nextCursor = new TextEncoder().encode(response.nextCursor);
	const errorCode = new TextEncoder().encode(response.errorCode);
	const parts: Uint8Array[] = [
		encodeHead(4, 7),
		encodeHead(2, requestId.byteLength),
		requestId,
		new Uint8Array([response.success ? 0xf5 : 0xf4]),
		encodeHead(3, currency.byteLength),
		currency,
		encodeHead(3, network.byteLength),
		network,
		encodeHead(4, response.records.length),
	];
	for (const record of response.records) {
		parts.push(encodeChargeRecord(record));
	}
	parts.push(encodeHead(3, nextCursor.byteLength), nextCursor);
	parts.push(encodeHead(3, errorCode.byteLength), errorCode);
	const result = concatBytes(parts);
	if (result.byteLength > MAX_DOCUMENT_BYTES) {
		throw new SSPError(ErrorCode.MESSAGE_TOO_LARGE, "BillingResponse document 超过长度限制");
	}
	return result;
}

/** 严格解析 BillingResponse 文档并校验字段组合。 */
export function decodeBillingResponseDocument(document: Uint8Array): BillingResponse {
	ensureDocumentSize(document);
	const decoder = new Decoder(document);
	decoder.readArrayExact(7);
	const requestId = validateRequestId(decoder.readFixedBytes(REQUEST_ID_BYTES));
	const success = decoder.readBool();
	const currency = decoder.readTextAllowEmpty(MAX_BILLING_TEXT_BYTES, ErrorCode.INVALID_WIRE);
	const network = decoder.readTextAllowEmpty(MAX_BILLING_TEXT_BYTES, ErrorCode.INVALID_WIRE);
	const count = decoder.readArrayCount(MAX_BILLING_RECORDS, ErrorCode.INVALID_WIRE);
	const records: ChargeRecord[] = [];
	for (let index = 0; index < count; index += 1) {
		records.push(decodeChargeRecord(decoder));
	}
	const nextCursor = decoder.readTextAllowEmpty(MAX_BILLING_CURSOR_BYTES, ErrorCode.INVALID_WIRE);
	const errorCode = decoder.readTextAllowEmpty(MAX_BILLING_TEXT_BYTES, ErrorCode.INVALID_WIRE);
	if (!decoder.atEnd()) {
		throw new SSPError(ErrorCode.INVALID_WIRE, "BillingResponse document 存在尾随字节");
	}
	const candidate = {
		requestId,
		success,
		currency,
		network,
		records,
		nextCursor,
		errorCode,
	} as BillingResponse;
	validateBillingResponse(candidate);
	if (!equalBytes(encodeBillingResponseDocument(candidate), document)) {
		throw new SSPError(ErrorCode.NON_CANONICAL_CBOR, "BillingResponse document 不是 deterministic CBOR");
	}
	return {
		...candidate,
		requestId: cloneBytes(requestId),
		records: records.map((record) => ({ ...record, sourceRequestId: cloneBytes(record.sourceRequestId) })),
	};
}

function encodeChargeRecord(record: ChargeRecord): Uint8Array {
	validateChargeRecord(record);
	const chargeId = new TextEncoder().encode(record.chargeId);
	const action = new TextEncoder().encode(record.action);
	const channel = new TextEncoder().encode(record.channel);
	const sourceRequestId = validateRequestId(record.sourceRequestId);
	const amount = new TextEncoder().encode(record.chargedAmount);
	return concatBytes([
		encodeHead(4, 6),
		encodeHead(3, chargeId.byteLength),
		chargeId,
		encodeHead(0, record.occurredAtMs),
		encodeHead(3, action.byteLength),
		action,
		encodeHead(3, channel.byteLength),
		channel,
		encodeHead(2, sourceRequestId.byteLength),
		sourceRequestId,
		encodeHead(3, amount.byteLength),
		amount,
	]);
}

function decodeChargeRecord(decoder: Decoder): ChargeRecord {
	decoder.readArrayExact(6);
	const chargeId = decoder.readText(MAX_BILLING_TEXT_BYTES, ErrorCode.INVALID_WIRE);
	const occurredAtMs = decoder.readUint();
	const action = decoder.readText(MAX_BILLING_TEXT_BYTES, ErrorCode.INVALID_WIRE);
	const channel = decoder.readText(MAX_CHANNEL_UTF8_BYTES, ErrorCode.INVALID_WIRE);
	validateChannel(channel, true);
	const sourceRequestId = validateRequestId(decoder.readFixedBytes(REQUEST_ID_BYTES));
	const chargedAmount = decoder.readText(MAX_AMOUNT_BYTES, ErrorCode.INVALID_WIRE);
	validateAmount(chargedAmount);
	const record: ChargeRecord = {
		chargeId,
		occurredAtMs,
		action,
		channel,
		sourceRequestId,
		chargedAmount,
	};
	validateChargeRecord(record);
	return record;
}

function parseOuter(input: Uint8Array): Message {
  if (!(input instanceof Uint8Array)) {
    throw new SSPError(ErrorCode.INVALID_WIRE, "Wire 必须是 Uint8Array");
  }
  if (input.byteLength > MAX_WIRE_BYTES) {
    throw new SSPError(ErrorCode.MESSAGE_TOO_LARGE, "Wire 超过长度限制");
  }
  const inputCopy = cloneBytes(input);
  const decoder = new Decoder(inputCopy);
  decoder.readArrayExact(3);
  const version = decoder.readUint();
  if (version !== BigInt(WIRE_VERSION)) {
    throw new SSPError(ErrorCode.UNSUPPORTED_VERSION, "Wire 版本不受支持");
  }
  const kindValue = decoder.readUint();
  if (kindValue > BigInt(Number.MAX_SAFE_INTEGER) || !isKind(Number(kindValue))) {
    throw new SSPError(ErrorCode.UNSUPPORTED_KIND, "Wire Kind 不受支持");
  }
  const kind = Number(kindValue) as WireKind;
  const document = decoder.readBytes(MAX_DOCUMENT_BYTES, ErrorCode.MESSAGE_TOO_LARGE);
  if (!decoder.atEnd()) {
    throw new SSPError(ErrorCode.INVALID_WIRE, "Wire 末尾存在尾随字节");
  }
  const canonical = encodeWireUnchecked(kind, document);
  if (!equalBytes(canonical, inputCopy)) {
    throw new SSPError(ErrorCode.NON_CANONICAL_CBOR, "Wire 不是 deterministic CBOR");
  }
  return { version: WIRE_VERSION, kind, document: cloneBytes(document) };
}

function encodeChannelRequestDocument(requestId: RequestId, channel: string): Uint8Array {
  const id = validateRequestId(requestId);
  validateChannel(channel, true);
  const channelBytes = new TextEncoder().encode(channel);
  const result = concatBytes([
    encodeHead(4, 2),
    encodeHead(2, id.byteLength),
    id,
    encodeHead(3, channelBytes.byteLength),
    channelBytes,
  ]);
  if (result.byteLength > MAX_DOCUMENT_BYTES) {
    throw new SSPError(ErrorCode.MESSAGE_TOO_LARGE, "channel request document 超过长度限制");
  }
  return result;
}

function decodeChannelRequestDocument(document: Uint8Array): { requestId: RequestId; channel: string } {
  ensureDocumentSize(document);
  const decoder = new Decoder(document);
  decoder.readArrayExact(2);
  const requestId = validateRequestId(decoder.readFixedBytes(REQUEST_ID_BYTES));
  const channel = decoder.readText(MAX_CHANNEL_UTF8_BYTES, ErrorCode.INVALID_CHANNEL);
  validateChannel(channel, true);
  if (!decoder.atEnd()) {
    throw new SSPError(ErrorCode.INVALID_WIRE, "channel request document 存在尾随字节");
  }
  const candidate = { requestId, channel };
  if (!equalBytes(encodeChannelRequestDocument(requestId, channel), document)) {
    throw new SSPError(ErrorCode.NON_CANONICAL_CBOR, "channel request document 不是 deterministic CBOR");
  }
  return candidate;
}

function ensureSameRequestId(left: RequestId, right: RequestId): void {
  if (!equalBytes(left, right)) {
    throw new SSPError(ErrorCode.INVALID_WIRE, "request_id 在分阶段解析中发生变化");
  }
}

function isRequestKind(kind: WireKind): kind is typeof Kind.Publish | typeof Kind.Subscribe | typeof Kind.Unsubscribe | typeof Kind.SubscriptionsRequest | typeof Kind.BillingRequest {
	return kind === Kind.Publish || kind === Kind.Subscribe || kind === Kind.Unsubscribe || kind === Kind.SubscriptionsRequest || kind === Kind.BillingRequest;
}

function validateDocument(kind: WireKind, document: Uint8Array): void {
  switch (kind) {
    case Kind.Publish:
      decodePublishDocument(document);
      return;
    case Kind.Subscribe:
      decodeSubscribeDocument(document);
      return;
    case Kind.Unsubscribe:
      decodeUnsubscribeDocument(document);
      return;
    case Kind.SubscriptionsRequest:
      decodeSubscriptionsRequestDocument(document);
      return;
    case Kind.SubscriptionsResponse:
      decodeSubscriptionsResponseDocument(document);
      return;
	case Kind.ActionResult:
		decodeActionResultDocument(document);
		return;
	case Kind.BillingRequest:
		decodeBillingRequestDocument(document);
		return;
	case Kind.BillingResponse:
		decodeBillingResponseDocument(document);
		return;
    default:
      throw new SSPError(ErrorCode.UNSUPPORTED_KIND, "Kind 不属于 SSP V1");
  }
}

function encodeWireUnchecked(kind: WireKind, document: Uint8Array): Uint8Array {
  const result = concatBytes([
    encodeHead(4, 3),
    encodeHead(0, WIRE_VERSION),
    encodeHead(0, kind),
    encodeHead(2, document.byteLength),
    cloneBytes(document),
  ]);
  if (result.byteLength > MAX_WIRE_BYTES) {
    throw new SSPError(ErrorCode.MESSAGE_TOO_LARGE, "Wire 超过长度限制");
  }
  return result;
}

function validateCanonicalChannels(channels: readonly string[]): void {
  if (channels.length > MAX_SUBSCRIPTIONS) {
    throw new SSPError(ErrorCode.INVALID_SUBSCRIPTIONS, "订阅数量超过限制");
  }
  if (channels.length === 0) {
    return;
  }
  for (let index = 0; index < channels.length; index += 1) {
    const channel = channels[index];
    try {
      validateChannel(channel, true);
    } catch {
      throw new SSPError(ErrorCode.INVALID_SUBSCRIPTIONS, "订阅列表包含非法 channel");
    }
    if (channel === "*" && channels.length !== 1) {
      throw new SSPError(ErrorCode.INVALID_SUBSCRIPTIONS, "wildcard 不能和普通 channel 混合");
    }
    if (index > 0 && (channels[index - 1] === "*" || compareChannelsUtf8(channels[index - 1], channel) >= 0)) {
      throw new SSPError(ErrorCode.INVALID_SUBSCRIPTIONS, "订阅列表必须唯一并按 UTF-8 字节升序");
    }
  }
}

function ensureDocumentSize(document: Uint8Array): void {
  if (!(document instanceof Uint8Array)) {
    throw new SSPError(ErrorCode.INVALID_WIRE, "document 必须是 Uint8Array");
  }
  if (document.byteLength > MAX_DOCUMENT_BYTES) {
    throw new SSPError(ErrorCode.MESSAGE_TOO_LARGE, "document_cbor 超过长度限制");
  }
}
