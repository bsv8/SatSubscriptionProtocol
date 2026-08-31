// Package client 提供 SSP 客户端报文构造和指定报文解析 API。
package client

import (
	"fmt"

	"github.com/bsv8/SatSubscriptionProtocol/protocol"
	"github.com/bsv8/SatSubscriptionProtocol/wire"
)

// NewRequestID 使用操作系统安全随机源生成一个新的 32 字节请求编号。
func NewRequestID() (protocol.RequestID, error) {
	return protocol.NewRequestID()
}

// NewPublish 创建 Publish Wire；requestID 必须由调用者管理，contentJSON 保持原始字节。
func NewPublish(requestID protocol.RequestID, channel string, contentJSON []byte) ([]byte, error) {
	publish := protocol.Publish{
		RequestID:   requestID,
		Channel:     channel,
		ContentJSON: protocol.CloneBytes(contentJSON),
	}
	document, err := wire.EncodePublishDocument(publish)
	if err != nil {
		return nil, err
	}
	return wire.EncodeWire(protocol.PublishKind, document)
}

// NewSubscribe 创建 Subscribe Wire，可订阅普通 channel 或精确名称 "*"。
func NewSubscribe(requestID protocol.RequestID, channel string) ([]byte, error) {
	document, err := wire.EncodeSubscribeDocument(protocol.Subscribe{RequestID: requestID, Channel: channel})
	if err != nil {
		return nil, err
	}
	return wire.EncodeWire(protocol.SubscribeKind, document)
}

// NewUnsubscribe 创建 Unsubscribe Wire，可取消普通 channel 或精确名称 "*"。
func NewUnsubscribe(requestID protocol.RequestID, channel string) ([]byte, error) {
	document, err := wire.EncodeUnsubscribeDocument(protocol.Unsubscribe{RequestID: requestID, Channel: channel})
	if err != nil {
		return nil, err
	}
	return wire.EncodeWire(protocol.UnsubscribeKind, document)
}

// NewSubscriptionsRequest 创建查询订阅集合的 SubscriptionsRequest Wire。
func NewSubscriptionsRequest(requestID protocol.RequestID) ([]byte, error) {
	document := wire.EncodeSubscriptionsRequestDocument(protocol.SubscriptionsRequest{RequestID: requestID})
	return wire.EncodeWire(protocol.SubscriptionsRequestKind, document)
}

// ParsePublish 严格解析 Publish Wire，返回 request_id、channel 和原始 content_json 字节。
func ParsePublish(wireCBOR []byte) (protocol.Publish, error) {
	message, err := wire.Parse(wireCBOR)
	if err != nil {
		return protocol.Publish{}, err
	}
	if message.Kind != protocol.PublishKind {
		return protocol.Publish{}, fmt.Errorf("%w: expected Publish Wire", protocol.ErrInvalidWire)
	}
	return wire.DecodePublishDocument(message.Document)
}

// ParseActionResult 严格解析 ActionResult Wire。
func ParseActionResult(wireCBOR []byte) (protocol.ActionResult, error) {
	message, err := wire.Parse(wireCBOR)
	if err != nil {
		return protocol.ActionResult{}, err
	}
	if message.Kind != protocol.ActionResultKind {
		return protocol.ActionResult{}, fmt.Errorf("%w: expected ActionResult Wire", protocol.ErrInvalidWire)
	}
	return wire.DecodeActionResultDocument(message.Document)
}

// ParseSubscriptionsResponse 严格解析 SubscriptionsResponse Wire。
func ParseSubscriptionsResponse(wireCBOR []byte) (protocol.SubscriptionsResponse, error) {
	message, err := wire.Parse(wireCBOR)
	if err != nil {
		return protocol.SubscriptionsResponse{}, err
	}
	if message.Kind != protocol.SubscriptionsResponseKind {
		return protocol.SubscriptionsResponse{}, fmt.Errorf("%w: expected SubscriptionsResponse Wire", protocol.ErrInvalidWire)
	}
	return wire.DecodeSubscriptionsResponseDocument(message.Document)
}
