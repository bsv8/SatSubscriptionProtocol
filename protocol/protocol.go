// Package protocol 定义 SSP V1 的公共类型、常量和稳定错误分类。
package protocol

import (
	"bytes"
	"crypto/rand"
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
	// SDKVersion 是 Go 与 TypeScript SDK 对齐的发布版本。
	SDKVersion = "0.3.0"

	// WireVersion 是 SSP V1 的固定 Wire 版本常量。
	WireVersion Version = 1

	// PublishKind 表示发布请求或向订阅者交付一条 JSON 广播。
	PublishKind Kind = 1
	// SubscribeKind 表示订阅一个普通 channel 或所有 channel。
	SubscribeKind Kind = 2
	// UnsubscribeKind 表示取消订阅一个普通 channel 或全部 channel。
	UnsubscribeKind Kind = 3
	// SubscriptionsRequestKind 表示查询当前订阅集合。
	SubscriptionsRequestKind Kind = 4
	// SubscriptionsResponseKind 表示返回当前订阅集合。
	SubscriptionsResponseKind Kind = 5
	// ActionResultKind 表示返回动作成功/失败及实际扣费金额。
	ActionResultKind Kind = 6
	// BillingRequestKind 表示查询当前已认证身份账单的请求。
	BillingRequestKind Kind = 7
	// BillingResponseKind 表示返回账单分页记录或固定错误码。
	BillingResponseKind Kind = 8

	// RequestIDBytes 是 request_id 的固定字节长度。
	RequestIDBytes = 32
	// MaxAmountBytes 是 charged_amount 的完整 ASCII 字节上限。
	MaxAmountBytes = 64
	// MaxAmountScale 是 charged_amount 允许的小数位数上限。
	MaxAmountScale = 18
	// MaxRequestIDsPerSession 是单个 Stream Session 可登记的最大编号数量。
	MaxRequestIDsPerSession = 65_536

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
	// MaxBillingLimit 是一次账单查询允许请求的最大记录数。
	MaxBillingLimit uint64 = 100
	// MaxBillingRecords 是一页账单响应允许返回的最大记录数。
	MaxBillingRecords = 100
	// MaxBillingTextBytes 是账单标识、动作、货币和网络文本的 UTF-8 字节上限。
	MaxBillingTextBytes = 256
	// MaxBillingCursorBytes 是不透明分页游标的 UTF-8 字节上限。
	MaxBillingCursorBytes = 256
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
	// CodeSessionLimit 表示 Session 已达到 request_id 登记上限。
	CodeSessionLimit ErrorCode = "SESSION_LIMIT"
	// CodeSessionClosed 表示 Session 已关闭。
	CodeSessionClosed ErrorCode = "SESSION_CLOSED"
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
	// ErrSessionLimit 是 SESSION_LIMIT 的稳定本地 sentinel error。
	ErrSessionLimit = errors.New(string(CodeSessionLimit))
	// ErrSessionClosed 是 SESSION_CLOSED 的稳定本地 sentinel error。
	ErrSessionClosed = errors.New(string(CodeSessionClosed))
	// ErrRejected 表示 Backend 或 Dispatcher 明确拒绝了业务动作。
	// 它不会直接编码到 Wire；Service 会将其映射为 ActionResult.REJECTED。
	ErrRejected = errors.New("REJECTED")
)

const (
	// ActionErrorInvalidRequest 是 request_id 可识别但业务字段无效的线上错误码。
	ActionErrorInvalidRequest = "INVALID_REQUEST"
	// ActionErrorRejected 是余额、权限或服务策略拒绝的线上错误码。
	ActionErrorRejected = "REJECTED"
	// ActionErrorInternalError 是服务端内部处理失败的线上错误码。
	ActionErrorInternalError = "INTERNAL_ERROR"
)

// ErrorCodeOf 返回错误包含的稳定 SDK 错误分类。
func ErrorCodeOf(err error) ErrorCode {
	switch {
	case errors.Is(err, ErrBackendFailure):
		return CodeBackendFailure
	case errors.Is(err, ErrDispatchFailure):
		return CodeDispatchFailure
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
	case errors.Is(err, ErrSessionLimit):
		return CodeSessionLimit
	case errors.Is(err, ErrSessionClosed):
		return CodeSessionClosed
	default:
		return ""
	}
}

// RequestID 是同一 Stream 内关联请求与响应的固定 32 字节编号。
// 数组值本身不可通过外部切片修改；Bytes 返回的切片也是防御性副本。
type RequestID [RequestIDBytes]byte

