/** SSP V1 deterministic CBOR Wire 编解码。 */
import {
  cloneBytes,
  compareChannelsUtf8,
  ErrorCode,
  isKind,
  Kind,
  MAX_CHANNEL_UTF8_BYTES,
  MAX_CONTENT_JSON_BYTES,
  MAX_DOCUMENT_BYTES,
  MAX_SUBSCRIPTIONS,
  MAX_WIRE_BYTES,
  Publish,
  SSPError,
  validateChannel,
  validateJson,
  WireKind,
  WIRE_VERSION,
} from "../protocol/index.js";
import { concatBytes, Decoder, encodeHead, equalBytes } from "./cbor.js";

/** 严格解析后的外层 SSP Wire。 */
export interface Message {
  /** Wire 版本，V1 固定为 1。 */
  readonly version: number;
  /** 五种固定 SSP Kind 之一。 */
  readonly kind: WireKind;
  /** 内层 document_cbor 的 exact bytes 防御性副本。 */
  readonly document: Uint8Array;
}

/** 将已验证内层文档编码为 [version, kind, document_cbor]。 */
export function encodeWire(kind: WireKind, document: Uint8Array): Uint8Array {
  if (!isKind(kind)) {
    throw new SSPError(ErrorCode.UNSUPPORTED_KIND, "Kind 不属于 SSP V1 五种固定类型");
  }
  if (!(document instanceof Uint8Array)) {
    throw new SSPError(ErrorCode.INVALID_WIRE, "document 必须是 Uint8Array");
  }
  if (document.byteLength > MAX_DOCUMENT_BYTES) {
    throw new SSPError(ErrorCode.MESSAGE_TOO_LARGE, "document_cbor 超过长度限制");
  }
  const documentCopy = cloneBytes(document);
  validateDocument(kind, documentCopy);
  return encodeWireUnchecked(kind, documentCopy);
}

/** 严格解析完整 Wire，并校验对应内层文档和 canonical bytes。 */
export function parse(input: Uint8Array): Message {
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
  validateDocument(kind, document);
  const canonical = encodeWireUnchecked(kind, document);
  if (!equalBytes(canonical, inputCopy)) {
    throw new SSPError(ErrorCode.NON_CANONICAL_CBOR, "Wire 不是 deterministic CBOR");
  }
  return { version: WIRE_VERSION, kind, document: cloneBytes(document) };
}

