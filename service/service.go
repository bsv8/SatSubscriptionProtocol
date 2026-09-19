// Package service 将严格 Wire 解析、订阅 Backend 和广播 Dispatcher 组合成无网络的服务入口。
package service

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/bsv8/SatSubscriptionProtocol/protocol"
	"github.com/bsv8/SatSubscriptionProtocol/subscription"
	"github.com/bsv8/SatSubscriptionProtocol/wire"
)

// Session 使用 subscription.Backend 按已认证 remote_public_key 持久化订阅并执行实际
// 扣费。每次方法调用都必须由外部实现保证检查、扣费和状态读取/写入的原子事务边界。
// Session 只在当前 Stream 内抑制 request_id；跨 Stream 的收费幂等由具体
// Dispatcher/Backend 持久化实现，键为 (已认证 payer_id, request_id, deterministic
// SSP Wire 的 SHA-256 request_digest)，并在完全相同重试时返回已保存的原始结果。

// PublishEvent 是交给外部 Dispatcher 的精简 Publish 事件。
type PublishEvent struct {
	// RequestID 是发布请求编号，也是具体计费实现跨 Stream 幂等的第二部分；
	// 订阅者不应把它当作广播消息 ID。
	RequestID protocol.RequestID
	// PayerID 是外层已认证连接提供的付款人身份，不来自 SSP payload。
	PayerID protocol.PublicKey
	// Channel 是 Publish 的普通 channel。
	Channel string
	// ContentJSON 是 Publish 的 JSON exact bytes 防御性副本。
	ContentJSON []byte
}

// Dispatcher 是窄广播回调接口，不包含任何网络或连接类型。
// 成功返回值必须是服务端已经实际扣除的金额。
type Dispatcher interface {
	// Dispatch 接收一个已认证付款人和精简 Publish 事件，并返回实际扣费金额。
	// 生产实现必须按 (event.PayerID, event.RequestID, deterministic Wire 摘要)
	// 持久化 Publish 收费幂等，不能把 Session 或 Stream 生命周期当作持久化幂等边界。
	Dispatch(ctx context.Context, event PublishEvent) (chargedAmount protocol.Amount, err error)
}

// Result 是一次 Session 请求的处理结果。
type Result struct {
	// Response 是需要外部应用写回原 Stream 的完整 SSP Wire。
	Response []byte
	// HasResponse 区分是否存在响应；false 时不得写入空报文。
	HasResponse bool
	// Suppressed 表示相同 request_id 已在本 Session 登记，本次被静默丢弃。
	Suppressed bool
	// Cause 是本地诊断用的稳定包装错误，不会进入 Response Wire。
	// 业务失败仍然通过 ActionResult 返回；调用方可对 Cause 使用 errors.Is/As，
	// 同时找到 BACKEND_FAILURE/DISPATCH_FAILURE 和 Backend/Dispatcher 原始错误。
	Cause error
}

// Session 表示一个 Stream 的 request_id 登记范围。
// Session 只保护编号登记，不串行化不同 request_id 的 Backend/Dispatcher 调用。
type Session struct {
	remotePublicKey protocol.PublicKey
	backend         subscription.Backend
	dispatcher      Dispatcher

	mu     sync.Mutex
	seen   map[protocol.RequestID]struct{}
	closed bool
}

// NewSession 创建一个与单条 Stream 绑定的无网络 Session。
// remotePublicKey 必须来自外层已认证连接；公钥形状错误会在 HandleMessage 时返回本地错误。
func NewSession(remotePublicKey protocol.PublicKey, backend subscription.Backend, dispatcher Dispatcher) *Session {
	return &Session{
		remotePublicKey: remotePublicKey,
		backend:         backend,
		dispatcher:      dispatcher,
		seen:            make(map[protocol.RequestID]struct{}),
	}
}

