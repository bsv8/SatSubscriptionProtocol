// Package wire 实现 SSP V1 的 deterministic CBOR 编解码和严格解析。
package wire

import (
	"bytes"
	"fmt"
	"unicode/utf8"

	"github.com/bsv8/SatSubscriptionProtocol/protocol"
)

// Message 是严格解析后的 SSP 外层 Wire。
type Message struct {
	// Version 是 Wire 版本，V1 固定为 1。
	Version protocol.Version
	// Kind 是 SSP V1 六种固定消息类型之一。
	Kind protocol.Kind
	// Document 是内层 document_cbor 的 exact bytes 防御性副本。
	Document []byte
}

// RequestEnvelope 是已经完成外层解析并读取 request_id 的请求。
// 其余业务字段由 DecodeRequest 继续校验；这样服务端可以在业务字段无效时
// 使用同一个 request_id 返回 INVALID_REQUEST。
type RequestEnvelope struct {
	// Message 是已通过外层版本、Kind、document bstr 和 canonical 校验的 Wire。
	Message Message
	// RequestID 是从请求文档首字段读取的固定 32 字节编号。
	RequestID protocol.RequestID
}

// RequestDocument 是完成具体业务字段校验后的请求联合类型。
// 只有与 Kind 对应的字段具有意义。
type RequestDocument struct {
	// Kind 是请求的业务类型。
	Kind protocol.Kind
	// RequestID 是请求编号。
	RequestID protocol.RequestID
	// Publish 是 Publish 请求字段。
	Publish protocol.Publish
	// Subscribe 是 Subscribe 请求字段。
	Subscribe protocol.Subscribe
	// Unsubscribe 是 Unsubscribe 请求字段。
	Unsubscribe protocol.Unsubscribe
	// SubscriptionsRequest 是订阅列表查询请求字段。
	SubscriptionsRequest protocol.SubscriptionsRequest
}

// EncodeWire 将已验证的内层 document 编码为 [version, kind, document_cbor]。
func EncodeWire(kind protocol.Kind, document []byte) ([]byte, error) {
	if !protocol.IsKind(kind) {
		return nil, fmt.Errorf("%w: kind is not defined by SSP V1", protocol.ErrUnsupportedKind)
	}
	if len(document) > protocol.MaxDocumentBytes {
		return nil, fmt.Errorf("%w: document exceeds the configured limit", protocol.ErrMessageTooLarge)
	}
	if err := validateDocument(kind, document); err != nil {
		return nil, err
	}
	return encodeWireUnchecked(kind, document)
}

// Parse 严格解析完整 SSP Wire，并校验对应的内层文档。
func Parse(input []byte) (Message, error) {
	message, err := parseOuter(input)
	if err != nil {
		return Message{}, err
	}
	if err := validateDocument(message.Kind, message.Document); err != nil {
		return Message{}, err
	}
	return message, nil
}

// ParseRequestEnvelope 只解析外层和请求文档的 request_id。
// 它不校验 request_id 之后的业务字段，供 Session 在业务失败时生成 ActionResult。
func ParseRequestEnvelope(input []byte) (RequestEnvelope, error) {
	message, err := parseOuter(input)
	if err != nil {
		return RequestEnvelope{}, err
	}
	if !isRequestKind(message.Kind) {
		return RequestEnvelope{}, fmt.Errorf("%w: only client request kinds have request_id", protocol.ErrInvalidWire)
	}
	requestID, err := decodeRequestIDPrefix(message.Document)
	if err != nil {
		return RequestEnvelope{}, err
	}
	return RequestEnvelope{Message: message, RequestID: requestID}, nil
}

