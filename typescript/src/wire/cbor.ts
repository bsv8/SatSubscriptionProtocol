import { ErrorCode, SSPError } from "../protocol/index.js";

/** deterministic CBOR 编码器使用的无符号头部。 */
export function encodeHead(major: number, value: number): Uint8Array {
  if (!Number.isSafeInteger(value) || value < 0) {
    throw new SSPError(ErrorCode.INVALID_WIRE, "CBOR 长度不是安全的非负整数");
  }
  const prefix = major << 5;
  if (value < 24) {
    return new Uint8Array([prefix | value]);
  }
  if (value <= 0xff) {
    return new Uint8Array([prefix | 24, value]);
  }
  if (value <= 0xffff) {
    return new Uint8Array([prefix | 25, value >>> 8, value & 0xff]);
  }
  if (value <= 0xffffffff) {
    return new Uint8Array([prefix | 26, value >>> 24, value >>> 16, value >>> 8, value & 0xff]);
  }
  const result = new Uint8Array(9);
  result[0] = prefix | 27;
  let remaining = BigInt(value);
  for (let index = 8; index >= 1; index -= 1) {
    result[index] = Number(remaining & 0xffn);
    remaining >>= 8n;
  }
  return result;
}

/** 拼接若干 Uint8Array，并返回独立结果。 */
export function concatBytes(parts: readonly Uint8Array[]): Uint8Array {
  const total = parts.reduce((sum, part) => sum + part.byteLength, 0);
  const result = new Uint8Array(total);
  let offset = 0;
  for (const part of parts) {
    result.set(part, offset);
    offset += part.byteLength;
  }
  return result;
}

/** 严格 deterministic CBOR 解码器，只提供 SSP 固定结构需要的读取操作。 */
export class Decoder {
  private readonly data: Uint8Array;
  private offset = 0;

  /** 创建输入副本，避免解析期间受调用者修改影响。 */
  constructor(input: Uint8Array) {
    this.data = new Uint8Array(input);
  }

  /** 判断是否已精确读完全部字节。 */
  atEnd(): boolean {
    return this.offset === this.data.byteLength;
  }

  /** 读取指定 CBOR major type 的 canonical 头部数值。 */
  readHead(expectedMajor: number): bigint {
    if (this.offset >= this.data.byteLength) {
      throw new SSPError(ErrorCode.INVALID_WIRE, "CBOR 数据提前结束");
    }
    const first = this.data[this.offset];
    this.offset += 1;
    const major = first >>> 5;
    const additional = first & 0x1f;
    if (major !== expectedMajor) {
      throw new SSPError(ErrorCode.INVALID_WIRE, "CBOR major type 不符合 SSP 结构");
    }
    if (additional === 31 || additional >= 28) {
      throw new SSPError(ErrorCode.INVALID_WIRE, "SSP 禁止 indefinite 或保留 CBOR 编码");
    }
    if (additional <= 23) {
      return BigInt(additional);
    }
    const width = additional === 24 ? 1 : additional === 25 ? 2 : additional === 26 ? 4 : 8;
    const value = this.readUnsigned(width);
    const minimum = width === 1 ? 24n : width === 2 ? 0x100n : width === 4 ? 0x10000n : 0x100000000n;
    if (value < minimum) {
      throw new SSPError(ErrorCode.NON_CANONICAL_CBOR, "CBOR 使用了非最短整数或长度编码");
    }
    return value;
  }

  /** 读取 CBOR uint。 */
  readUint(): bigint {
    return this.readHead(0);
  }

  /** 读取定长数组。 */
  readArrayExact(expected: number): void {
    if (this.readHead(4) !== BigInt(expected)) {
      throw new SSPError(ErrorCode.INVALID_WIRE, "固定数组长度不符合 SSP 结构");
    }
  }

  /** 读取受上限约束的数组长度。 */
  readArrayCount(maximum: number, overflowCode: ErrorCode): number {
    const count = this.readHead(4);
    if (count > BigInt(maximum)) {
      throw new SSPError(overflowCode, "CBOR 数组超过配置限制");
    }
    return Number(count);
  }