// NewRequestID 使用操作系统安全随机源生成一个新的 request_id。
func NewRequestID() (RequestID, error) {
	var requestID RequestID
	if _, err := rand.Read(requestID[:]); err != nil {
		return RequestID{}, fmt.Errorf("generate request_id: %w", err)
	}
	return requestID, nil
}

// NewRequestIDFromBytes 校验并复制固定长度的 request_id。
func NewRequestIDFromBytes(raw []byte) (RequestID, error) {
	var requestID RequestID
	if len(raw) != RequestIDBytes {
		return requestID, fmt.Errorf("%w: request_id must be exactly %d bytes", ErrInvalidWire, RequestIDBytes)
	}
	copy(requestID[:], raw)
	return requestID, nil
}

// Valid 判断 RequestID 是否具有固定长度。数组类型总是满足该长度要求。
func (requestID RequestID) Valid() bool {
	return len(requestID) == RequestIDBytes
}

// Bytes 返回 request_id 的防御性副本。
func (requestID RequestID) Bytes() []byte {
	result := make([]byte, RequestIDBytes)
	copy(result, requestID[:])
	return result
}

// Amount 是规范化的精确十进制金额字符串。
// 它不提供 float32/float64 构造接口，线上金额不会经过二进制浮点。
type Amount string

// ZeroAmount 是免费或失败结果的规范金额编码。
const ZeroAmount Amount = "0"

// NewAmount 校验并创建规范十进制金额。
func NewAmount(value string) (Amount, error) {
	if err := ValidateAmount(value); err != nil {
		return "", err
	}
	return Amount(value), nil
}

// ValidateAmount 校验 charged_amount 的精确十进制规范。
func ValidateAmount(value string) error {
	if len(value) == 0 || len(value) > MaxAmountBytes {
		return fmt.Errorf("%w: amount must contain 1..%d ASCII bytes", ErrInvalidWire, MaxAmountBytes)
	}
	for index := 0; index < len(value); index++ {
		if value[index] > 0x7f {
			return fmt.Errorf("%w: amount must be ASCII", ErrInvalidWire)
		}
	}

	dot := -1
	for index := 0; index < len(value); index++ {
		switch value[index] {
		case '.':
			if dot >= 0 {
				return fmt.Errorf("%w: amount has more than one decimal point", ErrInvalidWire)
			}
			dot = index
		case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		default:
			return fmt.Errorf("%w: amount contains a forbidden character", ErrInvalidWire)
		}
	}

	integerEnd := len(value)
	if dot >= 0 {
		integerEnd = dot
		fractionLength := len(value) - dot - 1
		if fractionLength == 0 || fractionLength > MaxAmountScale {
			return fmt.Errorf("%w: amount scale must be 1..%d digits", ErrInvalidWire, MaxAmountScale)
		}
		if value[len(value)-1] == '0' {
			return fmt.Errorf("%w: amount must not have trailing fractional zero", ErrInvalidWire)
		}
	}
	if integerEnd == 0 {
		return fmt.Errorf("%w: amount requires integer digits before decimal point", ErrInvalidWire)
	}
	if value[0] == '0' && integerEnd != 1 {
		return fmt.Errorf("%w: amount has unnecessary leading zero", ErrInvalidWire)
	}
	for index := 0; index < integerEnd; index++ {
		if value[index] < '0' || value[index] > '9' {
			return fmt.Errorf("%w: amount integer part is invalid", ErrInvalidWire)
		}
	}
	return nil
}

// Valid 判断 Amount 是否已经是规范十进制金额。
func (amount Amount) Valid() bool {
	return ValidateAmount(string(amount)) == nil
}

// String 返回金额的规范十进制字符串。
func (amount Amount) String() string {
	return string(amount)
}

// IsActionErrorCode 判断线上 ActionResult 错误码是否属于 V1 固定集合。
func IsActionErrorCode(code string) bool {
	switch code {
	case ActionErrorInvalidRequest, ActionErrorRejected, ActionErrorInternalError:
		return true
	default:
		return false
	}
}