// DecodeRequest 校验 RequestEnvelope 中 request_id 之后的全部业务字段。
func DecodeRequest(envelope RequestEnvelope) (RequestDocument, error) {
	result := RequestDocument{Kind: envelope.Message.Kind, RequestID: envelope.RequestID}
	switch envelope.Message.Kind {
	case protocol.PublishKind:
		publish, err := DecodePublishDocument(envelope.Message.Document)
		if err != nil {
			return RequestDocument{}, err
		}
		if publish.RequestID != envelope.RequestID {
			return RequestDocument{}, fmt.Errorf("%w: request_id changed during decoding", protocol.ErrInvalidWire)
		}
		result.Publish = publish
	case protocol.SubscribeKind:
		subscribe, err := DecodeSubscribeDocument(envelope.Message.Document)
		if err != nil {
			return RequestDocument{}, err
		}
		if subscribe.RequestID != envelope.RequestID {
			return RequestDocument{}, fmt.Errorf("%w: request_id changed during decoding", protocol.ErrInvalidWire)
		}
		result.Subscribe = subscribe
	case protocol.UnsubscribeKind:
		unsubscribe, err := DecodeUnsubscribeDocument(envelope.Message.Document)
		if err != nil {
			return RequestDocument{}, err
		}
		if unsubscribe.RequestID != envelope.RequestID {
			return RequestDocument{}, fmt.Errorf("%w: request_id changed during decoding", protocol.ErrInvalidWire)
		}
		result.Unsubscribe = unsubscribe
	case protocol.SubscriptionsRequestKind:
		request, err := DecodeSubscriptionsRequestDocument(envelope.Message.Document)
		if err != nil {
			return RequestDocument{}, err
		}
		if request.RequestID != envelope.RequestID {
			return RequestDocument{}, fmt.Errorf("%w: request_id changed during decoding", protocol.ErrInvalidWire)
		}
		result.SubscriptionsRequest = request
	default:
		return RequestDocument{}, fmt.Errorf("%w: kind is not a client request", protocol.ErrInvalidWire)
	}
	return result, nil
}

// EncodePublishDocument 编码 Publish 内层文档 [request_id, channel, content_json]。
func EncodePublishDocument(publish protocol.Publish) ([]byte, error) {
	if err := protocol.ValidateChannel(publish.Channel, false); err != nil {
		return nil, err
	}
	if err := protocol.ValidateJSON(publish.ContentJSON); err != nil {
		return nil, err
	}
	channelBytes := []byte(publish.Channel)
	result := make([]byte, 0, len(channelBytes)+len(publish.ContentJSON)+protocol.RequestIDBytes+24)
	result = append(result, encodeHead(4, 3)...)
	result = append(result, encodeHead(2, protocol.RequestIDBytes)...)
	result = append(result, publish.RequestID[:]...)
	result = append(result, encodeHead(3, uint64(len(channelBytes)))...)
	result = append(result, channelBytes...)
	result = append(result, encodeHead(2, uint64(len(publish.ContentJSON)))...)
	result = append(result, publish.ContentJSON...)
	if len(result) > protocol.MaxDocumentBytes {
		return nil, fmt.Errorf("%w: document exceeds the configured limit", protocol.ErrMessageTooLarge)
	}
	return result, nil
}

// DecodePublishDocument 严格解析 Publish 内层文档，并复制 request_id/content_json。
func DecodePublishDocument(document []byte) (protocol.Publish, error) {
	var result protocol.Publish
	decoder := newDecoder(document)
	if err := decoder.readArrayExact(3); err != nil {
		return result, err
	}
	requestID, err := decoder.readFixedRequestID()
	if err != nil {
		return result, err
	}
	channel, err := decoder.readText(protocol.MaxChannelUTF8Bytes, protocol.ErrInvalidChannel)
	if err != nil {
		return result, err
	}
	if err := protocol.ValidateChannel(channel, false); err != nil {
		return result, err
	}
	contentJSON, err := decoder.readBytes(protocol.MaxContentJSONBytes, protocol.ErrMessageTooLarge)
	if err != nil {
		return result, err
	}
	if !decoder.atEnd() {
		return result, fmt.Errorf("%w: trailing bytes in publish document", protocol.ErrInvalidWire)
	}
	if err := protocol.ValidateJSON(contentJSON); err != nil {
		return result, err
	}
	candidate := protocol.Publish{RequestID: requestID, Channel: channel, ContentJSON: contentJSON}
	canonical, err := EncodePublishDocument(candidate)
	if err != nil {
		return result, err
	}
	if !bytes.Equal(canonical, document) {
		return result, fmt.Errorf("%w: publish document is not deterministic CBOR", protocol.ErrNonCanonicalCBOR)
	}
	result = candidate
	result.ContentJSON = protocol.CloneBytes(contentJSON)
	return result, nil
}

