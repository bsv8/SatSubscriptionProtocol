// Package service 将严格 Wire 解析、订阅 Backend 和广播 Dispatcher 组合成无网络的服务入口。
package service

import (
	"context"
	"fmt"
	"sync"

	"github.com/bsv8/SatSubscriptionProtocol/protocol"
	"github.com/bsv8/SatSubscriptionProtocol/subscription"
	"github.com/bsv8/SatSubscriptionProtocol/wire"
)

// PublishEvent 是交给外部 Dispatcher 的完整 Publish 事件。
type PublishEvent struct {
	// PublisherPublicKey 是外层认证得到的发布者公钥，不来自 SSP payload。
	PublisherPublicKey protocol.PublicKey
	// Channel 是 Publish 的普通 channel。
	Channel string
	// ContentJSON 是 Publish 的 JSON exact bytes 防御性副本。
	ContentJSON []byte
	// Wire 是收到的完整 Publish Wire exact bytes 防御性副本。
	Wire []byte
}

// Dispatcher 是窄广播回调接口，不包含任何网络或连接类型。
type Dispatcher interface {
	// Dispatch 接收一个已认证发布者和完整 Publish 事件。
	Dispatch(ctx context.Context, event PublishEvent) error
}

// HandleMessage 处理一个来自已认证长期连接的完整 SSP Wire。
// Subscribe、Unsubscribe、Publish 成功时返回 nil、false、nil；
// SubscriptionsRequest 成功时返回需要写回同一连接的 Response Wire。
func HandleMessage(
	ctx context.Context,
	remotePublicKey protocol.PublicKey,
	wireCBOR []byte,
	backend subscription.Backend,
	dispatcher Dispatcher,
) (responseWireCBOR []byte, hasResponse bool, err error) {
	if !remotePublicKey.Valid() {
		return nil, false, fmt.Errorf("%w: remote public key is not a compressed key", protocol.ErrInvalidPublicKey)
	}
	message, err := wire.Parse(wireCBOR)
	if err != nil {
		return nil, false, err
	}

	switch message.Kind {
	case protocol.SubscribeKind:
		if backend == nil {
			return nil, false, fmt.Errorf("%w: subscription backend is required", protocol.ErrBackendFailure)
		}
		channel, decodeErr := wire.DecodeSubscribeDocument(message.Document)
		if decodeErr != nil {
			return nil, false, decodeErr
		}
		if backendErr := backend.Subscribe(ctx, remotePublicKey, channel); backendErr != nil {
			return nil, false, fmt.Errorf("%w: backend Subscribe failed", protocol.ErrBackendFailure)
		}
		return nil, false, nil

	case protocol.UnsubscribeKind:
		if backend == nil {
			return nil, false, fmt.Errorf("%w: subscription backend is required", protocol.ErrBackendFailure)
		}
		channel, decodeErr := wire.DecodeUnsubscribeDocument(message.Document)
		if decodeErr != nil {
			return nil, false, decodeErr
		}
		if backendErr := backend.Unsubscribe(ctx, remotePublicKey, channel); backendErr != nil {
			return nil, false, fmt.Errorf("%w: backend Unsubscribe failed", protocol.ErrBackendFailure)
		}
		return nil, false, nil

	case protocol.SubscriptionsRequestKind:
		if backend == nil {
			return nil, false, fmt.Errorf("%w: subscription backend is required", protocol.ErrBackendFailure)
		}
		if decodeErr := wire.DecodeSubscriptionsRequestDocument(message.Document); decodeErr != nil {
			return nil, false, decodeErr
		}
		channels, backendErr := backend.Subscriptions(ctx, remotePublicKey)
		if backendErr != nil {
			return nil, false, fmt.Errorf("%w: backend Subscriptions failed", protocol.ErrBackendFailure)
		}
		normalized, normalizeErr := subscription.NormalizeSubscriptions(channels)
		if normalizeErr != nil {
			return nil, false, normalizeErr
		}
		document, encodeErr := wire.EncodeSubscriptionsResponseDocument(normalized)
		if encodeErr != nil {
			return nil, false, encodeErr
		}
		response, encodeErr := wire.EncodeWire(protocol.SubscriptionsResponseKind, document)
		if encodeErr != nil {
			return nil, false, encodeErr
		}
		return protocol.CloneBytes(response), true, nil

	case protocol.PublishKind:
		if dispatcher == nil {
			return nil, false, fmt.Errorf("%w: publish dispatcher is required", protocol.ErrDispatchFailure)
		}
		publish, decodeErr := wire.DecodePublishDocument(message.Document)
		if decodeErr != nil {
			return nil, false, decodeErr
		}
		event := PublishEvent{
			PublisherPublicKey: remotePublicKey,
			Channel:            publish.Channel,
			ContentJSON:        protocol.CloneBytes(publish.ContentJSON),
			Wire:               protocol.CloneBytes(wireCBOR),
		}
		if dispatchErr := dispatcher.Dispatch(ctx, event); dispatchErr != nil {
			return nil, false, fmt.Errorf("%w: dispatcher rejected publish", protocol.ErrDispatchFailure)
		}
		return nil, false, nil

	case protocol.SubscriptionsResponseKind:
		return nil, false, fmt.Errorf("%w: SubscriptionsResponse is not a client command", protocol.ErrInvalidWire)
	default:
		return nil, false, fmt.Errorf("%w: unsupported message kind", protocol.ErrUnsupportedKind)
	}
}

// RecordingDispatcher 是只记录事件的线程安全 Dispatcher，仅用于测试和示例。
type RecordingDispatcher struct {
	mu     sync.Mutex
	events []PublishEvent
}

// NewRecordingDispatcher 创建空的 RecordingDispatcher。
func NewRecordingDispatcher() *RecordingDispatcher {
	return &RecordingDispatcher{}
}

// Dispatch 记录事件并复制所有可变字节切片。
func (dispatcher *RecordingDispatcher) Dispatch(_ context.Context, event PublishEvent) error {
	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	event.ContentJSON = protocol.CloneBytes(event.ContentJSON)
	event.Wire = protocol.CloneBytes(event.Wire)
	dispatcher.events = append(dispatcher.events, event)
	return nil
}

// Events 返回已记录事件的深度副本。
func (dispatcher *RecordingDispatcher) Events() []PublishEvent {
	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	result := make([]PublishEvent, len(dispatcher.events))
	for index, event := range dispatcher.events {
		result[index] = event
		result[index].ContentJSON = protocol.CloneBytes(event.ContentJSON)
		result[index].Wire = protocol.CloneBytes(event.Wire)
	}
	return result
}

// Reset 清空已记录事件。
func (dispatcher *RecordingDispatcher) Reset() {
	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	dispatcher.events = nil
}
