package service_test

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/bsv8/SatSubscriptionProtocol/client"
	"github.com/bsv8/SatSubscriptionProtocol/protocol"
	"github.com/bsv8/SatSubscriptionProtocol/service"
	"github.com/bsv8/SatSubscriptionProtocol/subscription"
)

func TestSessionReturnsActionResultsAndExactPublishEvent(t *testing.T) {
	key := fixedKey()
	backend := subscription.NewMemoryBackend()
	dispatcher := service.NewRecordingDispatcher()
	session := service.NewSession(key, backend, dispatcher)
	ctx := context.Background()

	subscribeID := requestID(1)
	subscribeWire, err := client.NewSubscribe(subscribeID, "news")
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.HandleMessage(ctx, subscribeWire)
	if err != nil || !result.HasResponse {
		t.Fatalf("Subscribe result = %#v, %v", result, err)
	}
	subscribeAction, err := client.ParseActionResult(result.Response)
	if err != nil || subscribeAction.RequestID != subscribeID || !subscribeAction.Success || subscribeAction.ChargedAmount != protocol.ZeroAmount || subscribeAction.ErrorCode != "" {
		t.Fatalf("Subscribe ActionResult = %#v, %v", subscribeAction, err)
	}

	queryID := requestID(2)
	queryWire, err := client.NewSubscriptionsRequest(queryID)
	if err != nil {
		t.Fatal(err)
	}
	result, err = session.HandleMessage(ctx, queryWire)
	if err != nil || !result.HasResponse {
		t.Fatalf("SubscriptionsRequest result = %#v, %v", result, err)
	}
	response, err := client.ParseSubscriptionsResponse(result.Response)
	if err != nil || response.RequestID != queryID || response.ChargedAmount != protocol.ZeroAmount || !sameStrings(response.Channels, []string{"news"}) {
		t.Fatalf("SubscriptionsResponse = %#v, %v", response, err)
	}

	publishID := requestID(3)
	content := []byte("  {\"n\":1} \n")
	publishWire, err := client.NewPublish(publishID, "news", content)
	if err != nil {
		t.Fatal(err)
	}
	result, err = session.HandleMessage(ctx, publishWire)
	if err != nil || !result.HasResponse {
		t.Fatalf("Publish result = %#v, %v", result, err)
	}
	publishAction, err := client.ParseActionResult(result.Response)
	if err != nil || publishAction.RequestID != publishID || !publishAction.Success {
		t.Fatalf("Publish ActionResult = %#v, %v", publishAction, err)
	}
	events := dispatcher.Events()
	if len(events) != 1 || events[0].RequestID != publishID || events[0].PublisherPublicKey != key || events[0].Channel != "news" || string(events[0].ContentJSON) != string(content) || string(events[0].Wire) != string(publishWire) {
		t.Fatalf("dispatcher event = %#v", events)
	}

	unsubscribeID := requestID(4)
	unsubscribeWire, err := client.NewUnsubscribe(unsubscribeID, "news")
	if err != nil {
		t.Fatal(err)
	}
	result, err = session.HandleMessage(ctx, unsubscribeWire)
	if err != nil || !result.HasResponse {
		t.Fatalf("Unsubscribe result = %#v, %v", result, err)
	}
	unsubscribeAction, err := client.ParseActionResult(result.Response)
	if err != nil || unsubscribeAction.RequestID != unsubscribeID || !unsubscribeAction.Success {
		t.Fatalf("Unsubscribe ActionResult = %#v, %v", unsubscribeAction, err)
	}

	events[0].ContentJSON[0] = 'x'
	if string(dispatcher.Events()[0].ContentJSON) != string(content) {
		t.Fatalf("RecordingDispatcher leaked mutable ContentJSON")
	}
}

