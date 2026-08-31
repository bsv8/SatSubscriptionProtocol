/** SSP V1 的公共类型、协议常量和稳定错误分类。 */

/** SSP V1 的固定 Wire 版本。 */
export const WIRE_VERSION = 1 as const;
/** SSP V1 的固定 Wire 版本中文别名导出。 */
export const WireVersion = WIRE_VERSION;
/** 与 Go SDK 对齐的发布版本，从 0.1.0 开始。 */
export const SDK_VERSION = "0.1.0" as const;
/** SDK_VERSION 的跨语言命名别名。 */
export const SDKVersion = SDK_VERSION;

/** SSP V1 的六种固定消息类型。 */
export const Kind = {
  /** Publish（发布请求或广播交付）。 */
  Publish: 1,
  /** Subscribe（订阅）。 */
  Subscribe: 2,
  /** Unsubscribe（取消订阅）。 */
  Unsubscribe: 3,
  /** SubscriptionsRequest（查询订阅列表）。 */
  SubscriptionsRequest: 4,
  /** SubscriptionsResponse（返回订阅列表和实际扣费）。 */
  SubscriptionsResponse: 5,
  /** ActionResult（返回动作成功/失败和实际扣费）。 */
  ActionResult: 6,
} as const;

/** SSP Wire Kind 数值类型。 */
export type WireKind = (typeof Kind)[keyof typeof Kind];

/** 固定 Kind 的大写常量别名。 */
export const KIND_PUBLISH = Kind.Publish;
/** Subscribe Kind 的大写常量别名。 */
export const KIND_SUBSCRIBE = Kind.Subscribe;
/** Unsubscribe Kind 的大写常量别名。 */
export const KIND_UNSUBSCRIBE = Kind.Unsubscribe;
/** SubscriptionsRequest Kind 的大写常量别名。 */
export const KIND_SUBSCRIPTIONS_REQUEST = Kind.SubscriptionsRequest;
/** SubscriptionsResponse Kind 的大写常量别名。 */
export const KIND_SUBSCRIPTIONS_RESPONSE = Kind.SubscriptionsResponse;
/** ActionResult Kind 的大写常量别名。 */
export const KIND_ACTION_RESULT = Kind.ActionResult;

/** request_id 的固定字节长度。 */
export const REQUEST_ID_BYTES = 32;
/** request_id 的跨语言命名别名。 */
export const RequestIDBytes = REQUEST_ID_BYTES;
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
/** charged_amount 的完整 ASCII 字节上限。 */
export const MAX_AMOUNT_BYTES = 64;
/** charged_amount 的最大小数位数。 */
export const MAX_AMOUNT_SCALE = 18;
/** 单个 Stream Session 可登记的最大 request_id 数量。 */
export const MAX_REQUEST_IDS_PER_SESSION = 65_536;

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
/** MaxAmountBytes 的跨语言命名别名。 */
export const MaxAmountBytes = MAX_AMOUNT_BYTES;
/** MaxAmountScale 的跨语言命名别名。 */
export const MaxAmountScale = MAX_AMOUNT_SCALE;
/** MaxRequestIDsPerSession 的跨语言命名别名。 */
export const MaxRequestIDsPerSession = MAX_REQUEST_IDS_PER_SESSION;

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
  /** Session 已达到 request_id 登记上限。 */
  SESSION_LIMIT: "SESSION_LIMIT",
  /** Session 已关闭。 */
  SESSION_CLOSED: "SESSION_CLOSED",
} as const;

/** 稳定 SDK 错误码类型。 */
export type ErrorCode = (typeof ErrorCode)[keyof typeof ErrorCode];

/** ActionResult 的三个固定线上错误码。 */
export const ActionErrorCode = {
  /** request_id 可识别但其他业务字段无效。 */
  INVALID_REQUEST: "INVALID_REQUEST",
  /** 余额、权限或服务策略拒绝。 */
  REJECTED: "REJECTED",
  /** Backend、Dispatcher 或服务端内部失败。 */
  INTERNAL_ERROR: "INTERNAL_ERROR",
} as const;

/** ActionResult 线上错误码类型。 */
export type ActionErrorCode = (typeof ActionErrorCode)[keyof typeof ActionErrorCode];