// EncodeSubscribeDocument 编码 Subscribe 内层文档 [request_id, channel]。
func EncodeSubscribeDocument(subscribe protocol.Subscribe) ([]byte, error) {
	return encodeChannelRequestDocument(subscribe.RequestID, subscribe.Channel)
}

// DecodeSubscribeDocument 严格解析 Subscribe 内层文档。
func DecodeSubscribeDocument(document []byte) (protocol.Subscribe, error) {
	requestID, channel, err := decodeChannelRequestDocument(document)
	return protocol.Subscribe{RequestID: requestID, Channel: channel}, err
}

// EncodeUnsubscribeDocument 编码 Unsubscribe 内层文档 [request_id, channel]。
func EncodeUnsubscribeDocument(unsubscribe protocol.Unsubscribe) ([]byte, error) {
	return encodeChannelRequestDocument(unsubscribe.RequestID, unsubscribe.Channel)
}

// DecodeUnsubscribeDocument 严格解析 Unsubscribe 内层文档。
func DecodeUnsubscribeDocument(document []byte) (protocol.Unsubscribe, error) {
	requestID, channel, err := decodeChannelRequestDocument(document)
	return protocol.Unsubscribe{RequestID: requestID, Channel: channel}, err
}

// EncodeSubscriptionsRequestDocument 编码 SubscriptionsRequest 文档 [request_id]。
func EncodeSubscriptionsRequestDocument(request protocol.SubscriptionsRequest) []byte {
	result := make([]byte, 0, protocol.RequestIDBytes+3)
	result = append(result, encodeHead(4, 1)...)
	result = append(result, encodeHead(2, protocol.RequestIDBytes)...)
	result = append(result, request.RequestID[:]...)
	return result
}

// DecodeSubscriptionsRequestDocument 严格解析 SubscriptionsRequest 文档。
func DecodeSubscriptionsRequestDocument(document []byte) (protocol.SubscriptionsRequest, error) {
	var result protocol.SubscriptionsRequest
	decoder := newDecoder(document)
	if err := decoder.readArrayExact(1); err != nil {
		return result, err
	}
	requestID, err := decoder.readFixedRequestID()
	if err != nil {
		return result, err
	}
	if !decoder.atEnd() {
		return result, fmt.Errorf("%w: trailing bytes in subscriptions request", protocol.ErrInvalidWire)
	}
	candidate := protocol.SubscriptionsRequest{RequestID: requestID}
	canonical := EncodeSubscriptionsRequestDocument(candidate)
	if !bytes.Equal(canonical, document) {
		return result, fmt.Errorf("%w: subscriptions request is not deterministic CBOR", protocol.ErrNonCanonicalCBOR)
	}
	return candidate, nil
}

// EncodeSubscriptionsResponseDocument 编码订阅列表响应文档
// [request_id, charged_amount, channels]。
func EncodeSubscriptionsResponseDocument(response protocol.SubscriptionsResponse) ([]byte, error) {
	if err := protocol.ValidateAmount(response.ChargedAmount.String()); err != nil {
		return nil, err
	}
	if err := validateCanonicalChannels(response.Channels); err != nil {
		return nil, err
	}
	result := make([]byte, 0, protocol.RequestIDBytes+len(response.ChargedAmount)+8+len(response.Channels)*8)
	result = append(result, encodeHead(4, 3)...)
	result = append(result, encodeHead(2, protocol.RequestIDBytes)...)
	result = append(result, response.RequestID[:]...)
	amountBytes := []byte(response.ChargedAmount.String())
	result = append(result, encodeHead(3, uint64(len(amountBytes)))...)
	result = append(result, amountBytes...)
	result = append(result, encodeHead(4, uint64(len(response.Channels)))...)
	for _, channel := range response.Channels {
		channelBytes := []byte(channel)
		result = append(result, encodeHead(3, uint64(len(channelBytes)))...)
		result = append(result, channelBytes...)
	}
	if len(result) > protocol.MaxDocumentBytes {
		return nil, fmt.Errorf("%w: document exceeds the configured limit", protocol.ErrMessageTooLarge)
	}
	return result, nil
}

