// Package subscription 实现 SSP 的订阅集合状态机、规范排序和内存 Backend。
package subscription

import (
	"context"
	"fmt"
	"sync"

	"github.com/bsv8/SatSubscriptionProtocol/protocol"
)

// NormalizeSubscriptions 校验并复制订阅集合，返回唯一的 UTF-8 字节升序表示。
// 输入非法时返回 INVALID_SUBSCRIPTIONS，不会静默修复输入。
func NormalizeSubscriptions(channels []string) ([]string, error) {
	if len(channels) > protocol.MaxSubscriptions {
		return nil, fmt.Errorf("%w: subscription count exceeds the configured limit", protocol.ErrInvalidSubscriptions)
	}
	if len(channels) == 0 {
		return []string{}, nil
	}

	result := make([]string, len(channels))
	copy(result, channels)
	for _, channel := range result {
		if err := protocol.ValidateChannel(channel, true); err != nil {
			return nil, fmt.Errorf("%w: invalid channel in subscription set", protocol.ErrInvalidSubscriptions)
		}
	}
	result = protocol.SortChannelsUTF8(result)
	for index := 1; index < len(result); index++ {
		if result[index-1] == result[index] {
			return nil, fmt.Errorf("%w: duplicate channel in subscription set", protocol.ErrInvalidSubscriptions)
		}
	}
	for index, channel := range result {
		if channel == "*" && len(result) != 1 {
			return nil, fmt.Errorf("%w: wildcard cannot be mixed with ordinary channels", protocol.ErrInvalidSubscriptions)
		}
		if index > 0 && result[index-1] == "*" {
			return nil, fmt.Errorf("%w: wildcard must be the only subscription", protocol.ErrInvalidSubscriptions)
		}
	}
	return result, nil
}

// Subscribe 对订阅集合执行一次原子语义上的 Subscribe 状态转换。
func Subscribe(current []string, channel string) ([]string, error) {
	currentNormalized, err := NormalizeSubscriptions(current)
	if err != nil {
		return nil, err
	}
	if err := protocol.ValidateChannel(channel, true); err != nil {
		return nil, err
	}
	if channel == "*" || (len(currentNormalized) == 1 && currentNormalized[0] == "*") {
		return []string{"*"}, nil
	}
	for _, subscribedChannel := range currentNormalized {
		if subscribedChannel == channel {
			return append([]string(nil), currentNormalized...), nil
		}
	}
	next := make([]string, 0, len(currentNormalized)+1)
	next = append(next, currentNormalized...)
	next = append(next, channel)
	return NormalizeSubscriptions(next)
}

// Unsubscribe 对订阅集合执行一次原子语义上的 Unsubscribe 状态转换。
func Unsubscribe(current []string, channel string) ([]string, error) {
	currentNormalized, err := NormalizeSubscriptions(current)
	if err != nil {
		return nil, err
	}
	if err := protocol.ValidateChannel(channel, true); err != nil {
		return nil, err
	}
	if channel == "*" {
		return []string{}, nil
	}
	if len(currentNormalized) == 1 && currentNormalized[0] == "*" {
		return []string{"*"}, nil
	}
	next := make([]string, 0, len(currentNormalized))
	for _, subscribedChannel := range currentNormalized {
		if subscribedChannel != channel {
			next = append(next, subscribedChannel)
		}
	}
	return NormalizeSubscriptions(next)
}

// Matches 判断订阅集合是否匹配一个普通 Publish channel。
// 非法订阅集合或非法 Publish channel 会返回对应稳定错误，而不是被自动修复。
func Matches(channels []string, publishChannel string) (bool, error) {
	normalized, err := NormalizeSubscriptions(channels)
	if err != nil {
		return false, err
	}
	if err := protocol.ValidateChannel(publishChannel, false); err != nil {
		return false, err
	}
	if len(normalized) == 1 && normalized[0] == "*" {
		return true, nil
	}
	for _, channel := range normalized {
		if channel == publishChannel {
			return true, nil
		}
	}
	return false, nil
}