/** 带稳定 code 和原始 cause 的 SDK 本地错误。 */
export class SSPError extends Error {
  /** 稳定错误分类，Go 端可用 errors.Is 对应。 */
  readonly code: ErrorCode;

  /** 以中文上下文创建错误，并保留原始异常到标准 Error.cause。 */
  constructor(code: ErrorCode, options?: { cause?: unknown });
  /** 以指定消息和 cause 创建错误。 */
  constructor(code: ErrorCode, message?: string, options?: { cause?: unknown });
  constructor(
    code: ErrorCode,
    messageOrOptions: string | { cause?: unknown } = code,
    options: { cause?: unknown } = {},
  ) {
    const message = typeof messageOrOptions === "string" ? messageOrOptions : code;
    const errorOptions = typeof messageOrOptions === "string" ? options : messageOrOptions;
    super(message, errorOptions);
    this.name = "SSPError";
    this.code = code;
    Object.setPrototypeOf(this, new.target.prototype);
  }
}

/** 从未知异常中提取 SDK 稳定错误码。 */
export function errorCode(error: unknown): ErrorCode | undefined {
  return error instanceof SSPError ? error.code : undefined;
}

/** request_id 的运行时类型；所有公开 API 都会复制它。 */
export type RequestId = Uint8Array;
/** RequestId 的大写命名兼容别名。 */
export type RequestID = RequestId;

/** 使用 Web Crypto 安全随机源生成 request_id。 */
export function newRequestId(): RequestId {
  const cryptoObject = globalThis.crypto;
  if (!cryptoObject || typeof cryptoObject.getRandomValues !== "function") {
    throw new SSPError(ErrorCode.INVALID_WIRE, "当前运行环境没有可用的 Web Crypto 安全随机源");
  }
  return cloneBytes(cryptoObject.getRandomValues(new Uint8Array(REQUEST_ID_BYTES)));
}

/** 校验并复制固定 32 字节 request_id。 */
export function validateRequestId(input: RequestId): RequestId {
  if (!(input instanceof Uint8Array) || input.byteLength !== REQUEST_ID_BYTES) {
    throw new SSPError(ErrorCode.INVALID_WIRE, `request_id 必须是 ${REQUEST_ID_BYTES} 字节`);
  }
  return cloneBytes(input);
}

/** Publish 文档；contentJson 始终是原始 JSON UTF-8 字节。 */
export interface Publish {
  /** 同一 Stream 内关联请求和结果的编号。 */
  readonly requestId: RequestId;
  /** 精确匹配的普通 channel 名称。 */
  readonly channel: string;
  /** 一份完整 JSON 文本的原始字节，不会被重新序列化。 */
  readonly contentJson: Uint8Array;
}

/** Subscribe 文档。 */
export interface Subscribe {
  /** 同一 Stream 内关联请求和结果的编号。 */
  readonly requestId: RequestId;
  /** 要订阅的普通 channel 或全部 channel（"*"）。 */
  readonly channel: string;
}

/** Unsubscribe 文档。 */
export interface Unsubscribe {
  /** 同一 Stream 内关联请求和结果的编号。 */
  readonly requestId: RequestId;
  /** 要取消的普通 channel 或全部订阅（"*"）。 */
  readonly channel: string;
}

/** SubscriptionsRequest 文档。 */
export interface SubscriptionsRequest {
  /** 同一 Stream 内关联查询和响应的编号。 */
  readonly requestId: RequestId;
}

/** SubscriptionsResponse 文档。 */
export interface SubscriptionsResponse {
  /** 原样回显的查询请求编号。 */
  readonly requestId: RequestId;
  /** 本次实际扣费的规范十进制金额。 */
  readonly chargedAmount: Amount;
  /** 无重复、按 UTF-8 字节升序排列的订阅集合。 */
  readonly channels: readonly string[];
}

/** ActionResult 文档。 */
export interface ActionResult {
  /** 原样回显的请求编号。 */
  readonly requestId: RequestId;
  /** 动作是否成功。 */
  readonly success: boolean;
  /** 成功时为实际扣费；失败固定为 "0"。 */
  readonly chargedAmount: Amount;
  /** 成功为空；失败只能是三个固定线上错误码。 */
  readonly errorCode: "" | ActionErrorCode;
}