// DecodeSubscriptionsResponseDocument 严格解析并复制订阅列表响应。
func DecodeSubscriptionsResponseDocument(document []byte) (protocol.SubscriptionsResponse, error) {
	var result protocol.SubscriptionsResponse
	decoder := newDecoder(document)
	if err := decoder.readArrayExact(3); err != nil {
		return result, err
	}
	requestID, err := decoder.readFixedRequestID()
	if err != nil {
		return result, err
	}
	amountText, err := decoder.readText(protocol.MaxAmountBytes, protocol.ErrInvalidWire)
	if err != nil {
		return result, err
	}
	amount, err := protocol.NewAmount(amountText)
	if err != nil {
		return result, err
	}
	count, err := decoder.readArrayCount(protocol.MaxSubscriptions, protocol.ErrInvalidSubscriptions)
	if err != nil {
		return result, err
	}
	channels := make([]string, 0, count)
	for index := 0; index < count; index++ {
		channel, readErr := decoder.readText(protocol.MaxChannelUTF8Bytes, protocol.ErrInvalidSubscriptions)
		if readErr != nil {
			return result, readErr
		}
		channels = append(channels, channel)
	}
	if !decoder.atEnd() {
		return result, fmt.Errorf("%w: trailing bytes in subscriptions response", protocol.ErrInvalidWire)
	}
	if err := validateCanonicalChannels(channels); err != nil {
		return result, err
	}
	candidate := protocol.SubscriptionsResponse{
		RequestID:     requestID,
		ChargedAmount: amount,
		Channels:      channels,
	}
	canonical, err := EncodeSubscriptionsResponseDocument(candidate)
	if err != nil {
		return result, err
	}
	if !bytes.Equal(canonical, document) {
		return result, fmt.Errorf("%w: subscriptions response is not deterministic CBOR", protocol.ErrNonCanonicalCBOR)
	}
	candidate.Channels = append([]string(nil), channels...)
	return candidate, nil
}

// EncodeActionResultDocument 编码 ActionResult 文档
// [request_id, success, charged_amount, error_code]。
func EncodeActionResultDocument(result protocol.ActionResult) ([]byte, error) {
	if err := protocol.ValidateActionResult(result); err != nil {
		return nil, err
	}
	amountBytes := []byte(result.ChargedAmount.String())
	errorCodeBytes := []byte(result.ErrorCode)
	encoded := make([]byte, 0, protocol.RequestIDBytes+len(amountBytes)+len(errorCodeBytes)+16)
	encoded = append(encoded, encodeHead(4, 4)...)
	encoded = append(encoded, encodeHead(2, protocol.RequestIDBytes)...)
	encoded = append(encoded, result.RequestID[:]...)
	if result.Success {
		encoded = append(encoded, 0xf5)
	} else {
		encoded = append(encoded, 0xf4)
	}
	encoded = append(encoded, encodeHead(3, uint64(len(amountBytes)))...)
	encoded = append(encoded, amountBytes...)
	encoded = append(encoded, encodeHead(3, uint64(len(errorCodeBytes)))...)
	encoded = append(encoded, errorCodeBytes...)
	if len(encoded) > protocol.MaxDocumentBytes {
		return nil, fmt.Errorf("%w: document exceeds the configured limit", protocol.ErrMessageTooLarge)
	}
	return encoded, nil
}

