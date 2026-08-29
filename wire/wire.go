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
	// Kind 是五种固定 SSP 消息类型之一。
	Kind protocol.Kind
	// Document 是内层 document_cbor 的 exact bytes 防御性副本。
	Document []byte
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

// Parse 严格解析完整 SSP Wire，并校验对应的内层文档。
func Parse(input []byte) (Message, error) {
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
	if err := validateDocument(kind, document); err != nil {
		return result, err
	}

	canonical, err := encodeWireUnchecked(kind, document)
	if err != nil {
		return result, err
	}
	if !bytes.Equal(canonical, input) {
		return result, fmt.Errorf("%w: wire is not deterministic CBOR", protocol.ErrNonCanonicalCBOR)
	}
	result.Version = protocol.Version(version)
	result.Kind = kind
	result.Document = protocol.CloneBytes(document)
	return result, nil
}

// EncodePublishDocument 编码 Publish 内层文档 [channel, content_json]。
func EncodePublishDocument(publish protocol.Publish) ([]byte, error) {
	if err := protocol.ValidateChannel(publish.Channel, false); err != nil {
		return nil, err
	}
	if err := protocol.ValidateJSON(publish.ContentJSON); err != nil {
		return nil, err
	}
	channelBytes := []byte(publish.Channel)
	result := make([]byte, 0, len(channelBytes)+len(publish.ContentJSON)+16)
	result = append(result, encodeHead(4, 2)...)
	result = append(result, encodeHead(3, uint64(len(channelBytes)))...)
	result = append(result, channelBytes...)
	result = append(result, encodeHead(2, uint64(len(publish.ContentJSON)))...)
	result = append(result, publish.ContentJSON...)
	if len(result) > protocol.MaxDocumentBytes {
		return nil, fmt.Errorf("%w: document exceeds the configured limit", protocol.ErrMessageTooLarge)
	}
	return result, nil
}