func TestSessionSuppressesConcurrentDuplicateAndAllowsDifferentIDsToRunInParallel(t *testing.T) {
	key := fixedKey()
	backend := &countingBackend{}
	dispatcher := service.NewRecordingDispatcher()
	session := service.NewSession(key, backend, dispatcher)

	id := requestID(9)
	wireBytes, err := client.NewSubscribe(id, "news")
	if err != nil {
		t.Fatal(err)
	}
	const duplicates = 32
	results := make(chan service.Result, duplicates)
	errs := make(chan error, duplicates)
	var waitGroup sync.WaitGroup
	for index := 0; index < duplicates; index++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			result, handleErr := session.HandleMessage(context.Background(), wireBytes)
			results <- result
			errs <- handleErr
		}()
	}
	waitGroup.Wait()
	close(results)
	close(errs)
	responses := 0
	suppressed := 0
	for result := range results {
		if result.HasResponse {
			responses++
		}
		if result.Suppressed {
			suppressed++
		}
	}
	for handleErr := range errs {
		if handleErr != nil {
			t.Fatal(handleErr)
		}
	}
	if responses != 1 || suppressed != duplicates-1 || backend.subscribeCalls() != 1 {
		t.Fatalf("responses=%d suppressed=%d backend_calls=%d", responses, suppressed, backend.subscribeCalls())
	}

	firstID := requestID(10)
	secondID := requestID(11)
	firstWire, _ := client.NewSubscribe(firstID, "first")
	secondWire, _ := client.NewSubscribe(secondID, "second")
	firstResult, secondResult := make(chan service.Result, 1), make(chan service.Result, 1)
	go func() { result, _ := session.HandleMessage(context.Background(), firstWire); firstResult <- result }()
	go func() { result, _ := session.HandleMessage(context.Background(), secondWire); secondResult <- result }()
	if result := <-firstResult; !result.HasResponse {
		t.Fatal("first distinct request did not complete")
	}
	if result := <-secondResult; !result.HasResponse {
		t.Fatal("second distinct request did not complete")
	}
}

func TestSameRequestIDRunsAgainOnNewSession(t *testing.T) {
	backend := &countingBackend{}
	wireBytes, err := client.NewSubscribe(requestID(13), "news")
	if err != nil {
		t.Fatal(err)
	}
	firstSession := service.NewSession(fixedKey(), backend, service.NewRecordingDispatcher())
	firstResult, err := firstSession.HandleMessage(context.Background(), wireBytes)
	if err != nil || !firstResult.HasResponse || firstResult.Suppressed {
		t.Fatalf("first session result = %#v, %v", firstResult, err)
	}
	duplicate, err := firstSession.HandleMessage(context.Background(), wireBytes)
	if err != nil || !duplicate.Suppressed || duplicate.HasResponse {
		t.Fatalf("same-session duplicate result = %#v, %v", duplicate, err)
	}
	firstSession.Close()

	secondSession := service.NewSession(fixedKey(), backend, service.NewRecordingDispatcher())
	secondResult, err := secondSession.HandleMessage(context.Background(), wireBytes)
	if err != nil || !secondResult.HasResponse || secondResult.Suppressed {
		t.Fatalf("new session result = %#v, %v", secondResult, err)
	}
	if backend.subscribeCalls() != 2 {
		t.Fatalf("new session backend calls = %d, want 2", backend.subscribeCalls())
	}
}

func TestSessionDoesNotSerializeDifferentIDs(t *testing.T) {
	backend := &barrierBackend{started: make(chan struct{}, 2), release: make(chan struct{})}
	session := service.NewSession(fixedKey(), backend, service.NewRecordingDispatcher())
	firstWire, err := client.NewSubscribe(requestID(26), "first")
	if err != nil {
		t.Fatal(err)
	}
	secondWire, err := client.NewSubscribe(requestID(27), "second")
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	go func() {
		_, handleErr := session.HandleMessage(context.Background(), firstWire)
		results <- handleErr
	}()
	go func() {
		_, handleErr := session.HandleMessage(context.Background(), secondWire)
		results <- handleErr
	}()
	for index := 0; index < 2; index++ {
		select {
		case <-backend.started:
		case <-time.After(time.Second):
			close(backend.release)
			t.Fatal("different request IDs were serialized")
		}
	}
	close(backend.release)
	for index := 0; index < 2; index++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
}