// DecodeActionResultDocument 严格解析 ActionResult 文档并校验字段组合。
func DecodeActionResultDocument(document []byte) (protocol.ActionResult, error) {
	var result protocol.ActionResult
	decoder := newDecoder(document)
	if err := decoder.readArrayExact(4); err != nil {
		return result, err
	}
	requestID, err := decoder.readFixedRequestID()
	if err != nil {
		return result, err
	}
	success, err := decoder.readBool()
	if err != nil {
		return result, err
	}
	amountText, err := decoder.readText(protocol.MaxAmountBytes, protocol.ErrInvalidWire)
	if err != nil {
		return result, err
	}
	amount, err := protocol.NewAmount(amountText)
	if err != nil {
		return result, err
	}
	errorCode, err := decoder.readTextAllowEmpty(protocol.MaxAmountBytes, protocol.ErrInvalidWire)
	if err != nil {
		return result, err
	}
	if !decoder.atEnd() {
		return result, fmt.Errorf("%w: trailing bytes in ActionResult", protocol.ErrInvalidWire)
	}
	candidate := protocol.ActionResult{
		RequestID:     requestID,
		Success:       success,
		ChargedAmount: amount,
		ErrorCode:     errorCode,
	}
	if err := protocol.ValidateActionResult(candidate); err != nil {
		return result, err
	}
	canonical, err := EncodeActionResultDocument(candidate)
	if err != nil {
		return result, err
	}
	if !bytes.Equal(canonical, document) {
		return result, fmt.Errorf("%w: ActionResult is not deterministic CBOR", protocol.ErrNonCanonicalCBOR)
	}
	return candidate, nil
}

func parseOuter(input []byte) (Message, error) {
	var result Message
	if len(input) > protocol.MaxWireBytes {
		return result, fmt.Errorf("%w: wire exceeds the configured limit", protocol.ErrMessageTooLarge)
	}
	decoder := newDecoder(input)
	if err := decoder.readArrayExact(3); err != nil {
		return result, err
	}
	version, err := decoder.readUint()
	if err != nil {
		return result, err
	}
	if protocol.Version(version) != protocol.WireVersion {
		return result, fmt.Errorf("%w: unsupported wire version", protocol.ErrUnsupportedVersion)
	}
	kindValue, err := decoder.readUint()
	if err != nil {
		return result, err
	}
	kind := protocol.Kind(kindValue)
	if !protocol.IsKind(kind) {
		return result, fmt.Errorf("%w: unsupported wire kind", protocol.ErrUnsupportedKind)
	}
	document, err := decoder.readBytes(protocol.MaxDocumentBytes, protocol.ErrMessageTooLarge)
	if err != nil {
		return result, err
	}
	if !decoder.atEnd() {
		return result, fmt.Errorf("%w: trailing bytes after wire", protocol.ErrInvalidWire)
	}
	canonical, err := encodeWireUnchecked(kind, document)
	if err != nil {
		return result, err
	}
	if !bytes.Equal(canonical, input) {
		return result, fmt.Errorf("%w: wire is not deterministic CBOR", protocol.ErrNonCanonicalCBOR)
	}
	return Message{
		Version:  protocol.Version(version),
		Kind:     kind,
		Document: protocol.CloneBytes(document),
	}, nil
}

func decodeRequestIDPrefix(document []byte) (protocol.RequestID, error) {
	decoder := newDecoder(document)
	// 前缀阶段只需要确认“有首字段”，不能因为后续多字段而提前丢失
	// request_id；具体数组长度由 DecodeRequest 的完整校验决定。
	count, err := decoder.readHead(4)
	if err != nil {
		return protocol.RequestID{}, err
	}
	if count == 0 {
		return protocol.RequestID{}, fmt.Errorf("%w: request document is missing request_id", protocol.ErrInvalidWire)
	}
	return decoder.readFixedRequestID()
}