// Backend 是按已认证 remote_public_key 持久化订阅集合的接口。
type Backend interface {
	// Subscribe 在同一事务边界中检查、扣费并写入订阅状态，返回实际扣费金额。
	Subscribe(ctx context.Context, remotePublicKey protocol.PublicKey, requestID protocol.RequestID, channel string) (protocol.Amount, error)
	// Unsubscribe 在同一事务边界中检查、扣费并写入订阅状态，返回实际扣费金额。
	Unsubscribe(ctx context.Context, remotePublicKey protocol.PublicKey, requestID protocol.RequestID, channel string) (protocol.Amount, error)
	// Subscriptions 在同一事务边界中查询并扣费，返回该事务读取的列表和实际扣费金额。
	Subscriptions(ctx context.Context, remotePublicKey protocol.PublicKey, requestID protocol.RequestID) ([]string, protocol.Amount, error)
}

// MemoryBackend 是线程安全的进程内 Backend，仅用于测试和示例。
// 进程退出或实例重启后，所有状态都会丢失，不适合作为生产持久化实现。
type MemoryBackend struct {
	mu            sync.RWMutex
	subscriptions map[protocol.PublicKey][]string
}

// NewMemoryBackend 创建一个空的线程安全内存 Backend。
func NewMemoryBackend() *MemoryBackend {
	return &MemoryBackend{subscriptions: make(map[protocol.PublicKey][]string)}
}

// Subscribe 原子、幂等地更新内存中的订阅集合。
func (backend *MemoryBackend) Subscribe(_ context.Context, remotePublicKey protocol.PublicKey, _ protocol.RequestID, channel string) (protocol.Amount, error) {
	if err := validateBackendKey(remotePublicKey); err != nil {
		return "", err
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if backend.subscriptions == nil {
		backend.subscriptions = make(map[protocol.PublicKey][]string)
	}
	current := backend.subscriptions[remotePublicKey]
	next, err := Subscribe(current, channel)
	if err != nil {
		return "", err
	}
	backend.subscriptions[remotePublicKey] = next
	return protocol.ZeroAmount, nil
}

// Unsubscribe 原子、幂等地更新内存中的订阅集合。
func (backend *MemoryBackend) Unsubscribe(_ context.Context, remotePublicKey protocol.PublicKey, _ protocol.RequestID, channel string) (protocol.Amount, error) {
	if err := validateBackendKey(remotePublicKey); err != nil {
		return "", err
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if backend.subscriptions == nil {
		backend.subscriptions = make(map[protocol.PublicKey][]string)
	}
	current := backend.subscriptions[remotePublicKey]
	next, err := Unsubscribe(current, channel)
	if err != nil {
		return "", err
	}
	backend.subscriptions[remotePublicKey] = next
	return protocol.ZeroAmount, nil
}

// Subscriptions 返回内存 Backend 中该身份的规范订阅集合副本。
func (backend *MemoryBackend) Subscriptions(_ context.Context, remotePublicKey protocol.PublicKey, _ protocol.RequestID) ([]string, protocol.Amount, error) {
	if err := validateBackendKey(remotePublicKey); err != nil {
		return nil, "", err
	}
	backend.mu.RLock()
	defer backend.mu.RUnlock()
	current, ok := backend.subscriptions[remotePublicKey]
	if !ok {
		return []string{}, protocol.ZeroAmount, nil
	}
	result, err := NormalizeSubscriptions(current)
	if err != nil {
		return nil, "", err
	}
	return result, protocol.ZeroAmount, nil
}

func validateBackendKey(remotePublicKey protocol.PublicKey) error {
	if !remotePublicKey.Valid() {
		return fmt.Errorf("%w: backend key is not a compressed public key", protocol.ErrInvalidPublicKey)
	}
	return nil
}
