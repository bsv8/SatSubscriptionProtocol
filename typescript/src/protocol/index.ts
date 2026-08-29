/** SSP V1 的公共类型、协议常量和稳定错误分类。 */

/** SSP V1 的固定 Wire 版本。 */
export const WIRE_VERSION = 1 as const;
/** SSP V1 的固定 Wire 版本中文别名导出。 */
export const WireVersion = WIRE_VERSION;
/** 与 Go SDK 对齐的发布版本，从 0.1.0 开始。 */
export const SDK_VERSION = "0.1.0" as const;
/** SDKVersion 的跨语言命名别名。 */
export const SDKVersion = SDK_VERSION;

/** SSP Wire 的五种固定消息类型。 */
export const Kind = {
  /** Publish（发布/广播）。 */
  Publish: 1,
  /** Subscribe（订阅）。 */
  Subscribe: 2,
  /** Unsubscribe（取消订阅）。 */
  Unsubscribe: 3,
  /** SubscriptionsRequest（查询订阅列表）。 */
  SubscriptionsRequest: 4,
  /** SubscriptionsResponse（返回订阅列表）。 */
  SubscriptionsResponse: 5,
} as const;

/** SSP Wire Kind 数值类型。 */
export type WireKind = (typeof Kind)[keyof typeof Kind];

/** 固定 Kind 的大写常量别名，数值与 Kind 对象完全相同。 */
export const KIND_PUBLISH = Kind.Publish;
/** Subscribe Kind 的大写常量别名。 */
export const KIND_SUBSCRIBE = Kind.Subscribe;
/** Unsubscribe Kind 的大写常量别名。 */
export const KIND_UNSUBSCRIBE = Kind.Unsubscribe;
/** SubscriptionsRequest Kind 的大写常量别名。 */
export const KIND_SUBSCRIPTIONS_REQUEST = Kind.SubscriptionsRequest;
/** SubscriptionsResponse Kind 的大写常量别名。 */
export const KIND_SUBSCRIPTIONS_RESPONSE = Kind.SubscriptionsResponse;

/** 完整 Wire 最大字节数（1 MiB）。 */
export const MAX_WIRE_BYTES = 1_048_576;
/** 外层 document_cbor 最大字节数。 */
export const MAX_DOCUMENT_BYTES = 1_048_512;
/** channel UTF-8 编码最大字节数。 */
export const MAX_CHANNEL_UTF8_BYTES = 256;
/** content_json 原始字节最大长度。 */
export const MAX_CONTENT_JSON_BYTES = 1_048_000;
/** 一个身份的订阅集合最大元素数。 */
export const MAX_SUBSCRIPTIONS = 1_024;

/** MaxWireBytes 的跨语言命名别名。 */
export const MaxWireBytes = MAX_WIRE_BYTES;
/** MaxDocumentBytes 的跨语言命名别名。 */
export const MaxDocumentBytes = MAX_DOCUMENT_BYTES;
/** MaxChannelUTF8Bytes 的跨语言命名别名。 */
export const MaxChannelUTF8Bytes = MAX_CHANNEL_UTF8_BYTES;
/** MaxContentJSONBytes 的跨语言命名别名。 */
export const MaxContentJSONBytes = MAX_CONTENT_JSON_BYTES;
/** MaxSubscriptions 的跨语言命名别名。 */
export const MaxSubscriptions = MAX_SUBSCRIPTIONS;

/** 跨语言稳定的 SDK 本地错误码。 */
export const ErrorCode = {
  /** Wire 或固定数组结构无效。 */
  INVALID_WIRE: "INVALID_WIRE",
  /** Wire 版本不受支持。 */
  UNSUPPORTED_VERSION: "UNSUPPORTED_VERSION",
  /** Wire Kind 不受支持。 */
  UNSUPPORTED_KIND: "UNSUPPORTED_KIND",
  /** CBOR 不是 deterministic 唯一编码。 */
  NON_CANONICAL_CBOR: "NON_CANONICAL_CBOR",
  /** 消息或字段超过冻结上限。 */
  MESSAGE_TOO_LARGE: "MESSAGE_TOO_LARGE",
  /** channel 无效或使用位置不正确。 */
  INVALID_CHANNEL: "INVALID_CHANNEL",
  /** content_json 不是完整合法 JSON。 */
  INVALID_JSON: "INVALID_JSON",
  /** 订阅集合重复、乱序、混合通配符或超限。 */
  INVALID_SUBSCRIPTIONS: "INVALID_SUBSCRIPTIONS",
  /** 外部认证公钥不是 33 字节压缩公钥。 */
  INVALID_PUBLIC_KEY: "INVALID_PUBLIC_KEY",
  /** 订阅 Backend 失败。 */
  BACKEND_FAILURE: "BACKEND_FAILURE",
  /** 广播 Dispatcher 失败。 */
  DISPATCH_FAILURE: "DISPATCH_FAILURE",
} as const;

/** 稳定 SDK 错误码类型。 */
export type ErrorCode = (typeof ErrorCode)[keyof typeof ErrorCode];

/** 带稳定 code 的 SDK 本地错误；message 不包含 content_json 原文。 */
export class SSPError extends Error {
  /** 稳定错误分类，Go 端可用 errors.Is 对应。 */
  readonly code: ErrorCode;