func encodeChannelRequestDocument(requestID protocol.RequestID, channel string) ([]byte, error) {
	if err := protocol.ValidateChannel(channel, true); err != nil {
		return nil, err
	}
	channelBytes := []byte(channel)
	result := make([]byte, 0, protocol.RequestIDBytes+len(channelBytes)+12)
	result = append(result, encodeHead(4, 2)...)
	result = append(result, encodeHead(2, protocol.RequestIDBytes)...)
	result = append(result, requestID[:]...)
	result = append(result, encodeHead(3, uint64(len(channelBytes)))...)
	result = append(result, channelBytes...)
	if len(result) > protocol.MaxDocumentBytes {
		return nil, fmt.Errorf("%w: document exceeds the configured limit", protocol.ErrMessageTooLarge)
	}
	return result, nil
}

func decodeChannelRequestDocument(document []byte) (protocol.RequestID, string, error) {
	decoder := newDecoder(document)
	if err := decoder.readArrayExact(2); err != nil {
		return protocol.RequestID{}, "", err
	}
	requestID, err := decoder.readFixedRequestID()
	if err != nil {
		return protocol.RequestID{}, "", err
	}
	channel, err := decoder.readText(protocol.MaxChannelUTF8Bytes, protocol.ErrInvalidChannel)
	if err != nil {
		return protocol.RequestID{}, "", err
	}
	if channelErr := protocol.ValidateChannel(channel, true); channelErr != nil {
		return protocol.RequestID{}, "", channelErr
	}
	if !decoder.atEnd() {
		return protocol.RequestID{}, "", fmt.Errorf("%w: trailing bytes in channel request", protocol.ErrInvalidWire)
	}
	canonical, err := encodeChannelRequestDocument(requestID, channel)
	if err != nil {
		return protocol.RequestID{}, "", err
	}
	if !bytes.Equal(canonical, document) {
		return protocol.RequestID{}, "", fmt.Errorf("%w: channel request is not deterministic CBOR", protocol.ErrNonCanonicalCBOR)
	}
	return requestID, channel, nil
}

func validateDocument(kind protocol.Kind, document []byte) error {
	switch kind {
	case protocol.PublishKind:
		_, err := DecodePublishDocument(document)
		return err
	case protocol.SubscribeKind:
		_, err := DecodeSubscribeDocument(document)
		return err
	case protocol.UnsubscribeKind:
		_, err := DecodeUnsubscribeDocument(document)
		return err
	case protocol.SubscriptionsRequestKind:
		_, err := DecodeSubscriptionsRequestDocument(document)
		return err
	case protocol.SubscriptionsResponseKind:
		_, err := DecodeSubscriptionsResponseDocument(document)
		return err
	case protocol.ActionResultKind:
		_, err := DecodeActionResultDocument(document)
		return err
	default:
		return fmt.Errorf("%w: kind is not defined by SSP V1", protocol.ErrUnsupportedKind)
	}
}

func isRequestKind(kind protocol.Kind) bool {
	switch kind {
	case protocol.PublishKind, protocol.SubscribeKind, protocol.UnsubscribeKind, protocol.SubscriptionsRequestKind:
		return true
	default:
		return false
	}
}

func encodeWireUnchecked(kind protocol.Kind, document []byte) ([]byte, error) {
	result := make([]byte, 0, len(document)+16)
	result = append(result, encodeHead(4, 3)...)
	result = append(result, encodeHead(0, uint64(protocol.WireVersion))...)
	result = append(result, encodeHead(0, uint64(kind))...)
	result = append(result, encodeHead(2, uint64(len(document)))...)
	result = append(result, document...)
	if len(result) > protocol.MaxWireBytes {
		return nil, fmt.Errorf("%w: wire exceeds the configured limit", protocol.ErrMessageTooLarge)
	}
	return result, nil
}