func TestSessionReturnsActionResultForBusinessValidationFailure(t *testing.T) {
	key := fixedKey()
	session := service.NewSession(key, subscription.NewMemoryBackend(), service.NewRecordingDispatcher())
	id := requestID(12)
	wireBytes, err := client.NewPublish(id, "news", []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	// 保持长度和 canonical 外层不变，把 channel 的 CBOR text 类型改成 bytes。
	for index := 0; index+4 < len(wireBytes); index++ {
		if wireBytes[index] == 0x64 && string(wireBytes[index+1:index+5]) == "news" {
			wireBytes[index] = 0x44
			break
		}
	}
	result, err := session.HandleMessage(context.Background(), wireBytes)
	if err != nil || !result.HasResponse {
		t.Fatalf("invalid business request result = %#v, %v", result, err)
	}
	action, err := client.ParseActionResult(result.Response)
	if err != nil || action.RequestID != id || action.Success || action.ChargedAmount != protocol.ZeroAmount || action.ErrorCode != protocol.ActionErrorInvalidRequest {
		t.Fatalf("invalid request ActionResult = %#v, %v", action, err)
	}
}

func TestSessionClosedAndResponseCommandBoundaries(t *testing.T) {
	key := fixedKey()
	session := service.NewSession(key, subscription.NewMemoryBackend(), service.NewRecordingDispatcher())
	session.Close()
	_, err := session.HandleMessage(context.Background(), nil)
	if !errors.Is(err, protocol.ErrSessionClosed) {
		t.Fatalf("closed Session error = %v", err)
	}
}

func TestSessionPreservesBackendAndDispatcherCauses(t *testing.T) {
	key := fixedKey()
	backendCause := &backendTestError{message: "database unavailable"}
	backendSession := service.NewSession(key, &failingBackend{err: backendCause}, service.NewRecordingDispatcher())
	backendWire, err := client.NewSubscribe(requestID(20), "news")
	if err != nil {
		t.Fatal(err)
	}
	backendResult, backendErr := backendSession.HandleMessage(context.Background(), backendWire)
	if backendErr != nil {
		t.Fatalf("business failure returned transport error: %v", backendErr)
	}
	if !errors.Is(backendResult.Cause, backendCause) || !errors.Is(backendResult.Cause, protocol.ErrBackendFailure) {
		t.Fatalf("result backend cause = %v", backendResult.Cause)
	}
	var extractedBackendCause *backendTestError
	if !errors.As(backendResult.Cause, &extractedBackendCause) || extractedBackendCause != backendCause {
		t.Fatalf("backend errors.As cause = %#v", extractedBackendCause)
	}
	backendAction, err := client.ParseActionResult(backendResult.Response)
	if err != nil || backendAction.ErrorCode != protocol.ActionErrorInternalError {
		t.Fatalf("backend ActionResult = %#v, %v", backendAction, err)
	}

	dispatchCause := fmt.Errorf("balance: %w", protocol.ErrRejected)
	dispatchSession := service.NewSession(key, subscription.NewMemoryBackend(), &failingDispatcher{err: dispatchCause})
	dispatchWire, err := client.NewPublish(requestID(21), "news", []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	dispatchResult, dispatchErr := dispatchSession.HandleMessage(context.Background(), dispatchWire)
	if dispatchErr != nil {
		t.Fatalf("business failure returned transport error: %v", dispatchErr)
	}
	if !errors.Is(dispatchResult.Cause, dispatchCause) || !errors.Is(dispatchResult.Cause, protocol.ErrDispatchFailure) {
		t.Fatalf("result dispatcher cause = %v", dispatchResult.Cause)
	}
	dispatchAction, err := client.ParseActionResult(dispatchResult.Response)
	if err != nil || dispatchAction.ErrorCode != protocol.ActionErrorRejected {
		t.Fatalf("dispatcher ActionResult = %#v, %v", dispatchAction, err)
	}
}

func TestSessionClassifiesInvalidBackendDataAsBackendFailure(t *testing.T) {
	key := fixedKey()
	session := service.NewSession(key, &invalidSubscriptionsBackend{}, service.NewRecordingDispatcher())
	queryWire, err := client.NewSubscriptionsRequest(requestID(26))
	if err != nil {
		t.Fatal(err)
	}
	result, handleErr := session.HandleMessage(context.Background(), queryWire)
	if handleErr != nil || !result.HasResponse {
		t.Fatalf("invalid backend data crossed transport boundary: result=%#v error=%v", result, handleErr)
	}
	if protocol.ErrorCodeOf(result.Cause) != protocol.CodeBackendFailure || !errors.Is(result.Cause, protocol.ErrInvalidSubscriptions) {
		t.Fatalf("backend cause classification = %q, cause=%v", protocol.ErrorCodeOf(result.Cause), result.Cause)
	}
	action, err := client.ParseActionResult(result.Response)
	if err != nil || action.ErrorCode != protocol.ActionErrorInternalError || action.ChargedAmount != protocol.ZeroAmount {
		t.Fatalf("invalid backend data ActionResult = %#v, %v", action, err)
	}
}

func TestHandleMessageKeepsBusinessResponseAndCauseSeparate(t *testing.T) {
	key := fixedKey()
	backendCause := &backendTestError{message: "database unavailable"}
	requestWire, err := client.NewSubscribe(requestID(28), "news")
	if err != nil {
		t.Fatal(err)
	}
	result, handleErr := service.HandleMessage(context.Background(), key, requestWire, &failingBackend{err: backendCause}, service.NewRecordingDispatcher())
	if handleErr != nil || !result.HasResponse || result.Cause == nil {
		t.Fatalf("one-shot business failure crossed transport boundary: result=%#v error=%v", result, handleErr)
	}
	if !errors.Is(result.Cause, backendCause) || !errors.Is(result.Cause, protocol.ErrBackendFailure) {
		t.Fatalf("one-shot cause = %v", result.Cause)
	}
	if action, parseErr := client.ParseActionResult(result.Response); parseErr != nil || action.ErrorCode != protocol.ActionErrorInternalError {
		t.Fatalf("one-shot ActionResult = %#v, %v", action, parseErr)
	}
}

func TestSessionReturnsActualChargedAmounts(t *testing.T) {
	key := fixedKey()
	session := service.NewSession(key, &pricedBackend{}, &pricedDispatcher{})

	subscribeWire, err := client.NewSubscribe(requestID(22), "news")
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.HandleMessage(context.Background(), subscribeWire)
	if err != nil {
		t.Fatal(err)
	}
	action, err := client.ParseActionResult(result.Response)
	if err != nil || action.ChargedAmount != protocol.Amount("0.001") {
		t.Fatalf("Subscribe amount = %#v, %v", action, err)
	}

	unsubscribeWire, err := client.NewUnsubscribe(requestID(23), "news")
	if err != nil {
		t.Fatal(err)
	}
	result, err = session.HandleMessage(context.Background(), unsubscribeWire)
	if err != nil {
		t.Fatal(err)
	}
	action, err = client.ParseActionResult(result.Response)
	if err != nil || action.ChargedAmount != protocol.Amount("12.34") {
		t.Fatalf("Unsubscribe amount = %#v, %v", action, err)
	}

	queryWire, err := client.NewSubscriptionsRequest(requestID(24))
	if err != nil {
		t.Fatal(err)
	}
	result, err = session.HandleMessage(context.Background(), queryWire)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.ParseSubscriptionsResponse(result.Response)
	if err != nil || response.ChargedAmount != protocol.Amount("0.123456789012345678") {
		t.Fatalf("Subscriptions amount = %#v, %v", response, err)
	}

	publishWire, err := client.NewPublish(requestID(25), "news", []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	result, err = session.HandleMessage(context.Background(), publishWire)
	if err != nil {
		t.Fatal(err)
	}
	action, err = client.ParseActionResult(result.Response)
	if err != nil || action.ChargedAmount != protocol.Amount("1.2") {
		t.Fatalf("Publish amount = %#v, %v", action, err)
	}
}

func TestSameActionWithDifferentRequestIDsChargesAgain(t *testing.T) {
	backend := &chargeCountingBackend{}
	session := service.NewSession(fixedKey(), backend, service.NewRecordingDispatcher())

	for value := byte(1); value <= 2; value++ {
		wireBytes, err := client.NewSubscribe(requestID(value), "news")
		if err != nil {
			t.Fatal(err)
		}
		result, handleErr := session.HandleMessage(context.Background(), wireBytes)
		if handleErr != nil || !result.HasResponse {
			t.Fatalf("Subscribe %d result = %#v, %v", value, result, handleErr)
		}
		action, parseErr := client.ParseActionResult(result.Response)
		if parseErr != nil || action.ChargedAmount != protocol.Amount("0.001") {
			t.Fatalf("Subscribe %d amount = %#v, %v", value, action, parseErr)
		}
	}
	for value := byte(3); value <= 4; value++ {
		wireBytes, err := client.NewUnsubscribe(requestID(value), "news")
		if err != nil {
			t.Fatal(err)
		}
		result, handleErr := session.HandleMessage(context.Background(), wireBytes)
		if handleErr != nil || !result.HasResponse {
			t.Fatalf("Unsubscribe %d result = %#v, %v", value, result, handleErr)
		}
		action, parseErr := client.ParseActionResult(result.Response)
		if parseErr != nil || action.ChargedAmount != protocol.Amount("0.001") {
			t.Fatalf("Unsubscribe %d amount = %#v, %v", value, action, parseErr)
		}
	}
	if backend.subscribeCalls != 2 || backend.unsubscribeCalls != 2 {
		t.Fatalf("same action calls = subscribe:%d unsubscribe:%d, want 2/2", backend.subscribeCalls, backend.unsubscribeCalls)
	}
}

func TestSessionRequestIDLimit(t *testing.T) {
	key := fixedKey()
	session := service.NewSession(key, subscription.NewMemoryBackend(), service.NewRecordingDispatcher())
	var lastWire []byte
	for value := uint32(0); value < protocol.MaxRequestIDsPerSession; value++ {
		wireBytes, err := client.NewSubscriptionsRequest(numberedRequestID(value))
		if err != nil {
			t.Fatal(err)
		}
		lastWire = wireBytes
		result, handleErr := session.HandleMessage(context.Background(), wireBytes)
		if handleErr != nil || !result.HasResponse || result.Suppressed {
			t.Fatalf("request %d result = %#v, %v", value, result, handleErr)
		}
	}
	newWire, err := client.NewSubscriptionsRequest(numberedRequestID(protocol.MaxRequestIDsPerSession))
	if err != nil {
		t.Fatal(err)
	}
	result, handleErr := session.HandleMessage(context.Background(), newWire)
	if !errors.Is(handleErr, protocol.ErrSessionLimit) || result.HasResponse || result.Suppressed {
		t.Fatalf("request over limit result = %#v, %v", result, handleErr)
	}
	duplicate, handleErr := session.HandleMessage(context.Background(), lastWire)
	if handleErr != nil || !duplicate.Suppressed || duplicate.HasResponse {
		t.Fatalf("duplicate at limit result = %#v, %v", duplicate, handleErr)
	}
}

type countingBackend struct {
	mu       sync.Mutex
	calls    int
	delegate *subscription.MemoryBackend
}

type failingBackend struct {
	err error
}

type backendTestError struct {
	message string
}

func (err *backendTestError) Error() string {
	return err.message
}

type invalidSubscriptionsBackend struct{}

func (backend *invalidSubscriptionsBackend) Subscribe(context.Context, protocol.PublicKey, protocol.RequestID, string) (protocol.Amount, error) {
	return protocol.ZeroAmount, nil
}

func (backend *invalidSubscriptionsBackend) Unsubscribe(context.Context, protocol.PublicKey, protocol.RequestID, string) (protocol.Amount, error) {
	return protocol.ZeroAmount, nil
}

func (backend *invalidSubscriptionsBackend) Subscriptions(context.Context, protocol.PublicKey, protocol.RequestID) ([]string, protocol.Amount, error) {
	return []string{"news", "news"}, protocol.ZeroAmount, nil
}

type chargeCountingBackend struct {
	subscribeCalls   int
	unsubscribeCalls int
}

func (backend *chargeCountingBackend) Subscribe(context.Context, protocol.PublicKey, protocol.RequestID, string) (protocol.Amount, error) {
	backend.subscribeCalls++
	return protocol.Amount("0.001"), nil
}

func (backend *chargeCountingBackend) Unsubscribe(context.Context, protocol.PublicKey, protocol.RequestID, string) (protocol.Amount, error) {
	backend.unsubscribeCalls++
	return protocol.Amount("0.001"), nil
}

func (backend *chargeCountingBackend) Subscriptions(context.Context, protocol.PublicKey, protocol.RequestID) ([]string, protocol.Amount, error) {
	return []string{}, protocol.ZeroAmount, nil
}

func (backend *failingBackend) Subscribe(context.Context, protocol.PublicKey, protocol.RequestID, string) (protocol.Amount, error) {
	return "", backend.err
}

func (backend *failingBackend) Unsubscribe(context.Context, protocol.PublicKey, protocol.RequestID, string) (protocol.Amount, error) {
	return "", backend.err
}

func (backend *failingBackend) Subscriptions(context.Context, protocol.PublicKey, protocol.RequestID) ([]string, protocol.Amount, error) {
	return nil, "", backend.err
}

type failingDispatcher struct {
	err error
}

func (dispatcher *failingDispatcher) Dispatch(context.Context, service.PublishEvent) (protocol.Amount, error) {
	return "", dispatcher.err
}

type pricedBackend struct{}

func (backend *pricedBackend) Subscribe(context.Context, protocol.PublicKey, protocol.RequestID, string) (protocol.Amount, error) {
	return "0.001", nil
}

func (backend *pricedBackend) Unsubscribe(context.Context, protocol.PublicKey, protocol.RequestID, string) (protocol.Amount, error) {
	return "12.34", nil
}

func (backend *pricedBackend) Subscriptions(context.Context, protocol.PublicKey, protocol.RequestID) ([]string, protocol.Amount, error) {
	return []string{"news"}, "0.123456789012345678", nil
}

type pricedDispatcher struct{}

func (dispatcher *pricedDispatcher) Dispatch(context.Context, service.PublishEvent) (protocol.Amount, error) {
	return "1.2", nil
}

type barrierBackend struct {
	started chan struct{}
	release chan struct{}
}

func (backend *barrierBackend) Subscribe(context.Context, protocol.PublicKey, protocol.RequestID, string) (protocol.Amount, error) {
	backend.started <- struct{}{}
	<-backend.release
	return protocol.ZeroAmount, nil
}

func (backend *barrierBackend) Unsubscribe(context.Context, protocol.PublicKey, protocol.RequestID, string) (protocol.Amount, error) {
	return protocol.ZeroAmount, nil
}

func (backend *barrierBackend) Subscriptions(context.Context, protocol.PublicKey, protocol.RequestID) ([]string, protocol.Amount, error) {
	return []string{}, protocol.ZeroAmount, nil
}

func (backend *countingBackend) Subscribe(ctx context.Context, key protocol.PublicKey, id protocol.RequestID, channel string) (protocol.Amount, error) {
	backend.mu.Lock()
	backend.calls++
	if backend.delegate == nil {
		backend.delegate = subscription.NewMemoryBackend()
	}
	delegate := backend.delegate
	backend.mu.Unlock()
	return delegate.Subscribe(ctx, key, id, channel)
}

func (backend *countingBackend) Unsubscribe(ctx context.Context, key protocol.PublicKey, id protocol.RequestID, channel string) (protocol.Amount, error) {
	if backend.delegate == nil {
		backend.delegate = subscription.NewMemoryBackend()
	}
	return backend.delegate.Unsubscribe(ctx, key, id, channel)
}

func (backend *countingBackend) Subscriptions(ctx context.Context, key protocol.PublicKey, id protocol.RequestID) ([]string, protocol.Amount, error) {
	if backend.delegate == nil {
		backend.delegate = subscription.NewMemoryBackend()
	}
	return backend.delegate.Subscriptions(ctx, key, id)
}

func (backend *countingBackend) subscribeCalls() int {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return backend.calls
}

func fixedKey() protocol.PublicKey {
	var key protocol.PublicKey
	key[0] = 0x02
	return key
}

func requestID(value byte) protocol.RequestID {
	var id protocol.RequestID
	for index := range id {
		id[index] = value
	}
	return id
}

func numberedRequestID(value uint32) protocol.RequestID {
	var id protocol.RequestID
	binary.BigEndian.PutUint32(id[len(id)-4:], value)
	return id
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
