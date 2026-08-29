// Package protocol 定义 SSP V1 的公共类型、常量和稳定错误分类。
package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"unicode/utf8"
)

// Version 是 SSP Wire 的版本数值类型。
type Version uint64

// Kind 是 SSP Wire 的业务消息类型。
type Kind uint64

const (
	// SDKVersion 是 Go 与 TypeScript SDK 对齐的发布版本，从 0.1.0 开始。
	SDKVersion = "0.1.0"

	// WireVersion 是 SSP V1 的固定 Wire 版本常量。
	WireVersion Version = 1
	// CurrentWireVersion 是当前唯一支持的 Wire 版本。
	CurrentWireVersion Version = WireVersion
	// WireVersionV1 是 SSP V1 的 Wire 版本兼容别名。
	WireVersionV1 Version = WireVersion

	// PublishKind 表示发布或广播一条 JSON 消息。
	PublishKind Kind = 1
	// SubscribeKind 表示订阅一个普通 channel 或所有 channel。
	SubscribeKind Kind = 2
	// UnsubscribeKind 表示取消订阅一个普通 channel 或全部 channel。
	UnsubscribeKind Kind = 3
	// SubscriptionsRequestKind 表示查询当前订阅集合。
	SubscriptionsRequestKind Kind = 4
	// SubscriptionsResponseKind 表示返回当前订阅集合。
	SubscriptionsResponseKind Kind = 5

	// KindPublish 是 PublishKind 的语义化兼容别名。
	KindPublish Kind = PublishKind
	// KindSubscribe 是 SubscribeKind 的语义化兼容别名。
	KindSubscribe Kind = SubscribeKind
	// KindUnsubscribe 是 UnsubscribeKind 的语义化兼容别名。
	KindUnsubscribe Kind = UnsubscribeKind
	// KindSubscriptionsRequest 是 SubscriptionsRequestKind 的语义化兼容别名。
	KindSubscriptionsRequest Kind = SubscriptionsRequestKind
	// KindSubscriptionsResponse 是 SubscriptionsResponseKind 的语义化兼容别名。
	KindSubscriptionsResponse Kind = SubscriptionsResponseKind

	// MaxWireBytes 是完整 SSP Wire 允许的最大字节数（1 MiB）。
	MaxWireBytes = 1_048_576
	// MaxDocumentBytes 是外层 document_cbor 允许的最大字节数。
	MaxDocumentBytes = 1_048_512
	// MaxChannelUTF8Bytes 是 channel UTF-8 编码允许的最大字节数。
	MaxChannelUTF8Bytes = 256
	// MaxContentJSONBytes 是 content_json 原始字节允许的最大长度。
	MaxContentJSONBytes = 1_048_000
	// MaxSubscriptions 是一个订阅集合允许的最大元素数。
	MaxSubscriptions = 1_024
)

// ErrorCode 是 SDK 对外稳定的本地错误分类。
type ErrorCode string

const (
	// CodeInvalidWire 表示 Wire 或固定数组结构无效。
	CodeInvalidWire ErrorCode = "INVALID_WIRE"
	// CodeUnsupportedVersion 表示 Wire 版本不受支持。
	CodeUnsupportedVersion ErrorCode = "UNSUPPORTED_VERSION"
	// CodeUnsupportedKind 表示 Wire Kind 不受支持。
	CodeUnsupportedKind ErrorCode = "UNSUPPORTED_KIND"
	// CodeNonCanonicalCBOR 表示 CBOR 不是唯一的 deterministic 编码。
	CodeNonCanonicalCBOR ErrorCode = "NON_CANONICAL_CBOR"
	// CodeMessageTooLarge 表示消息、document 或字段超过冻结上限。
	CodeMessageTooLarge ErrorCode = "MESSAGE_TOO_LARGE"
	// CodeInvalidChannel 表示 channel 为空、非法 UTF-8、超长或使用位置不合法。
	CodeInvalidChannel ErrorCode = "INVALID_CHANNEL"
	// CodeInvalidJSON 表示 content_json 不是合法完整 JSON 文本。
	CodeInvalidJSON ErrorCode = "INVALID_JSON"
	// CodeInvalidSubscriptions 表示订阅集合重复、乱序、混合通配符或超限。
	CodeInvalidSubscriptions ErrorCode = "INVALID_SUBSCRIPTIONS"
	// CodeInvalidPublicKey 表示外部认证公钥不是 33 字节压缩公钥。
	CodeInvalidPublicKey ErrorCode = "INVALID_PUBLIC_KEY"
	// CodeBackendFailure 表示订阅持久化 Backend 失败。
	CodeBackendFailure ErrorCode = "BACKEND_FAILURE"
	// CodeDispatchFailure 表示广播 Dispatcher 失败。
	CodeDispatchFailure ErrorCode = "DISPATCH_FAILURE"
)

