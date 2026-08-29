/** SSP 订阅集合状态机、规范排序和进程内 Backend。 */
import {
  compareChannelsUtf8,
  ErrorCode,
  MAX_SUBSCRIPTIONS,
  PublicKey,
  SSPError,
  validateChannel,
  validatePublicKey,
} from "../protocol/index.js";

/** 按已认证 PublicKey 保存订阅状态的异步接口。 */
export interface SubscriptionBackend {
  /** 原子、幂等地添加一个 channel。 */
  subscribe(remotePublicKey: PublicKey, channel: string): void | Promise<void>;
  /** 原子、幂等地取消一个 channel。 */
  unsubscribe(remotePublicKey: PublicKey, channel: string): void | Promise<void>;
  /** 返回该身份订阅集合；SDK 会再次验证并复制列表。 */
  subscriptions(remotePublicKey: PublicKey): readonly string[] | Promise<readonly string[]>;
}

/** Backend 的语义化别名。 */
export type Backend = SubscriptionBackend;

/** 校验并复制订阅集合，返回唯一的 UTF-8 字节升序表示。 */
export function normalizeSubscriptions(channels: readonly string[]): string[] {
  if (!Array.isArray(channels) || channels.length > MAX_SUBSCRIPTIONS) {
    throw new SSPError(ErrorCode.INVALID_SUBSCRIPTIONS, "订阅数量超过限制或输入不是列表");
  }
  const result = Array.from(channels);
  for (const channel of result) {
    try {
      validateChannel(channel, true);
    } catch {
      throw new SSPError(ErrorCode.INVALID_SUBSCRIPTIONS, "订阅列表包含非法 channel");
    }
  }
  result.sort(compareChannelsUtf8);
  for (let index = 1; index < result.length; index += 1) {
    if (result[index - 1] === result[index]) {
      throw new SSPError(ErrorCode.INVALID_SUBSCRIPTIONS, "订阅列表包含重复 channel");
    }
  }
  if (result.includes("*") && result.length !== 1) {
    throw new SSPError(ErrorCode.INVALID_SUBSCRIPTIONS, "wildcard 不能和普通 channel 混合");
  }
  return result;
}

/** 对订阅集合执行 Subscribe 状态转换。 */
export function subscribe(current: readonly string[], channel: string): string[] {
  const normalized = normalizeSubscriptions(current);
  validateChannel(channel, true);
  if (channel === "*" || (normalized.length === 1 && normalized[0] === "*")) {
    return ["*"];
  }
  if (normalized.includes(channel)) {
    return normalized.slice();
  }
  return normalizeSubscriptions([...normalized, channel]);
}

/** 对订阅集合执行 Unsubscribe 状态转换。 */
export function unsubscribe(current: readonly string[], channel: string): string[] {
  const normalized = normalizeSubscriptions(current);
  validateChannel(channel, true);
  if (channel === "*") {
    return [];
  }
  if (normalized.length === 1 && normalized[0] === "*") {
    return ["*"];
  }
  return normalizeSubscriptions(normalized.filter((item) => item !== channel));
}

/** 判断普通 Publish channel 是否匹配订阅集合；非法输入会抛出稳定错误。 */
export function matches(channels: readonly string[], publishChannel: string): boolean {
  const normalized = normalizeSubscriptions(channels);
  validateChannel(publishChannel, false);
  if (normalized.length === 1 && normalized[0] === "*") {
    return true;
  }
  return normalized.includes(publishChannel);
}

/** 线程/事件循环安全的进程内 Backend，仅用于测试和示例。重启后状态丢失。 */
export class MemoryBackend implements SubscriptionBackend {
  private readonly states = new Map<string, string[]>();

  /** 原子、幂等地更新内存订阅集合。 */
  subscribe(remotePublicKey: PublicKey, channel: string): void {
    const key = keyString(remotePublicKey);
    const current = this.states.get(key) ?? [];
    this.states.set(key, subscribe(current, channel));
  }

  /** 原子、幂等地更新内存订阅集合。 */
  unsubscribe(remotePublicKey: PublicKey, channel: string): void {
    const key = keyString(remotePublicKey);
    const current = this.states.get(key) ?? [];
    this.states.set(key, unsubscribe(current, channel));
  }

  /** 返回该身份的规范订阅集合新副本。 */
  subscriptions(remotePublicKey: PublicKey): readonly string[] {
    const key = keyString(remotePublicKey);
    return normalizeSubscriptions(this.states.get(key) ?? []);
  }
}

function keyString(remotePublicKey: PublicKey): string {
  const key = validatePublicKey(remotePublicKey);
  let result = "";
  for (const byte of key) {
    result += byte.toString(16).padStart(2, "0");
  }
  return result;
}