// Close 关闭 Session 并释放本 Stream 的 request_id 登记集合。
// 已经开始执行的业务调用不会被强行取消；之后的新调用返回 SESSION_CLOSED。
func (session *Session) Close() {
	if session == nil {
		return
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	session.closed = true
	session.seen = nil
}

// HandleMessage 处理一个来自已认证长期连接的完整 SSP Wire。
// 不同 request_id 可并行执行；响应必须由外部应用通过单独 writer 串行写出。
func (session *Session) HandleMessage(ctx context.Context, wireCBOR []byte) (Result, error) {
	if session == nil {
		return Result{}, fmt.Errorf("%w: nil Session", protocol.ErrSessionClosed)
	}
	if !session.remotePublicKey.Valid() {
		return Result{}, fmt.Errorf("%w: remote public key is not a compressed key", protocol.ErrInvalidPublicKey)
	}

	// 先快速检查关闭状态，避免已关闭 Session 对格式错误输入返回错误分类不一致的结果。
	session.mu.Lock()
	if session.closed {
		session.mu.Unlock()
		return Result{}, fmt.Errorf("%w: Session is closed", protocol.ErrSessionClosed)
	}
	session.mu.Unlock()

	// 这里只解析一次外层，并读取首字段 request_id；后续业务字段由 DecodeRequest 校验。
	envelope, err := wire.ParseRequestEnvelope(wireCBOR)
	if err != nil {
		return Result{}, err
	}

	session.mu.Lock()
	if session.closed {
		session.mu.Unlock()
		return Result{}, fmt.Errorf("%w: Session is closed", protocol.ErrSessionClosed)
	}
	if _, exists := session.seen[envelope.RequestID]; exists {
		session.mu.Unlock()
		return Result{Suppressed: true}, nil
	}
	if len(session.seen) >= protocol.MaxRequestIDsPerSession {
		session.mu.Unlock()
		return Result{}, fmt.Errorf("%w: request_id limit reached", protocol.ErrSessionLimit)
	}
	// 登记必须发生在任何 Backend/Dispatcher 调用之前；锁只覆盖这一小段。
	session.seen[envelope.RequestID] = struct{}{}
	session.mu.Unlock()

	request, err := wire.DecodeRequest(envelope)
	if err != nil {
		if envelope.Message.Kind == protocol.BillingRequestKind {
			return session.billingFailure(envelope.RequestID, protocol.ActionErrorInvalidRequest)
		}
		return session.actionFailure(envelope.RequestID, protocol.ActionErrorInvalidRequest)
	}

	switch request.Kind {
	case protocol.SubscribeKind:
		return session.handleSubscribe(ctx, request.Subscribe)
	case protocol.UnsubscribeKind:
		return session.handleUnsubscribe(ctx, request.Unsubscribe)
	case protocol.SubscriptionsRequestKind:
		return session.handleSubscriptions(ctx, request.SubscriptionsRequest)
	case protocol.PublishKind:
		return session.handlePublish(ctx, request.Publish)
	case protocol.BillingRequestKind:
		return session.handleBilling(ctx, request.BillingRequest)
	default:
		return Result{}, fmt.Errorf("%w: unsupported client request kind", protocol.ErrUnsupportedKind)
	}
}

func (session *Session) handleSubscribe(ctx context.Context, request protocol.Subscribe) (Result, error) {
	if session.backend == nil {
		return session.actionFailureWithCause(request.RequestID, protocol.ActionErrorInternalError,
			wrapBackendFailure("Subscribe", errors.New("subscription backend is required")))
	}
	amount, err := session.backend.Subscribe(ctx, session.remotePublicKey, request.RequestID, request.Channel)
	if err != nil {
		return session.actionFailureWithCause(request.RequestID, actionErrorCode(err), wrapBackendFailure("Subscribe", err))
	}
	if amountErr := protocol.ValidateAmount(amount.String()); amountErr != nil {
		return session.actionFailureWithCause(request.RequestID, protocol.ActionErrorInternalError,
			wrapBackendFailure("Subscribe returned invalid charged_amount", amountErr))
	}
	return session.actionSuccess(request.RequestID, amount)
}

func (session *Session) handleUnsubscribe(ctx context.Context, request protocol.Unsubscribe) (Result, error) {
	if session.backend == nil {
		return session.actionFailureWithCause(request.RequestID, protocol.ActionErrorInternalError,
			wrapBackendFailure("Unsubscribe", errors.New("subscription backend is required")))
	}
	amount, err := session.backend.Unsubscribe(ctx, session.remotePublicKey, request.RequestID, request.Channel)
	if err != nil {
		return session.actionFailureWithCause(request.RequestID, actionErrorCode(err), wrapBackendFailure("Unsubscribe", err))
	}
	if amountErr := protocol.ValidateAmount(amount.String()); amountErr != nil {
		return session.actionFailureWithCause(request.RequestID, protocol.ActionErrorInternalError,
			wrapBackendFailure("Unsubscribe returned invalid charged_amount", amountErr))
	}
	return session.actionSuccess(request.RequestID, amount)
}

func (session *Session) handleSubscriptions(ctx context.Context, request protocol.SubscriptionsRequest) (Result, error) {
	if session.backend == nil {
		return session.actionFailureWithCause(request.RequestID, protocol.ActionErrorInternalError,
			wrapBackendFailure("Subscriptions", errors.New("subscription backend is required")))
	}
	channels, amount, err := session.backend.Subscriptions(ctx, session.remotePublicKey, request.RequestID)
	if err != nil {
		return session.actionFailureWithCause(request.RequestID, actionErrorCode(err), wrapBackendFailure("Subscriptions", err))
	}
	normalized, normalizeErr := subscription.NormalizeSubscriptions(channels)
	if normalizeErr != nil {
		// Backend 返回非法集合是服务端依赖失败，不伪装成客户端 INVALID_REQUEST。
		return session.actionFailureWithCause(request.RequestID, protocol.ActionErrorInternalError,
			wrapBackendFailure("Subscriptions returned invalid channels", normalizeErr))
	}
	if amountErr := protocol.ValidateAmount(amount.String()); amountErr != nil {
		return session.actionFailureWithCause(request.RequestID, protocol.ActionErrorInternalError,
			wrapBackendFailure("Subscriptions returned invalid charged_amount", amountErr))
	}
	response := protocol.SubscriptionsResponse{
		RequestID:     request.RequestID,
		ChargedAmount: amount,
		Channels:      normalized,
	}
	document, encodeErr := wire.EncodeSubscriptionsResponseDocument(response)
	if encodeErr != nil {
		return session.actionFailureWithCause(request.RequestID, protocol.ActionErrorInternalError,
			wrapBackendFailure("Subscriptions response encoding", encodeErr))
	}
	encoded, encodeErr := wire.EncodeWire(protocol.SubscriptionsResponseKind, document)
	if encodeErr != nil {
		return session.actionFailureWithCause(request.RequestID, protocol.ActionErrorInternalError,
			wrapBackendFailure("Subscriptions response Wire encoding", encodeErr))
	}
	return Result{Response: protocol.CloneBytes(encoded), HasResponse: true}, nil
}

func (session *Session) handlePublish(ctx context.Context, publish protocol.Publish) (Result, error) {
	if session.dispatcher == nil {
		return session.actionFailureWithCause(publish.RequestID, protocol.ActionErrorInternalError,
			wrapDispatchFailure("Publish", errors.New("publish dispatcher is required")))
	}
	event := PublishEvent{
		RequestID:   publish.RequestID,
		PayerID:     session.remotePublicKey,
		Channel:     publish.Channel,
		ContentJSON: protocol.CloneBytes(publish.ContentJSON),
	}
	amount, err := session.dispatcher.Dispatch(ctx, event)
	if err != nil {
		return session.actionFailureWithCause(publish.RequestID, actionErrorCode(err), wrapDispatchFailure("Publish", err))
	}
	if amountErr := protocol.ValidateAmount(amount.String()); amountErr != nil {
		return session.actionFailureWithCause(publish.RequestID, protocol.ActionErrorInternalError,
			wrapDispatchFailure("Publish returned invalid charged_amount", amountErr))
	}
	return session.actionSuccess(publish.RequestID, amount)
}

func (session *Session) handleBilling(ctx context.Context, request protocol.BillingRequest) (Result, error) {
	if session.backend == nil {
		return session.billingFailureWithCause(request.RequestID, protocol.ActionErrorInternalError,
			wrapBackendFailure("ListChargeRecords", errors.New("billing backend is required")))
	}
	page, err := session.backend.ListChargeRecords(ctx, session.remotePublicKey, request)
	if err != nil {
		return session.billingFailureWithCause(request.RequestID, actionErrorCode(err), wrapBackendFailure("ListChargeRecords", err))
	}
	if validationErr := protocol.ValidateBillingPage(page); validationErr != nil {
		return session.billingFailureWithCause(request.RequestID, protocol.ActionErrorInternalError,
			wrapBackendFailure("ListChargeRecords returned invalid page", validationErr))
	}
	if uint64(len(page.Records)) > request.Limit {
		return session.billingFailureWithCause(request.RequestID, protocol.ActionErrorInternalError,
			wrapBackendFailure("ListChargeRecords returned too many records", errors.New("records exceed requested limit")))
	}
	response := protocol.BillingResponse{
		RequestID:  request.RequestID,
		Success:    true,
		Currency:   page.Currency,
		Network:    page.Network,
		Records:    append([]protocol.ChargeRecord(nil), page.Records...),
		NextCursor: page.NextCursor,
		ErrorCode:  "",
	}
	return session.encodeBillingResponse(response)
}

func (session *Session) actionSuccess(requestID protocol.RequestID, amount protocol.Amount) (Result, error) {
	return session.encodeActionResult(protocol.ActionResult{
		RequestID:     requestID,
		Success:       true,
		ChargedAmount: amount,
		ErrorCode:     "",
	})
}

func (session *Session) actionFailure(requestID protocol.RequestID, errorCode string) (Result, error) {
	return session.actionFailureWithCause(requestID, errorCode, nil)
}

func (session *Session) actionFailureWithCause(requestID protocol.RequestID, errorCode string, cause error) (Result, error) {
	result, err := session.encodeActionResult(protocol.ActionResult{
		RequestID:     requestID,
		Success:       false,
		ChargedAmount: protocol.ZeroAmount,
		ErrorCode:     errorCode,
	})
	if err != nil {
		return Result{}, err
	}
	result.Cause = cause
	// ActionResult 已经可以安全写回；本地 cause 只放在 Result 中，不能让
	// 连接层因非空 error 跳过响应或关闭整个 Stream。
	return result, nil
}

func (session *Session) billingFailure(requestID protocol.RequestID, errorCode string) (Result, error) {
	return session.billingFailureWithCause(requestID, errorCode, nil)
}

func (session *Session) billingFailureWithCause(requestID protocol.RequestID, errorCode string, cause error) (Result, error) {
	return session.encodeBillingResponseWithCause(protocol.BillingResponse{
		RequestID: requestID,
		Success:   false,
		Records:   []protocol.ChargeRecord{},
		ErrorCode: errorCode,
	}, cause)
}

func (session *Session) encodeActionResult(action protocol.ActionResult) (Result, error) {
	document, err := wire.EncodeActionResultDocument(action)
	if err != nil {
		return Result{}, err
	}
	encoded, err := wire.EncodeWire(protocol.ActionResultKind, document)
	if err != nil {
		return Result{}, err
	}
	return Result{Response: protocol.CloneBytes(encoded), HasResponse: true}, nil
}

func (session *Session) encodeBillingResponse(response protocol.BillingResponse) (Result, error) {
	return session.encodeBillingResponseWithCause(response, nil)
}

func (session *Session) encodeBillingResponseWithCause(response protocol.BillingResponse, cause error) (Result, error) {
	document, err := wire.EncodeBillingResponseDocument(response)
	if err != nil {
		return Result{}, err
	}
	encoded, err := wire.EncodeWire(protocol.BillingResponseKind, document)
	if err != nil {
		return Result{}, err
	}
	return Result{Response: protocol.CloneBytes(encoded), HasResponse: true, Cause: cause}, nil
}

func actionErrorCode(err error) string {
	if errors.Is(err, protocol.ErrRejected) {
		return protocol.ActionErrorRejected
	}
	return protocol.ActionErrorInternalError
}

// wrapBackendFailure 保留 Backend 原始错误，同时提供稳定的 BACKEND_FAILURE 分类。
func wrapBackendFailure(operation string, cause error) error {
	if cause == nil {
		cause = errors.New(operation + " failed")
	}
	return fmt.Errorf("%w: %s: %w", protocol.ErrBackendFailure, operation, cause)
}

// wrapDispatchFailure 保留 Dispatcher 原始错误，同时提供稳定的 DISPATCH_FAILURE 分类。
func wrapDispatchFailure(operation string, cause error) error {
	if cause == nil {
		cause = errors.New(operation + " failed")
	}
	return fmt.Errorf("%w: %s: %w", protocol.ErrDispatchFailure, operation, cause)
}

// RecordingDispatcher 是只记录事件并返回免费金额的线程安全 Dispatcher，仅用于测试和示例。
type RecordingDispatcher struct {
	mu     sync.Mutex
	events []PublishEvent
}

// NewRecordingDispatcher 创建空的 RecordingDispatcher。
func NewRecordingDispatcher() *RecordingDispatcher {
	return &RecordingDispatcher{}
}

// Dispatch 记录事件并复制所有可变字节切片；示例实现不收费。
func (dispatcher *RecordingDispatcher) Dispatch(_ context.Context, event PublishEvent) (protocol.Amount, error) {
	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	event.ContentJSON = protocol.CloneBytes(event.ContentJSON)
	dispatcher.events = append(dispatcher.events, event)
	return protocol.ZeroAmount, nil
}

// Events 返回已记录事件的深度副本。
func (dispatcher *RecordingDispatcher) Events() []PublishEvent {
	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	result := make([]PublishEvent, len(dispatcher.events))
	for index, event := range dispatcher.events {
		result[index] = event
		result[index].ContentJSON = protocol.CloneBytes(event.ContentJSON)
	}
	return result
}

// Reset 清空已记录事件。
func (dispatcher *RecordingDispatcher) Reset() {
	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	dispatcher.events = nil
}