var (
	// ErrInvalidWire 是 INVALID_WIRE 的稳定 sentinel error。
	ErrInvalidWire = errors.New(string(CodeInvalidWire))
	// ErrUnsupportedVersion 是 UNSUPPORTED_VERSION 的稳定 sentinel error。
	ErrUnsupportedVersion = errors.New(string(CodeUnsupportedVersion))
	// ErrUnsupportedKind 是 UNSUPPORTED_KIND 的稳定 sentinel error。
	ErrUnsupportedKind = errors.New(string(CodeUnsupportedKind))
	// ErrNonCanonicalCBOR 是 NON_CANONICAL_CBOR 的稳定 sentinel error。
	ErrNonCanonicalCBOR = errors.New(string(CodeNonCanonicalCBOR))
	// ErrMessageTooLarge 是 MESSAGE_TOO_LARGE 的稳定 sentinel error。
	ErrMessageTooLarge = errors.New(string(CodeMessageTooLarge))
	// ErrInvalidChannel 是 INVALID_CHANNEL 的稳定 sentinel error。
	ErrInvalidChannel = errors.New(string(CodeInvalidChannel))
	// ErrInvalidJSON 是 INVALID_JSON 的稳定 sentinel error。
	ErrInvalidJSON = errors.New(string(CodeInvalidJSON))
	// ErrInvalidSubscriptions 是 INVALID_SUBSCRIPTIONS 的稳定 sentinel error。
	ErrInvalidSubscriptions = errors.New(string(CodeInvalidSubscriptions))
	// ErrInvalidPublicKey 是 INVALID_PUBLIC_KEY 的稳定 sentinel error。
	ErrInvalidPublicKey = errors.New(string(CodeInvalidPublicKey))
	// ErrBackendFailure 是 BACKEND_FAILURE 的稳定 sentinel error。
	ErrBackendFailure = errors.New(string(CodeBackendFailure))
	// ErrDispatchFailure 是 DISPATCH_FAILURE 的稳定 sentinel error。
	ErrDispatchFailure = errors.New(string(CodeDispatchFailure))
)

// ErrorCodeOf 返回错误包含的稳定 SDK 错误分类。
func ErrorCodeOf(err error) ErrorCode {
	switch {
	case errors.Is(err, ErrInvalidWire):
		return CodeInvalidWire
	case errors.Is(err, ErrUnsupportedVersion):
		return CodeUnsupportedVersion
	case errors.Is(err, ErrUnsupportedKind):
		return CodeUnsupportedKind
	case errors.Is(err, ErrNonCanonicalCBOR):
		return CodeNonCanonicalCBOR
	case errors.Is(err, ErrMessageTooLarge):
		return CodeMessageTooLarge
	case errors.Is(err, ErrInvalidChannel):
		return CodeInvalidChannel
	case errors.Is(err, ErrInvalidJSON):
		return CodeInvalidJSON
	case errors.Is(err, ErrInvalidSubscriptions):
		return CodeInvalidSubscriptions
	case errors.Is(err, ErrInvalidPublicKey):
		return CodeInvalidPublicKey
	case errors.Is(err, ErrBackendFailure):
		return CodeBackendFailure
	case errors.Is(err, ErrDispatchFailure):
		return CodeDispatchFailure
	default:
		return ""
	}
}