  /** 创建一个带中文上下文的稳定 SDK 错误。 */
  constructor(code: ErrorCode, message: string = code) {
    super(message);
    this.name = "SSPError";
    this.code = code;
    Object.setPrototypeOf(this, new.target.prototype);
  }
}

/** 从未知异常中提取 SDK 稳定错误码。 */
export function errorCode(error: unknown): ErrorCode | undefined {
  if (error instanceof SSPError) {
    return error.code;
  }
  return undefined;
}

/** PublicKey 是外层认证提供的 33 字节公钥字节，不进入 SSP Wire。 */
export type PublicKey = Uint8Array;

/** Publish 文档；contentJson 始终是原始 JSON UTF-8 字节。 */
export interface Publish {
  /** 精确匹配的普通 channel 名称。 */
  readonly channel: string;
  /** 一份完整 JSON 文本的原始字节，不会被重新序列化。 */
  readonly contentJson: Uint8Array;
}

/** 复制 Uint8Array，避免 SDK 输出与调用者输入共享可变内存。 */
export function cloneBytes(input: Uint8Array): Uint8Array {
  return new Uint8Array(input);
}

/**
 * 校验并复制外层认证得到的 33 字节公钥字节形状。
 * SDK 只检查长度和 02/03 压缩前缀，不验证 secp256k1 曲线点；真正的公钥认证由外层完成。
 */
export function validatePublicKey(input: Uint8Array): Uint8Array {
  if (!(input instanceof Uint8Array) || input.byteLength !== 33 || (input[0] !== 0x02 && input[0] !== 0x03)) {
    throw new SSPError(ErrorCode.INVALID_PUBLIC_KEY, "压缩公钥必须是 33 字节并以 02 或 03 开头");
  }
  return cloneBytes(input);
}

/** 将字符串编码为 UTF-8；孤立 surrogate 不属于合法 UTF-8 文本。 */
export function utf8Bytes(value: string): Uint8Array {
  utf8ByteLength(value);
  return new TextEncoder().encode(value);
}

/** 计算 UTF-8 字节长度并拒绝孤立 surrogate，不分配编码结果。 */
export function utf8ByteLength(value: string): number {
  let length = 0;
  for (let index = 0; index < value.length; index += 1) {
    const code = value.charCodeAt(index);
    if (code >= 0xd800 && code <= 0xdbff) {
      const next = value.charCodeAt(index + 1);
      if (next < 0xdc00 || next > 0xdfff) {
        throw new SSPError(ErrorCode.INVALID_CHANNEL, "channel 含有孤立 surrogate");
      }
      length += 4;
      index += 1;
    } else if (code >= 0xdc00 && code <= 0xdfff) {
      throw new SSPError(ErrorCode.INVALID_CHANNEL, "channel 含有孤立 surrogate");
    } else if (code <= 0x7f) {
      length += 1;
    } else if (code <= 0x7ff) {
      length += 2;
    } else {
      length += 3;
    }
  }
  return length;
}

/** 校验 channel 的非空、UTF-8 长度和 wildcard 使用位置。 */
export function validateChannel(channel: string, allowWildcard: boolean): void {
  if (typeof channel !== "string" || channel.length === 0) {
    throw new SSPError(ErrorCode.INVALID_CHANNEL, "channel 不能为空");
  }
  if (utf8ByteLength(channel) > MAX_CHANNEL_UTF8_BYTES) {
    throw new SSPError(ErrorCode.INVALID_CHANNEL, "channel 超过 UTF-8 长度限制");
  }
  if (channel === "*" && !allowWildcard) {
    throw new SSPError(ErrorCode.INVALID_CHANNEL, "此报文位置不允许 wildcard");
  }
}

/** 校验一份完整 RFC 8259 JSON 文本，并保留调用者传入的原始字节。 */
export function validateJson(contentJson: Uint8Array): void {
  if (!(contentJson instanceof Uint8Array)) {
    throw new SSPError(ErrorCode.INVALID_JSON, "content_json 必须是 Uint8Array");
  }
  if (contentJson.byteLength > MAX_CONTENT_JSON_BYTES) {
    throw new SSPError(ErrorCode.MESSAGE_TOO_LARGE, "content_json 超过长度限制");
  }
  let text: string;
  try {
    text = new TextDecoder("utf-8", { fatal: true, ignoreBOM: true }).decode(contentJson);
  } catch {
    throw new SSPError(ErrorCode.INVALID_JSON, "content_json 不是合法 UTF-8");
  }
  try {
    JSON.parse(text);
  } catch {
    throw new SSPError(ErrorCode.INVALID_JSON, "content_json 不是完整合法 JSON");
  }
}

/** 按 UTF-8 字节比较两个 channel。 */
export function compareChannelsUtf8(left: string, right: string): number {
  const leftBytes = utf8Bytes(left);
  const rightBytes = utf8Bytes(right);
  const length = Math.min(leftBytes.length, rightBytes.length);
  for (let index = 0; index < length; index += 1) {
    if (leftBytes[index] !== rightBytes[index]) {
      return leftBytes[index] < rightBytes[index] ? -1 : 1;
    }
  }
  return Math.sign(leftBytes.length - rightBytes.length);
}

/** 判断是否为五种固定 SSP Kind 之一。 */
export function isKind(kind: number): kind is WireKind {
  return kind >= Kind.Publish && kind <= Kind.SubscriptionsResponse && Number.isInteger(kind);
}