/** Amount 是规范化的精确十进制金额字符串，不使用 JavaScript number。 */
export type Amount = string;

/** 免费或失败结果的规范金额编码。 */
export const ZERO_AMOUNT: Amount = "0";
/** ZeroAmount 的跨语言命名别名。 */
export const ZeroAmount = ZERO_AMOUNT;

/** 校验 charged_amount 的精确十进制规范。 */
export function validateAmount(value: Amount): void {
  if (typeof value !== "string" || value.length === 0 || value.length > MAX_AMOUNT_BYTES) {
    throw new SSPError(ErrorCode.INVALID_WIRE, `charged_amount 必须是 1..${MAX_AMOUNT_BYTES} 字节 ASCII 字符串`);
  }
  for (let index = 0; index < value.length; index += 1) {
    if (value.charCodeAt(index) > 0x7f) {
      throw new SSPError(ErrorCode.INVALID_WIRE, "charged_amount 必须是 ASCII");
    }
  }
  let dot = -1;
  for (let index = 0; index < value.length; index += 1) {
    const character = value[index];
    if (character === ".") {
      if (dot >= 0) {
        throw new SSPError(ErrorCode.INVALID_WIRE, "charged_amount 不能包含多个小数点");
      }
      dot = index;
    } else if (character < "0" || character > "9") {
      throw new SSPError(ErrorCode.INVALID_WIRE, "charged_amount 含有禁止字符");
    }
  }
  const integerEnd = dot >= 0 ? dot : value.length;
  if (dot >= 0) {
    const fractionLength = value.length - dot - 1;
    if (fractionLength === 0 || fractionLength > MAX_AMOUNT_SCALE) {
      throw new SSPError(ErrorCode.INVALID_WIRE, `charged_amount 小数位必须为 1..${MAX_AMOUNT_SCALE}`);
    }
    if (value[value.length - 1] === "0") {
      throw new SSPError(ErrorCode.INVALID_WIRE, "charged_amount 不能有小数末尾零");
    }
  }
  if (integerEnd === 0) {
    throw new SSPError(ErrorCode.INVALID_WIRE, "charged_amount 小数点前必须有整数位");
  }
  if (value[0] === "0" && integerEnd !== 1) {
    throw new SSPError(ErrorCode.INVALID_WIRE, "charged_amount 不能有多余前导零");
  }
  for (let index = 0; index < integerEnd; index += 1) {
    if (value[index] < "0" || value[index] > "9") {
      throw new SSPError(ErrorCode.INVALID_WIRE, "charged_amount 整数部分无效");
    }
  }
}

/** 校验并返回规范金额字符串；不会转换成 number。 */
export function newAmount(value: string): Amount {
  validateAmount(value);
  return value;
}

/** 判断 ActionResult 线上错误码是否有效。 */
export function isActionErrorCode(value: string): value is ActionErrorCode {
  return value === ActionErrorCode.INVALID_REQUEST || value === ActionErrorCode.REJECTED || value === ActionErrorCode.INTERNAL_ERROR;
}

/** 校验 ActionResult 的字段组合。 */
export function validateActionResult(result: ActionResult): void {
  validateRequestId(result.requestId);
  validateAmount(result.chargedAmount);
  if (result.success) {
    if (result.errorCode !== "") {
      throw new SSPError(ErrorCode.INVALID_WIRE, "成功 ActionResult 的 error_code 必须为空字符串");
    }
    return;
  }
  if (result.chargedAmount !== ZERO_AMOUNT || !isActionErrorCode(result.errorCode)) {
    throw new SSPError(ErrorCode.INVALID_WIRE, "失败 ActionResult 必须使用 charged_amount=0 和固定错误码");
  }
}

/** PublicKey 是外层认证提供的 33 字节公钥字节，不进入 SSP Wire。 */
export type PublicKey = Uint8Array;

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

/** 判断是否为六种固定 SSP Kind 之一。 */
export function isKind(kind: number): kind is WireKind {
  return Number.isInteger(kind) && kind >= Kind.Publish && kind <= Kind.ActionResult;
}