  /** 读取并复制 byte string。类型错误仍归类为 INVALID_WIRE。 */
  readBytes(maximum: number, tooLargeCode: ErrorCode): Uint8Array {
    const length = this.readHead(2);
    if (length > BigInt(maximum)) {
      throw new SSPError(tooLargeCode, "CBOR byte string 超过配置限制");
    }
    const size = Number(length);
    if (this.data.byteLength - this.offset < size) {
      throw new SSPError(ErrorCode.INVALID_WIRE, "CBOR byte string 不完整");
    }
    const result = this.data.slice(this.offset, this.offset + size);
    this.offset += size;
    return result;
  }

  /** 读取固定长度 byte string，长度错误统一归类为 INVALID_WIRE。 */
  readFixedBytes(expected: number): Uint8Array {
    const length = this.readHead(2);
    if (length !== BigInt(expected)) {
      throw new SSPError(ErrorCode.INVALID_WIRE, `CBOR byte string 必须是 ${expected} 字节`);
    }
    const size = expected;
    if (this.data.byteLength - this.offset < size) {
      throw new SSPError(ErrorCode.INVALID_WIRE, "CBOR 固定 byte string 不完整");
    }
    const result = this.data.slice(this.offset, this.offset + size);
    this.offset += size;
    return result;
  }

  /** 读取并验证 UTF-8 text string。 */
  readText(maximum: number, errorCode: ErrorCode): string {
    return this.readTextInternal(maximum, errorCode, false);
  }

  /** 读取允许空字符串的 UTF-8 text string。 */
  readTextAllowEmpty(maximum: number, errorCode: ErrorCode): string {
    return this.readTextInternal(maximum, errorCode, true);
  }

  private readTextInternal(maximum: number, errorCode: ErrorCode, allowEmpty: boolean): string {
    const length = this.readHead(3);
    if ((!allowEmpty && length === 0n) || length > BigInt(maximum)) {
      throw new SSPError(errorCode, "CBOR text string 为空或超过长度限制");
    }
    const size = Number(length);
    if (this.data.byteLength - this.offset < size) {
      throw new SSPError(ErrorCode.INVALID_WIRE, "CBOR text string 不完整");
    }
    const bytes = this.data.slice(this.offset, this.offset + size);
    this.offset += size;
    try {
      return new TextDecoder("utf-8", { fatal: true, ignoreBOM: true }).decode(bytes);
    } catch {
      throw new SSPError(errorCode, "CBOR text string 不是合法 UTF-8");
    }
  }

  /** 读取 canonical CBOR bool；只接受 false(0xf4) 和 true(0xf5)。 */
  readBool(): boolean {
    if (this.offset >= this.data.byteLength) {
      throw new SSPError(ErrorCode.INVALID_WIRE, "CBOR bool 数据提前结束");
    }
    const value = this.data[this.offset];
    if (value === 0xf4) {
      this.offset += 1;
      return false;
    }
    if (value === 0xf5) {
      this.offset += 1;
      return true;
    }
    throw new SSPError(ErrorCode.INVALID_WIRE, "ActionResult success 必须是 CBOR bool");
  }

  private readUnsigned(width: number): bigint {
    if (this.data.byteLength - this.offset < width) {
      throw new SSPError(ErrorCode.INVALID_WIRE, "CBOR 数值不完整");
    }
    let value = 0n;
    for (let index = 0; index < width; index += 1) {
      value = (value << 8n) | BigInt(this.data[this.offset]);
      this.offset += 1;
    }
    return value;
  }
}

/** 比较两个字节数组是否逐字节相同。 */
export function equalBytes(left: Uint8Array, right: Uint8Array): boolean {
  if (left.byteLength !== right.byteLength) {
    return false;
  }
  for (let index = 0; index < left.byteLength; index += 1) {
    if (left[index] !== right[index]) {
      return false;
    }
  }
  return true;
}