// DecodePublishDocument 严格解析 Publish 内层文档，并复制 content_json。
func DecodePublishDocument(document []byte) (protocol.Publish, error) {
	var result protocol.Publish
	decoder := newDecoder(document)
	if err := decoder.readArrayExact(2); err != nil {
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
	canonical, err := EncodePublishDocument(protocol.Publish{Channel: channel, ContentJSON: contentJSON})
	if err != nil {
		return result, err
	}
	if !bytes.Equal(canonical, document) {
		return result, fmt.Errorf("%w: publish document is not deterministic CBOR", protocol.ErrNonCanonicalCBOR)
	}
	result.Channel = channel
	result.ContentJSON = protocol.CloneBytes(contentJSON)
	return result, nil
}

// EncodeSubscribeDocument 编码 Subscribe 内层文档 [channel]。
func EncodeSubscribeDocument(channel string) ([]byte, error) {
	return encodeSingleChannelDocument(channel)
}

// DecodeSubscribeDocument 严格解析 Subscribe 内层文档。
func DecodeSubscribeDocument(document []byte) (string, error) {
	return decodeSingleChannelDocument(document)
}

// EncodeUnsubscribeDocument 编码 Unsubscribe 内层文档 [channel]。
func EncodeUnsubscribeDocument(channel string) ([]byte, error) {
	return encodeSingleChannelDocument(channel)
}

// DecodeUnsubscribeDocument 严格解析 Unsubscribe 内层文档。
func DecodeUnsubscribeDocument(document []byte) (string, error) {
	return decodeSingleChannelDocument(document)
}

// EncodeSubscriptionsRequestDocument 编码空数组请求文档 []。
func EncodeSubscriptionsRequestDocument() []byte {
	return []byte{0x80}
}

// DecodeSubscriptionsRequestDocument 严格解析空数组请求文档。
func DecodeSubscriptionsRequestDocument(document []byte) error {
	decoder := newDecoder(document)
	if err := decoder.readArrayExact(0); err != nil {
		return err
	}
	if !decoder.atEnd() {
		return fmt.Errorf("%w: trailing bytes in subscriptions request", protocol.ErrInvalidWire)
	}
	if !bytes.Equal(document, EncodeSubscriptionsRequestDocument()) {
		return fmt.Errorf("%w: request document is not deterministic CBOR", protocol.ErrNonCanonicalCBOR)
	}
	return nil
}

// EncodeSubscriptionsResponseDocument 编码规范订阅列表文档 [[channels]]。
func EncodeSubscriptionsResponseDocument(channels []string) ([]byte, error) {
	if err := validateCanonicalChannels(channels); err != nil {
		return nil, err
	}
	result := make([]byte, 0, 8+len(channels)*8)
	result = append(result, encodeHead(4, 1)...)
	result = append(result, encodeHead(4, uint64(len(channels)))...)
	for _, channel := range channels {
		channelBytes := []byte(channel)
		result = append(result, encodeHead(3, uint64(len(channelBytes)))...)
		result = append(result, channelBytes...)
	}
	if len(result) > protocol.MaxDocumentBytes {
		return nil, fmt.Errorf("%w: document exceeds the configured limit", protocol.ErrMessageTooLarge)
	}
	return result, nil
}

// DecodeSubscriptionsResponseDocument 严格解析并复制规范订阅列表。
func DecodeSubscriptionsResponseDocument(document []byte) ([]string, error) {
	decoder := newDecoder(document)
	if err := decoder.readArrayExact(1); err != nil {
		return nil, err
	}
	count, err := decoder.readArrayCount(protocol.MaxSubscriptions, protocol.ErrInvalidSubscriptions)
	if err != nil {
		return nil, err
	}
	channels := make([]string, 0, count)
	for index := 0; index < count; index++ {
		channel, readErr := decoder.readText(protocol.MaxChannelUTF8Bytes, protocol.ErrInvalidChannel)
		if readErr != nil {
			return nil, readErr
		}
		if validateErr := protocol.ValidateChannel(channel, true); validateErr != nil {
			return nil, validateErr
		}
		channels = append(channels, channel)
	}
	if !decoder.atEnd() {
		return nil, fmt.Errorf("%w: trailing bytes in subscriptions response", protocol.ErrInvalidWire)
	}
	if err := validateCanonicalChannels(channels); err != nil {
		return nil, err
	}
	canonical, err := EncodeSubscriptionsResponseDocument(channels)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(canonical, document) {
		return nil, fmt.Errorf("%w: subscriptions response is not deterministic CBOR", protocol.ErrNonCanonicalCBOR)
	}
	return append([]string(nil), channels...), nil
}

func encodeSingleChannelDocument(channel string) ([]byte, error) {
	if err := protocol.ValidateChannel(channel, true); err != nil {
		return nil, err
	}
	channelBytes := []byte(channel)
	result := make([]byte, 0, len(channelBytes)+8)
	result = append(result, encodeHead(4, 1)...)
	result = append(result, encodeHead(3, uint64(len(channelBytes)))...)
	result = append(result, channelBytes...)
	if len(result) > protocol.MaxDocumentBytes {
		return nil, fmt.Errorf("%w: document exceeds the configured limit", protocol.ErrMessageTooLarge)
	}
	return result, nil
}

func decodeSingleChannelDocument(document []byte) (string, error) {
	decoder := newDecoder(document)
	if err := decoder.readArrayExact(1); err != nil {
		return "", err
	}
	channel, err := decoder.readText(protocol.MaxChannelUTF8Bytes, protocol.ErrInvalidChannel)
	if err != nil {
		return "", err
	}
	if channelErr := protocol.ValidateChannel(channel, true); channelErr != nil {
		return "", channelErr
	}
	if !decoder.atEnd() {
		return "", fmt.Errorf("%w: trailing bytes in channel document", protocol.ErrInvalidWire)
	}
	canonical, err := encodeSingleChannelDocument(channel)
	if err != nil {
		return "", err
	}
	if !bytes.Equal(canonical, document) {
		return "", fmt.Errorf("%w: channel document is not deterministic CBOR", protocol.ErrNonCanonicalCBOR)
	}
	return channel, nil
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
		return DecodeSubscriptionsRequestDocument(document)
	case protocol.SubscriptionsResponseKind:
		_, err := DecodeSubscriptionsResponseDocument(document)
		return err
	default:
		return fmt.Errorf("%w: kind is not defined by SSP V1", protocol.ErrUnsupportedKind)
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

func (decoder *decoder) readText(max int, itemError error) (string, error) {
	value, err := decoder.readHead(3)
	if err != nil {
		return "", err
	}
	if value == 0 || value > uint64(max) {
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
