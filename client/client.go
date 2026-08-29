// Package client 提供 SSP 客户端报文构造和指定报文解析 API。
package client

import (
	"fmt"

	"github.com/bsv8/SatSubscriptionProtocol/protocol"
	"github.com/bsv8/SatSubscriptionProtocol/wire"
)

// NewPublish 创建 Publish Wire；contentJSON 会被 SDK 防御性复制并保持原始字节。
func NewPublish(channel string, contentJSON []byte) ([]byte, error) {
	publish := protocol.Publish{
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
func NewSubscribe(channel string) ([]byte, error) {
	document, err := wire.EncodeSubscribeDocument(channel)
	if err != nil {
		return nil, err
	}
	return wire.EncodeWire(protocol.SubscribeKind, document)
}

// NewUnsubscribe 创建 Unsubscribe Wire，可取消普通 channel 或精确名称 "*"。
func NewUnsubscribe(channel string) ([]byte, error) {
	document, err := wire.EncodeUnsubscribeDocument(channel)
	if err != nil {
		return nil, err
	}
	return wire.EncodeWire(protocol.UnsubscribeKind, document)
}

// NewSubscriptionsRequest 创建查询订阅集合的 Request Wire。
func NewSubscriptionsRequest() ([]byte, error) {
	document := wire.EncodeSubscriptionsRequestDocument()
	return wire.EncodeWire(protocol.SubscriptionsRequestKind, document)
}

// ParsePublish 严格解析 Publish Wire，返回 channel 和原始 content_json 字节。
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

// ParseSubscriptionsResponse 严格解析 SubscriptionsResponse Wire，并返回新列表。
func ParseSubscriptionsResponse(wireCBOR []byte) ([]string, error) {
	message, err := wire.Parse(wireCBOR)
	if err != nil {
		return nil, err
	}
	if message.Kind != protocol.SubscriptionsResponseKind {
		return nil, fmt.Errorf("%w: expected SubscriptionsResponse Wire", protocol.ErrInvalidWire)
	}
	channels, err := wire.DecodeSubscriptionsResponseDocument(message.Document)
	if err != nil {
		return nil, err
	}
	return append([]string(nil), channels...), nil
}