/** 编码 Publish 内层文档 [channel, content_json]。 */
export function encodePublishDocument(publish: Publish): Uint8Array {
  validateChannel(publish.channel, false);
  validateJson(publish.contentJson);
  const channel = new TextEncoder().encode(publish.channel);
  const result = concatBytes([
    encodeHead(4, 2),
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

/** 严格解析 Publish 内层文档，并复制 content_json。 */
export function decodePublishDocument(document: Uint8Array): Publish {
  ensureDocumentSize(document);
  const decoder = new Decoder(document);
  decoder.readArrayExact(2);
  const channel = decoder.readText(MAX_CHANNEL_UTF8_BYTES, ErrorCode.INVALID_CHANNEL);
  validateChannel(channel, false);
  const contentJson = decoder.readBytes(MAX_CONTENT_JSON_BYTES, ErrorCode.MESSAGE_TOO_LARGE);
  if (!decoder.atEnd()) {
    throw new SSPError(ErrorCode.INVALID_WIRE, "Publish document 存在尾随字节");
  }
  validateJson(contentJson);
  const canonical = encodePublishDocument({ channel, contentJson });
  if (!equalBytes(canonical, document)) {
    throw new SSPError(ErrorCode.NON_CANONICAL_CBOR, "Publish document 不是 deterministic CBOR");
  }
  return { channel, contentJson: cloneBytes(contentJson) };
}

/** 编码 Subscribe 内层文档 [channel]。 */
export function encodeSubscribeDocument(channel: string): Uint8Array {
  return encodeSingleChannelDocument(channel);
}

/** 严格解析 Subscribe 内层文档。 */
export function decodeSubscribeDocument(document: Uint8Array): string {
  return decodeSingleChannelDocument(document);
}

/** 编码 Unsubscribe 内层文档 [channel]。 */
export function encodeUnsubscribeDocument(channel: string): Uint8Array {
  return encodeSingleChannelDocument(channel);
}

/** 严格解析 Unsubscribe 内层文档。 */
export function decodeUnsubscribeDocument(document: Uint8Array): string {
  return decodeSingleChannelDocument(document);
}

/** 编码空数组 SubscriptionsRequest 文档。 */
export function encodeSubscriptionsRequestDocument(): Uint8Array {
  return new Uint8Array([0x80]);
}

/** 严格解析空数组 SubscriptionsRequest 文档。 */
export function decodeSubscriptionsRequestDocument(document: Uint8Array): void {
  ensureDocumentSize(document);
  const decoder = new Decoder(document);
  decoder.readArrayExact(0);
  if (!decoder.atEnd()) {
    throw new SSPError(ErrorCode.INVALID_WIRE, "SubscriptionsRequest document 存在尾随字节");
  }
  if (!equalBytes(document, encodeSubscriptionsRequestDocument())) {
    throw new SSPError(ErrorCode.NON_CANONICAL_CBOR, "SubscriptionsRequest document 不是 deterministic CBOR");
  }
}

/** 编码规范订阅列表文档 [channels]。 */
export function encodeSubscriptionsResponseDocument(channels: readonly string[]): Uint8Array {
  validateCanonicalChannels(channels);
  const parts: Uint8Array[] = [encodeHead(4, 1), encodeHead(4, channels.length)];
  for (const channel of channels) {
    const channelBytes = new TextEncoder().encode(channel);
    parts.push(encodeHead(3, channelBytes.byteLength), channelBytes);
  }
  const result = concatBytes(parts);
  if (result.byteLength > MAX_DOCUMENT_BYTES) {
    throw new SSPError(ErrorCode.MESSAGE_TOO_LARGE, "SubscriptionsResponse document 超过长度限制");
  }
  return result;
}

/** 严格解析并返回规范订阅列表新副本。 */
export function decodeSubscriptionsResponseDocument(document: Uint8Array): readonly string[] {
  ensureDocumentSize(document);
  const decoder = new Decoder(document);
  decoder.readArrayExact(1);
  const count = decoder.readArrayCount(MAX_SUBSCRIPTIONS, ErrorCode.INVALID_SUBSCRIPTIONS);
  const channels: string[] = [];
  for (let index = 0; index < count; index += 1) {
    const channel = decoder.readText(MAX_CHANNEL_UTF8_BYTES, ErrorCode.INVALID_CHANNEL);
    validateChannel(channel, true);
    channels.push(channel);
  }
  if (!decoder.atEnd()) {
    throw new SSPError(ErrorCode.INVALID_WIRE, "SubscriptionsResponse document 存在尾随字节");
  }
  validateCanonicalChannels(channels);
  const canonical = encodeSubscriptionsResponseDocument(channels);
  if (!equalBytes(canonical, document)) {
    throw new SSPError(ErrorCode.NON_CANONICAL_CBOR, "SubscriptionsResponse document 不是 deterministic CBOR");
  }
  return channels.slice();
}

function encodeSingleChannelDocument(channel: string): Uint8Array {
  validateChannel(channel, true);
  const channelBytes = new TextEncoder().encode(channel);
  const result = concatBytes([encodeHead(4, 1), encodeHead(3, channelBytes.byteLength), channelBytes]);
  if (result.byteLength > MAX_DOCUMENT_BYTES) {
    throw new SSPError(ErrorCode.MESSAGE_TOO_LARGE, "channel document 超过长度限制");
  }
  return result;
}

function decodeSingleChannelDocument(document: Uint8Array): string {
  ensureDocumentSize(document);
  const decoder = new Decoder(document);
  decoder.readArrayExact(1);
  const channel = decoder.readText(MAX_CHANNEL_UTF8_BYTES, ErrorCode.INVALID_CHANNEL);
  validateChannel(channel, true);
  if (!decoder.atEnd()) {
    throw new SSPError(ErrorCode.INVALID_WIRE, "channel document 存在尾随字节");
  }
  const canonical = encodeSingleChannelDocument(channel);
  if (!equalBytes(canonical, document)) {
    throw new SSPError(ErrorCode.NON_CANONICAL_CBOR, "channel document 不是 deterministic CBOR");
  }
  return channel;
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