func validateCanonicalChannels(channels []string) error {
	if len(channels) > protocol.MaxSubscriptions {
		return fmt.Errorf("%w: subscription count exceeds the configured limit", protocol.ErrInvalidSubscriptions)
	}
	if len(channels) == 0 {
		return nil
	}
	for index, channel := range channels {
		if err := protocol.ValidateChannel(channel, true); err != nil {
			return fmt.Errorf("%w: invalid channel in subscription list", protocol.ErrInvalidSubscriptions)
		}
		if channel == "*" {
			if len(channels) != 1 {
				return fmt.Errorf("%w: wildcard cannot be mixed with ordinary channels", protocol.ErrInvalidSubscriptions)
			}
			continue
		}
		if index > 0 {
			previous := channels[index-1]
			if previous == "*" || protocol.CompareChannelsUTF8(previous, channel) >= 0 {
				return fmt.Errorf("%w: channels must be unique and UTF-8 byte sorted", protocol.ErrInvalidSubscriptions)
			}
		}
	}
	return nil
}

func encodeHead(major byte, value uint64) []byte {
	prefix := major << 5
	switch {
	case value < 24:
		return []byte{prefix | byte(value)}
	case value <= 0xff:
		return []byte{prefix | 24, byte(value)}
	case value <= 0xffff:
		return []byte{prefix | 25, byte(value >> 8), byte(value)}
	case value <= 0xffffffff:
		return []byte{prefix | 26, byte(value >> 24), byte(value >> 16), byte(value >> 8), byte(value)}
	default:
		return []byte{prefix | 27, byte(value >> 56), byte(value >> 48), byte(value >> 40), byte(value >> 32), byte(value >> 24), byte(value >> 16), byte(value >> 8), byte(value)}
	}
}

type decoder struct {
	data   []byte
	offset int
}

func newDecoder(data []byte) *decoder {
	return &decoder{data: data}
}

func (decoder *decoder) atEnd() bool {
	return decoder.offset == len(decoder.data)
}

func (decoder *decoder) readHead(expectedMajor byte) (uint64, error) {
	if decoder.offset >= len(decoder.data) {
		return 0, fmt.Errorf("%w: truncated CBOR item", protocol.ErrInvalidWire)
	}
	first := decoder.data[decoder.offset]
	decoder.offset++
	major := first >> 5
	additional := first & 0x1f
	if major != expectedMajor {
		return 0, fmt.Errorf("%w: unexpected CBOR major type", protocol.ErrInvalidWire)
	}
	if additional == 31 {
		return 0, fmt.Errorf("%w: indefinite-length CBOR is forbidden", protocol.ErrInvalidWire)
	}
	var value uint64
	var width int
	switch {
	case additional <= 23:
		value = uint64(additional)
	case additional == 24:
		value, width = decoder.readN(1)
		if width < 0 {
			return 0, fmt.Errorf("%w: truncated CBOR length", protocol.ErrInvalidWire)
		}
		if value < 24 {
			return 0, fmt.Errorf("%w: non-minimal CBOR integer or length", protocol.ErrNonCanonicalCBOR)
		}
	case additional == 25:
		value, width = decoder.readN(2)
		if width < 0 {
			return 0, fmt.Errorf("%w: truncated CBOR length", protocol.ErrInvalidWire)
		}
		if value <= 0xff {
			return 0, fmt.Errorf("%w: non-minimal CBOR integer or length", protocol.ErrNonCanonicalCBOR)
		}
	case additional == 26:
		value, width = decoder.readN(4)
		if width < 0 {
			return 0, fmt.Errorf("%w: truncated CBOR length", protocol.ErrInvalidWire)
		}
		if value <= 0xffff {
			return 0, fmt.Errorf("%w: non-minimal CBOR integer or length", protocol.ErrNonCanonicalCBOR)
		}
	case additional == 27:
		value, width = decoder.readN(8)
		if width < 0 {
			return 0, fmt.Errorf("%w: truncated CBOR length", protocol.ErrInvalidWire)
		}
		if value <= 0xffffffff {
			return 0, fmt.Errorf("%w: non-minimal CBOR integer or length", protocol.ErrNonCanonicalCBOR)
		}
	default:
		return 0, fmt.Errorf("%w: reserved CBOR additional information", protocol.ErrInvalidWire)
	}
	return value, nil
}