// PublicKey 是外层认证提供的 33 字节公钥字节；它不进入 SSP Wire。
type PublicKey [33]byte

// NewPublicKey 校验并复制外部认证得到的公钥字节形状。
// SDK 只检查 33 字节长度和 02/03 压缩前缀，不验证 secp256k1 曲线点；
// 真正的公钥认证必须由外层长期连接完成。
func NewPublicKey(raw []byte) (PublicKey, error) {
	var key PublicKey
	if len(raw) != len(key) || (raw[0] != 0x02 && raw[0] != 0x03) {
		return key, fmt.Errorf("%w: compressed public key must be 33 bytes", ErrInvalidPublicKey)
	}
	copy(key[:], raw)
	return key, nil
}

// Valid 判断 PublicKey 是否具有 SDK 要求的 33 字节压缩公钥字节形状。
// Valid 不执行 secp256k1 曲线点验证。
func (key PublicKey) Valid() bool {
	return key[0] == 0x02 || key[0] == 0x03
}

// Bytes 返回 PublicKey 的防御性副本。
func (key PublicKey) Bytes() []byte {
	copyOfKey := make([]byte, len(key))
	copy(copyOfKey, key[:])
	return copyOfKey
}

// Publish 是已验证的 Publish 文档；ContentJSON 始终保存原始 JSON 字节。
type Publish struct {
	// Channel 是精确匹配的普通 channel 名称。
	Channel string
	// ContentJSON 是完整 JSON 文本的原始 UTF-8 字节，不会被重新序列化。
	ContentJSON []byte
}

// CloneBytes 返回字节切片的防御性副本；nil 输入仍返回 nil。
func CloneBytes(input []byte) []byte {
	if input == nil {
		return nil
	}
	output := make([]byte, len(input))
	copy(output, input)
	return output
}

// IsKind 判断 Kind 是否属于 SSP V1 的五种固定消息类型。
func IsKind(kind Kind) bool {
	switch kind {
	case PublishKind, SubscribeKind, UnsubscribeKind, SubscriptionsRequestKind, SubscriptionsResponseKind:
		return true
	default:
		return false
	}
}

// ValidateChannel 校验 channel 的 UTF-8、长度和通配符使用位置。
func ValidateChannel(channel string, allowWildcard bool) error {
	if channel == "" || len(channel) > MaxChannelUTF8Bytes || !utf8.ValidString(channel) {
		return fmt.Errorf("%w: channel is empty, invalid UTF-8, or too long", ErrInvalidChannel)
	}
	if channel == "*" && !allowWildcard {
		return fmt.Errorf("%w: wildcard is not allowed here", ErrInvalidChannel)
	}
	return nil
}

// ValidateJSON 校验一份完整合法的 RFC 8259 JSON 文本，但不改变原始字节。
func ValidateJSON(contentJSON []byte) error {
	if len(contentJSON) > MaxContentJSONBytes {
		return fmt.Errorf("%w: content_json exceeds the configured limit", ErrMessageTooLarge)
	}
	if !utf8.Valid(contentJSON) || !json.Valid(contentJSON) {
		return fmt.Errorf("%w: content_json is not one complete JSON value", ErrInvalidJSON)
	}
	return nil
}

// CompareChannelsUTF8 按 UTF-8 字节升序比较两个 channel，供规范排序使用。
func CompareChannelsUTF8(left, right string) int {
	return bytes.Compare([]byte(left), []byte(right))
}

// SortChannelsUTF8 对 channel 副本按 UTF-8 字节升序排序。
func SortChannelsUTF8(channels []string) []string {
	result := append([]string(nil), channels...)
	sort.Slice(result, func(i, j int) bool {
		return CompareChannelsUTF8(result[i], result[j]) < 0
	})
	return result
}