// ValidateActionResult 校验 ActionResult 的字段组合。
func ValidateActionResult(result ActionResult) error {
	if err := ValidateAmount(result.ChargedAmount.String()); err != nil {
		return err
	}
	if result.Success {
		if result.ErrorCode != "" {
			return fmt.Errorf("%w: successful ActionResult must have empty error_code", ErrInvalidWire)
		}
		return nil
	}
	if result.ChargedAmount != ZeroAmount || !IsActionErrorCode(result.ErrorCode) {
		return fmt.Errorf("%w: failed ActionResult must have charged_amount 0 and a fixed error_code", ErrInvalidWire)
	}
	return nil
}

// BillingRequest 是查询当前已认证身份账单的一页请求。
// from_ms 包含，to_ms 不包含；cursor 为空表示第一页。
type BillingRequest struct {
	// RequestID 是同一 Stream 内关联账单请求与响应的编号。
	RequestID RequestID
	// FromMS 是发生时间范围的起点，单位为 Unix 毫秒，包含该时刻。
	FromMS uint64
	// ToMS 是发生时间范围的终点，单位为 Unix 毫秒，不包含该时刻。
	ToMS uint64
	// Limit 是本页最多返回的记录数，范围为 1..100。
	Limit uint64
	// Cursor 是不透明分页游标；第一页必须为空。
	Cursor string
}

// ChargeRecord 是一条实际发生的正扣费记录。
// 免费操作和失败操作不应出现在账单记录中。
type ChargeRecord struct {
	// ChargeID 是账单记录唯一编号，当前由 operation_id 映射而来。
	ChargeID string
	// OccurredAtMS 是扣费发生时间，单位为 Unix 毫秒。
	OccurredAtMS uint64
	// Action 是产生扣费的 SSP 动作名称。
	Action string
	// Channel 是产生扣费的 channel；订阅相关动作允许 "*"。
	Channel string
	// SourceRequestID 是产生扣费的 SSP 请求编号。
	SourceRequestID RequestID
	// ChargedAmount 是精确十进制正金额，不使用浮点数。
	ChargedAmount Amount
}

// BillingPage 是 Backend 返回的一页账单数据。
type BillingPage struct {
	// Currency 是金额货币标识，例如 BSV；由 Backend/部署配置提供。
	Currency string
	// Network 是账单所属网络标识，例如 mainnet 或 testnet。
	Network string
	// Records 是按 Backend 定义顺序返回的实际正扣费记录。
	Records []ChargeRecord
	// NextCursor 是下一页不透明游标；没有下一页时为空。
	NextCursor string
}

// ChargeRecordPage 是 BillingPage 的兼容别名，便于调用方按记录分页语义命名。
type ChargeRecordPage = BillingPage

// BillingResponse 是账单查询响应；失败时除 request_id 和 error_code 外字段均为空。
type BillingResponse struct {
	// RequestID 是原样回显的账单请求编号。
	RequestID RequestID
	// Success 表示账单查询是否成功。
	Success bool
	// Currency 是成功结果的金额货币标识。
	Currency string
	// Network 是成功结果的账单网络标识。
	Network string
	// Records 是成功结果的实际正扣费记录页。
	Records []ChargeRecord
	// NextCursor 是成功结果的下一页不透明游标。
	NextCursor string
	// ErrorCode 成功为空；失败只能是三个固定线上错误码之一。
	ErrorCode string
}

// ValidateBillingRequest 校验账单查询的范围、页大小和游标。
func ValidateBillingRequest(request BillingRequest) error {
	if request.FromMS > request.ToMS {
		return fmt.Errorf("%w: billing from_ms must be less than or equal to to_ms", ErrInvalidWire)
	}
	if request.Limit < 1 || request.Limit > MaxBillingLimit {
		return fmt.Errorf("%w: billing limit must be between 1 and %d", ErrInvalidWire, MaxBillingLimit)
	}
	return validateBillingText(request.Cursor, MaxBillingCursorBytes, true, "billing cursor")
}

// ValidateChargeRecord 校验账单记录字段和“只返回正扣费”的约束。
func ValidateChargeRecord(record ChargeRecord) error {
	if err := validateBillingText(record.ChargeID, MaxBillingTextBytes, false, "charge_id"); err != nil {
		return err
	}
	if err := validateBillingText(record.Action, MaxBillingTextBytes, false, "action"); err != nil {
		return err
	}
	if err := ValidateChannel(record.Channel, true); err != nil {
		return fmt.Errorf("%w: billing record channel is invalid", ErrInvalidWire)
	}
	if err := ValidateAmount(record.ChargedAmount.String()); err != nil {
		return err
	}
	if record.ChargedAmount == ZeroAmount {
		return fmt.Errorf("%w: billing record charged_amount must be positive", ErrInvalidWire)
	}
	return nil
}