func (decoder *decoder) readN(size int) (uint64, int) {
	if len(decoder.data)-decoder.offset < size {
		return 0, -1
	}
	var value uint64
	for index := 0; index < size; index++ {
		value = (value << 8) | uint64(decoder.data[decoder.offset])
		decoder.offset++
	}
	return value, size
}

func (decoder *decoder) readUint() (uint64, error) {
	return decoder.readHead(0)
}

func (decoder *decoder) readArrayExact(expected int) error {
	value, err := decoder.readHead(4)
	if err != nil {
		return err
	}
	if value != uint64(expected) {
		return fmt.Errorf("%w: expected fixed array length", protocol.ErrInvalidWire)
	}
	return nil
}

func (decoder *decoder) readArrayCount(max int, overflow error) (int, error) {
	value, err := decoder.readHead(4)
	if err != nil {
		return 0, err
	}
	if value > uint64(max) {
		return 0, fmt.Errorf("%w: array exceeds the configured limit", overflow)
	}
	return int(value), nil
}

func (decoder *decoder) readBytes(max int, itemError error) ([]byte, error) {
	value, err := decoder.readHead(2)
	if err != nil {
		return nil, err
	}
	if value > uint64(max) {
		return nil, fmt.Errorf("%w: byte string exceeds the configured limit", itemError)
	}
	if value > uint64(len(decoder.data)-decoder.offset) {
		return nil, fmt.Errorf("%w: truncated byte string", protocol.ErrInvalidWire)
	}
	start := decoder.offset
	decoder.offset += int(value)
	return protocol.CloneBytes(decoder.data[start:decoder.offset]), nil
}

func (decoder *decoder) readFixedBytes(size int) ([]byte, error) {
	value, err := decoder.readHead(2)
	if err != nil {
		return nil, err
	}
	if value != uint64(size) {
		return nil, fmt.Errorf("%w: byte string must be exactly %d bytes", protocol.ErrInvalidWire, size)
	}
	if value > uint64(len(decoder.data)-decoder.offset) {
		return nil, fmt.Errorf("%w: truncated byte string", protocol.ErrInvalidWire)
	}
	start := decoder.offset
	decoder.offset += size
	return protocol.CloneBytes(decoder.data[start:decoder.offset]), nil
}

func (decoder *decoder) readFixedRequestID() (protocol.RequestID, error) {
	value, err := decoder.readFixedBytes(protocol.RequestIDBytes)
	if err != nil {
		return protocol.RequestID{}, err
	}
	return protocol.NewRequestIDFromBytes(value)
}

func (decoder *decoder) readText(max int, itemError error) (string, error) {
	return decoder.readTextInternal(max, itemError, false)
}

func (decoder *decoder) readTextAllowEmpty(max int, itemError error) (string, error) {
	return decoder.readTextInternal(max, itemError, true)
}

func (decoder *decoder) readTextInternal(max int, itemError error, allowEmpty bool) (string, error) {
	value, err := decoder.readHead(3)
	if err != nil {
		return "", err
	}
	if (!allowEmpty && value == 0) || value > uint64(max) {
		return "", fmt.Errorf("%w: text string is empty or too long", itemError)
	}
	if value > uint64(len(decoder.data)-decoder.offset) {
		return "", fmt.Errorf("%w: truncated text string", protocol.ErrInvalidWire)
	}
	start := decoder.offset
	decoder.offset += int(value)
	text := decoder.data[start:decoder.offset]
	if !utf8.Valid(text) {
		return "", fmt.Errorf("%w: text string is not valid UTF-8", itemError)
	}
	return string(text), nil
}

func (decoder *decoder) readBool() (bool, error) {
	if decoder.offset >= len(decoder.data) {
		return false, fmt.Errorf("%w: truncated CBOR boolean", protocol.ErrInvalidWire)
	}
	switch decoder.data[decoder.offset] {
	case 0xf4:
		decoder.offset++
		return false, nil
	case 0xf5:
		decoder.offset++
		return true, nil
	default:
		return false, fmt.Errorf("%w: ActionResult success must be CBOR bool", protocol.ErrInvalidWire)
	}
}