// ValidateBillingPage 校验 Backend 返回的成功账单页。
func ValidateBillingPage(page BillingPage) error {
	if err := validateBillingText(page.Currency, MaxBillingTextBytes, false, "currency"); err != nil {
		return err
	}
	if err := validateBillingText(page.Network, MaxBillingTextBytes, false, "network"); err != nil {
		return err
	}
	if len(page.Records) > MaxBillingRecords {
		return fmt.Errorf("%w: billing records exceed the configured page limit", ErrInvalidWire)
	}
	if err := validateBillingText(page.NextCursor, MaxBillingCursorBytes, true, "next_cursor"); err != nil {
		return err
	}
	for index, record := range page.Records {
		if err := ValidateChargeRecord(record); err != nil {
			return fmt.Errorf("%w: invalid billing record at index %d: %v", ErrInvalidWire, index, err)
		}
	}
	return nil
}

// ValidateBillingResponse 校验 BillingResponse 的成功/失败字段组合。
func ValidateBillingResponse(response BillingResponse) error {
	if response.Success {
		if response.ErrorCode != "" {
			return fmt.Errorf("%w: successful BillingResponse must have empty error_code", ErrInvalidWire)
		}
		return ValidateBillingPage(BillingPage{
			Currency:   response.Currency,
			Network:    response.Network,
			Records:    response.Records,
			NextCursor: response.NextCursor,
		})
	}
	if response.Currency != "" || response.Network != "" || len(response.Records) != 0 || response.NextCursor != "" || !IsActionErrorCode(response.ErrorCode) {
		return fmt.Errorf("%w: failed BillingResponse must have empty page fields and a fixed error_code", ErrInvalidWire)
	}
	return nil
}

func validateBillingText(value string, maximum int, allowEmpty bool, field string) error {
	if (!allowEmpty && value == "") || len(value) > maximum || !utf8.ValidString(value) {
		return fmt.Errorf("%w: %s is empty, invalid UTF-8, or too long", ErrInvalidWire, field)
	}
	return nil
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
	// RequestID 是同一 Stream 内关联 Publish 请求及响应的编号。
	RequestID RequestID
	// Channel 是精确匹配的普通 channel 名称。
	Channel string
	// ContentJSON 是完整 JSON 文本的原始 UTF-8 字节，不会被重新序列化。
	ContentJSON []byte
}

// Subscribe 是已验证的 Subscribe 文档。
type Subscribe struct {
	// RequestID 是同一 Stream 内关联订阅请求及响应的编号。
	RequestID RequestID
	// Channel 是要订阅的普通 channel 或全部 channel（"*"）。
	Channel string
}

// Unsubscribe 是已验证的 Unsubscribe 文档。
type Unsubscribe struct {
	// RequestID 是同一 Stream 内关联取消订阅请求及响应的编号。
	RequestID RequestID
	// Channel 是要取消的普通 channel 或全部订阅（"*"）。
	Channel string
}

// SubscriptionsRequest 是已验证的订阅列表查询文档。
type SubscriptionsRequest struct {
	// RequestID 是同一 Stream 内关联查询请求与列表响应的编号。
	RequestID RequestID
}

// SubscriptionsResponse 是服务端返回的订阅集合和本次实际扣费。
type SubscriptionsResponse struct {
	// RequestID 是原样回显的查询请求编号。
	RequestID RequestID
	// ChargedAmount 是本次查询实际扣费的规范十进制金额。
	ChargedAmount Amount
	// Channels 是无重复、按 UTF-8 字节升序排列的订阅集合。
	Channels []string
}

// ActionResult 是 Publish、Subscribe、Unsubscribe 及失败请求的统一结果。
type ActionResult struct {
	// RequestID 是原样回显的请求编号。
	RequestID RequestID
	// Success 表示动作是否成功。
	Success bool
	// ChargedAmount 是成功动作的实际扣费；失败固定为 "0"。
	ChargedAmount Amount
	// ErrorCode 成功固定为空；失败只能是三个 V1 稳定错误码之一。
	ErrorCode string
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

// IsKind 判断 Kind 是否属于 SSP V1 的八种固定消息类型。
func IsKind(kind Kind) bool {
	switch kind {
	case PublishKind, SubscribeKind, UnsubscribeKind, SubscriptionsRequestKind, SubscriptionsResponseKind, ActionResultKind, BillingRequestKind, BillingResponseKind:
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
