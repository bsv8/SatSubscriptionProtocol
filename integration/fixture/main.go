// Command fixture 通过 JSON 文件交换完整 Wire，执行不依赖网络的跨语言一致性检查。
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/bsv8/SatSubscriptionProtocol/client"
	"github.com/bsv8/SatSubscriptionProtocol/protocol"
	"github.com/bsv8/SatSubscriptionProtocol/service"
	"github.com/bsv8/SatSubscriptionProtocol/subscription"
	"github.com/bsv8/SatSubscriptionProtocol/wire"
)

type wireFixture struct {
	Cases []wireCase `json:"cases"`
}

type wireCase struct {
	Name           string             `json:"name"`
	Kind           uint64             `json:"kind"`
	RequestIDHex   string             `json:"request_id_hex"`
	Channel        string             `json:"channel"`
	ContentJSONHex string             `json:"content_json_hex"`
	ChargedAmount  string             `json:"charged_amount"`
	Channels       []string           `json:"channels"`
	Success        bool               `json:"success"`
	ErrorCode      string             `json:"error_code"`
	FromMS         uint64             `json:"from_ms"`
	ToMS           uint64             `json:"to_ms"`
	Limit          uint64             `json:"limit"`
	Cursor         string             `json:"cursor"`
	Currency       string             `json:"currency"`
	Network        string             `json:"network"`
	Records        []chargeRecordCase `json:"records"`
	NextCursor     string             `json:"next_cursor"`
}

type chargeRecordCase struct {
	ChargeID           string `json:"charge_id"`
	OccurredAtMS       uint64 `json:"occurred_at_ms"`
	Action             string `json:"action"`
	Channel            string `json:"channel"`
	SourceRequestIDHex string `json:"source_request_id_hex"`
	ChargedAmount      string `json:"charged_amount"`
}

type exchangeCase struct {
	Name    string `json:"name"`
	WireHex string `json:"wire_hex"`
}

type subscriptionsFixture struct {
	Normalization []normalizationCase `json:"normalization"`
	Transitions   []transitionCase    `json:"transitions"`
}

type normalizationCase struct {
	Name     string   `json:"name"`
	Input    []string `json:"input"`
	Expected []string `json:"expected"`
}

type transitionCase struct {
	Name      string   `json:"name"`
	Initial   []string `json:"initial"`
	Operation string   `json:"operation"`
	Channel   string   `json:"channel"`
	Expected  []string `json:"expected"`
}

type stateExchange struct {
	Normalization []stateResult `json:"normalization"`
	Transitions   []stateResult `json:"transitions"`
}

type stateResult struct {
	Name   string   `json:"name"`
	Result []string `json:"result"`
}

type invalidFixture struct {
	Cases []invalidCase `json:"cases"`
}

type invalidCase struct {
	Name         string `json:"name"`
	WireHex      string `json:"wire_hex"`
	ExpectedCode string `json:"expected_code"`
}

type invalidExchange struct {
	Name string `json:"name"`
	Code string `json:"code"`
}

type dispatchFixture struct {
	MatchCases            []matchCase           `json:"match_cases"`
	DuplicateIdentityCase duplicateIdentityCase `json:"duplicate_identity_case"`
	PublishEvent          publishEventFixture   `json:"publish_event"`
}

type matchCase struct {
	Name           string   `json:"name"`
	Subscriptions  []string `json:"subscriptions"`
	PublishChannel string   `json:"publish_channel"`
	Expected       bool     `json:"expected"`
}

type duplicateIdentityCase struct {
	PublishChannel                 string           `json:"publish_channel"`
	OnlineIdentities               []onlineIdentity `json:"online_identities"`
	ExpectedUniqueMatchingIdentity []string         `json:"expected_unique_matching_identity_hex"`
}

type onlineIdentity struct {
	RemotePublicKeyHex string   `json:"remote_public_key_hex"`
	Subscriptions      []string `json:"subscriptions"`
}

type publishEventFixture struct {
	PayerIDHex     string `json:"payer_id_hex"`
	Channel        string `json:"channel"`
	ContentJSONHex string `json:"content_json_hex"`
	WireHex        string `json:"wire_hex"`
}

type eventExchange struct {
	Response    string          `json:"response_hex"`
	HasResponse bool            `json:"has_response"`
	Events      []eventSnapshot `json:"events"`
}

type eventSnapshot struct {
	RequestIDHex   string `json:"request_id_hex"`
	PayerIDHex     string `json:"payer_id_hex"`
	Channel        string `json:"channel"`
	ContentJSONHex string `json:"content_json_hex"`
}

type billingFixture struct {
	PayerIDHex string        `json:"payer_id_hex"`
	Cases      []billingCase `json:"cases"`
}

type billingCase struct {
	Name                  string   `json:"name"`
	Kind                  uint64   `json:"kind"`
	RequestIDHex          string   `json:"request_id_hex"`
	Channel               string   `json:"channel"`
	ContentJSONHex        string   `json:"content_json_hex"`
	ExpectedResponseKind  uint64   `json:"expected_response_kind"`
	ExpectedSuccess       bool     `json:"expected_success"`
	ExpectedChargedAmount string   `json:"expected_charged_amount"`
	ExpectedErrorCode     string   `json:"expected_error_code"`
	ExpectedChannels      []string `json:"expected_channels"`
}

type billingExchangeCase struct {
	Name        string `json:"name"`
	ResponseHex string `json:"response_hex"`
}

type billingQueryFixture struct {
	PayerIDHex string             `json:"payer_id_hex"`
	Cases      []billingQueryCase `json:"cases"`
}

type billingQueryCase struct {
	Name               string             `json:"name"`
	RequestIDHex       string             `json:"request_id_hex"`
	FromMS             uint64             `json:"from_ms"`
	ToMS               uint64             `json:"to_ms"`
	Limit              uint64             `json:"limit"`
	Cursor             string             `json:"cursor"`
	ExpectedSuccess    bool               `json:"expected_success"`
	ExpectedCurrency   string             `json:"expected_currency"`
	ExpectedNetwork    string             `json:"expected_network"`
	ExpectedRecords    []chargeRecordCase `json:"expected_records"`
	ExpectedNextCursor string             `json:"expected_next_cursor"`
	ExpectedErrorCode  string             `json:"expected_error_code"`
}

type billingQueryExchangeCase struct {
	Name        string `json:"name"`
	ResponseHex string `json:"response_hex"`
}

type dispatchExchange struct {
	MatchCases                []matchResult `json:"match_cases"`
	UniqueMatchingIdentityHex []string      `json:"unique_matching_identity_hex"`
}

type matchResult struct {
	Name    string `json:"name"`
	Matched bool   `json:"matched"`
}

func main() {
	mode := flag.String("mode", "", "exchange mode")
	inputPath := flag.String("input", "", "input JSON path for decode mode")
	flag.Parse()

	var value any
	var err error
	switch *mode {
	case "encode":
		value, err = encodeFixtures()
	case "decode":
		value, err = decodeExchange(*inputPath)
	case "state":
		value, err = stateExchangeResult()
	case "invalid":
		value, err = invalidExchangeResult()
	case "service":
		value, err = serviceExchangeResult()
	case "dispatch":
		value, err = dispatchExchangeResult()
	case "billing":
		value, err = billingExchangeResult()
	case "billing-query":
		value, err = billingQueryExchangeResult()
	default:
		err = fmt.Errorf("unknown mode %q", *mode)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(string(encoded))
}

func readJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func fixturePath(name string) string {
	return "testdata/v1/" + name
}

func encodeFixtures() ([]exchangeCase, error) {
	var fixture wireFixture
	if err := readJSON(fixturePath("wire-v1.json"), &fixture); err != nil {
		return nil, err
	}
	result := make([]exchangeCase, 0, len(fixture.Cases))
	for _, item := range fixture.Cases {
		var wireBytes []byte
		var err error
		switch protocol.Kind(item.Kind) {
		case protocol.PublishKind:
			requestID, requestErr := protocol.NewRequestIDFromBytes(mustHex(item.RequestIDHex))
			if requestErr != nil {
				return nil, requestErr
			}
			wireBytes, err = client.NewPublish(requestID, item.Channel, mustHex(item.ContentJSONHex))
		case protocol.SubscribeKind:
			requestID, requestErr := protocol.NewRequestIDFromBytes(mustHex(item.RequestIDHex))
			if requestErr != nil {
				return nil, requestErr
			}
			wireBytes, err = client.NewSubscribe(requestID, item.Channel)
		case protocol.UnsubscribeKind:
			requestID, requestErr := protocol.NewRequestIDFromBytes(mustHex(item.RequestIDHex))
			if requestErr != nil {
				return nil, requestErr
			}
			wireBytes, err = client.NewUnsubscribe(requestID, item.Channel)
		case protocol.SubscriptionsRequestKind:
			requestID, requestErr := protocol.NewRequestIDFromBytes(mustHex(item.RequestIDHex))
			if requestErr != nil {
				return nil, requestErr
			}
			wireBytes, err = client.NewSubscriptionsRequest(requestID)
		case protocol.BillingRequestKind:
			requestID, requestErr := protocol.NewRequestIDFromBytes(mustHex(item.RequestIDHex))
			if requestErr != nil {
				return nil, requestErr
			}
			wireBytes, err = client.NewBillingRequest(requestID, item.FromMS, item.ToMS, item.Limit, item.Cursor)
		case protocol.SubscriptionsResponseKind:
			var document []byte
			requestID, requestErr := protocol.NewRequestIDFromBytes(mustHex(item.RequestIDHex))
			if requestErr != nil {
				return nil, requestErr
			}
			document, err = wire.EncodeSubscriptionsResponseDocument(protocol.SubscriptionsResponse{
				RequestID: requestID, ChargedAmount: protocol.Amount(item.ChargedAmount), Channels: item.Channels,
			})
			if err == nil {
				wireBytes, err = wire.EncodeWire(protocol.SubscriptionsResponseKind, document)
			}
		case protocol.ActionResultKind:
			requestID, requestErr := protocol.NewRequestIDFromBytes(mustHex(item.RequestIDHex))
			if requestErr != nil {
				return nil, requestErr
			}
			document, documentErr := wire.EncodeActionResultDocument(protocol.ActionResult{
				RequestID: requestID, Success: item.Success, ChargedAmount: protocol.Amount(item.ChargedAmount), ErrorCode: item.ErrorCode,
			})
			if documentErr != nil {
				return nil, documentErr
			}
			wireBytes, err = wire.EncodeWire(protocol.ActionResultKind, document)
		case protocol.BillingResponseKind:
			requestID, requestErr := protocol.NewRequestIDFromBytes(mustHex(item.RequestIDHex))
			if requestErr != nil {
				return nil, requestErr
			}
			records := make([]protocol.ChargeRecord, 0, len(item.Records))
			for _, record := range item.Records {
				sourceRequestID, sourceErr := protocol.NewRequestIDFromBytes(mustHex(record.SourceRequestIDHex))
				if sourceErr != nil {
					return nil, sourceErr
				}
				records = append(records, protocol.ChargeRecord{
					ChargeID:        record.ChargeID,
					OccurredAtMS:    record.OccurredAtMS,
					Action:          record.Action,
					Channel:         record.Channel,
					SourceRequestID: sourceRequestID,
					ChargedAmount:   protocol.Amount(record.ChargedAmount),
				})
			}
			document, documentErr := wire.EncodeBillingResponseDocument(protocol.BillingResponse{
				RequestID: requestID, Success: item.Success, Currency: item.Currency, Network: item.Network,
				Records: records, NextCursor: item.NextCursor, ErrorCode: item.ErrorCode,
			})
			if documentErr != nil {
				return nil, documentErr
			}
			wireBytes, err = wire.EncodeWire(protocol.BillingResponseKind, document)
		default:
			return nil, fmt.Errorf("fixture %s has unknown kind", item.Name)
		}
		if err != nil {
			return nil, fmt.Errorf("encode %s: %w", item.Name, err)
		}
		result = append(result, exchangeCase{Name: item.Name, WireHex: hex.EncodeToString(wireBytes)})
	}
	return result, nil
}

func decodeExchange(path string) ([]exchangeCase, error) {
	if path == "" {
		return nil, fmt.Errorf("decode mode requires -input")
	}
	var input []exchangeCase
	if err := readJSON(path, &input); err != nil {
		return nil, err
	}
	result := make([]exchangeCase, 0, len(input))
	for _, item := range input {
		wireBytes, err := hex.DecodeString(item.WireHex)
		if err != nil {
			return nil, err
		}
		message, err := wire.Parse(wireBytes)
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", item.Name, err)
		}
		reencoded, err := wire.EncodeWire(message.Kind, message.Document)
		if err != nil {
			return nil, err
		}
		result = append(result, exchangeCase{Name: item.Name, WireHex: hex.EncodeToString(reencoded)})
	}
	return result, nil
}

func stateExchangeResult() (stateExchange, error) {
	var fixture subscriptionsFixture
	if err := readJSON(fixturePath("subscriptions-v1.json"), &fixture); err != nil {
		return stateExchange{}, err
	}
	result := stateExchange{
		Normalization: make([]stateResult, 0, len(fixture.Normalization)),
		Transitions:   make([]stateResult, 0, len(fixture.Transitions)),
	}
	for _, item := range fixture.Normalization {
		value, err := subscription.NormalizeSubscriptions(item.Input)
		if err != nil {
			return stateExchange{}, err
		}
		result.Normalization = append(result.Normalization, stateResult{Name: item.Name, Result: value})
	}
	for _, item := range fixture.Transitions {
		var (
			value []string
			err   error
		)
		switch item.Operation {
		case "subscribe":
			value, err = subscription.Subscribe(item.Initial, item.Channel)
		case "unsubscribe":
			value, err = subscription.Unsubscribe(item.Initial, item.Channel)
		default:
			return stateExchange{}, fmt.Errorf("unknown operation %q", item.Operation)
		}
		if err != nil {
			return stateExchange{}, err
		}
		result.Transitions = append(result.Transitions, stateResult{Name: item.Name, Result: value})
	}
	return result, nil
}

func invalidExchangeResult() ([]invalidExchange, error) {
	var fixture invalidFixture
	if err := readJSON(fixturePath("invalid-v1.json"), &fixture); err != nil {
		return nil, err
	}
	result := make([]invalidExchange, 0, len(fixture.Cases))
	for _, item := range fixture.Cases {
		_, err := wire.Parse(mustHex(item.WireHex))
		if err == nil {
			return nil, fmt.Errorf("invalid fixture %s unexpectedly parsed", item.Name)
		}
		result = append(result, invalidExchange{Name: item.Name, Code: string(protocol.ErrorCodeOf(err))})
	}
	return result, nil
}

func serviceExchangeResult() (eventExchange, error) {
	var fixture dispatchFixture
	if err := readJSON(fixturePath("dispatch-v1.json"), &fixture); err != nil {
		return eventExchange{}, err
	}
	key, err := protocol.NewPublicKey(mustHex(fixture.PublishEvent.PayerIDHex))
	if err != nil {
		return eventExchange{}, err
	}
	backend := subscription.NewMemoryBackend()
	dispatcher := service.NewRecordingDispatcher()
	session := service.NewSession(key, backend, dispatcher)
	defer session.Close()
	handled, err := session.HandleMessage(context.Background(), mustHex(fixture.PublishEvent.WireHex))
	if err != nil {
		return eventExchange{}, err
	}
	result := eventExchange{HasResponse: handled.HasResponse}
	result.Response = hex.EncodeToString(handled.Response)
	for _, event := range dispatcher.Events() {
		result.Events = append(result.Events, eventSnapshot{
			RequestIDHex:   hex.EncodeToString(event.RequestID.Bytes()),
			PayerIDHex:     hex.EncodeToString(event.PayerID.Bytes()),
			Channel:        event.Channel,
			ContentJSONHex: hex.EncodeToString(event.ContentJSON),
		})
	}
	return result, nil
}

func billingExchangeResult() ([]billingExchangeCase, error) {
	var fixture billingFixture
	if err := readJSON(fixturePath("billing-v1.json"), &fixture); err != nil {
		return nil, err
	}
	key, err := protocol.NewPublicKey(mustHex(fixture.PayerIDHex))
	if err != nil {
		return nil, err
	}
	backend := &billingBackend{memory: subscription.NewMemoryBackend()}
	session := service.NewSession(key, backend, billingDispatcher{})
	defer session.Close()

	result := make([]billingExchangeCase, 0, len(fixture.Cases))
	for _, item := range fixture.Cases {
		requestWire, requestErr := encodeBillingRequest(item)
		if requestErr != nil {
			return nil, fmt.Errorf("billing request %s: %w", item.Name, requestErr)
		}
		handled, handleErr := session.HandleMessage(context.Background(), requestWire)
		if handleErr != nil {
			return nil, fmt.Errorf("billing service %s: %w", item.Name, handleErr)
		}
		if !handled.HasResponse {
			return nil, fmt.Errorf("billing service %s returned no response", item.Name)
		}
		if responseErr := validateBillingResponse(item, handled.Response); responseErr != nil {
			return nil, responseErr
		}
		result = append(result, billingExchangeCase{
			Name:        item.Name,
			ResponseHex: hex.EncodeToString(handled.Response),
		})
	}
	return result, nil
}

func billingQueryExchangeResult() ([]billingQueryExchangeCase, error) {
	var fixture billingQueryFixture
	if err := readJSON(fixturePath("billing-query-v1.json"), &fixture); err != nil {
		return nil, err
	}
	key, err := protocol.NewPublicKey(mustHex(fixture.PayerIDHex))
	if err != nil {
		return nil, err
	}
	backend := &billingQueryBackend{}
	session := service.NewSession(key, backend, service.NewRecordingDispatcher())
	defer session.Close()
	result := make([]billingQueryExchangeCase, 0, len(fixture.Cases))
	for _, item := range fixture.Cases {
		requestID, requestErr := protocol.NewRequestIDFromBytes(mustHex(item.RequestIDHex))
		if requestErr != nil {
			return nil, requestErr
		}
		requestWire, requestErr := client.NewBillingRequest(requestID, item.FromMS, item.ToMS, item.Limit, item.Cursor)
		if requestErr != nil {
			return nil, fmt.Errorf("billing query request %s: %w", item.Name, requestErr)
		}
		handled, handleErr := session.HandleMessage(context.Background(), requestWire)
		if handleErr != nil || !handled.HasResponse {
			return nil, fmt.Errorf("billing query service %s returned result=%#v error=%v", item.Name, handled, handleErr)
		}
		response, parseErr := client.ParseBillingResponse(handled.Response)
		if parseErr != nil {
			return nil, fmt.Errorf("billing query response %s: %w", item.Name, parseErr)
		}
		if err := validateBillingQueryCase(item, response); err != nil {
			return nil, err
		}
		result = append(result, billingQueryExchangeCase{Name: item.Name, ResponseHex: hex.EncodeToString(handled.Response)})
	}
	return result, nil
}

func validateBillingQueryCase(item billingQueryCase, response protocol.BillingResponse) error {
	requestID, err := protocol.NewRequestIDFromBytes(mustHex(item.RequestIDHex))
	if err != nil {
		return err
	}
	if response.RequestID != requestID || response.Success != item.ExpectedSuccess || response.Currency != item.ExpectedCurrency || response.Network != item.ExpectedNetwork || response.NextCursor != item.ExpectedNextCursor || response.ErrorCode != item.ExpectedErrorCode || len(response.Records) != len(item.ExpectedRecords) {
		return fmt.Errorf("billing query response %s does not match expected fields: %#v", item.Name, response)
	}
	for index, record := range response.Records {
		want := item.ExpectedRecords[index]
		sourceRequestID, sourceErr := protocol.NewRequestIDFromBytes(mustHex(want.SourceRequestIDHex))
		if sourceErr != nil {
			return sourceErr
		}
		if record.ChargeID != want.ChargeID || record.OccurredAtMS != want.OccurredAtMS || record.Action != want.Action || record.Channel != want.Channel || record.SourceRequestID != sourceRequestID || record.ChargedAmount.String() != want.ChargedAmount {
			return fmt.Errorf("billing query response %s record %d does not match", item.Name, index)
		}
	}
	return nil
}

func encodeBillingRequest(item billingCase) ([]byte, error) {
	requestID, err := protocol.NewRequestIDFromBytes(mustHex(item.RequestIDHex))
	if err != nil {
		return nil, err
	}
	switch protocol.Kind(item.Kind) {
	case protocol.PublishKind:
		return client.NewPublish(requestID, item.Channel, mustHex(item.ContentJSONHex))
	case protocol.SubscribeKind:
		return client.NewSubscribe(requestID, item.Channel)
	case protocol.UnsubscribeKind:
		return client.NewUnsubscribe(requestID, item.Channel)
	case protocol.SubscriptionsRequestKind:
		return client.NewSubscriptionsRequest(requestID)
	default:
		return nil, fmt.Errorf("billing fixture %s is not a client request kind", item.Name)
	}
}

func validateBillingResponse(item billingCase, response []byte) error {
	message, err := wire.Parse(response)
	if err != nil {
		return fmt.Errorf("billing response %s is invalid: %w", item.Name, err)
	}
	if uint64(message.Kind) != item.ExpectedResponseKind {
		return fmt.Errorf("billing response %s kind=%d, want %d", item.Name, message.Kind, item.ExpectedResponseKind)
	}
	requestID, err := protocol.NewRequestIDFromBytes(mustHex(item.RequestIDHex))
	if err != nil {
		return err
	}
	switch message.Kind {
	case protocol.ActionResultKind:
		result, parseErr := client.ParseActionResult(response)
		if parseErr != nil {
			return fmt.Errorf("billing ActionResult %s: %w", item.Name, parseErr)
		}
		if result.RequestID != requestID || result.Success != item.ExpectedSuccess || result.ChargedAmount.String() != item.ExpectedChargedAmount || result.ErrorCode != item.ExpectedErrorCode {
			return fmt.Errorf("billing ActionResult %s does not match expected fields", item.Name)
		}
	case protocol.SubscriptionsResponseKind:
		result, parseErr := client.ParseSubscriptionsResponse(response)
		if parseErr != nil {
			return fmt.Errorf("billing SubscriptionsResponse %s: %w", item.Name, parseErr)
		}
		if result.RequestID != requestID || result.ChargedAmount.String() != item.ExpectedChargedAmount || !sameStrings(result.Channels, item.ExpectedChannels) {
			return fmt.Errorf("billing SubscriptionsResponse %s does not match expected fields", item.Name)
		}
	default:
		return fmt.Errorf("billing response %s uses unsupported response kind %d", item.Name, message.Kind)
	}
	return nil
}

type billingBackend struct {
	memory *subscription.MemoryBackend
}

type billingQueryBackend struct{}

func (backend *billingQueryBackend) Subscribe(context.Context, protocol.PublicKey, protocol.RequestID, string) (protocol.Amount, error) {
	return protocol.ZeroAmount, nil
}

func (backend *billingQueryBackend) Unsubscribe(context.Context, protocol.PublicKey, protocol.RequestID, string) (protocol.Amount, error) {
	return protocol.ZeroAmount, nil
}

func (backend *billingQueryBackend) Subscriptions(context.Context, protocol.PublicKey, protocol.RequestID) ([]string, protocol.Amount, error) {
	return []string{}, protocol.ZeroAmount, nil
}

func (backend *billingQueryBackend) ListChargeRecords(_ context.Context, _ protocol.PublicKey, request protocol.BillingRequest) (protocol.BillingPage, error) {
	if request.Cursor == "reject" {
		return protocol.BillingPage{}, protocol.ErrRejected
	}
	if request.Cursor == "cursor-2" {
		return protocol.BillingPage{Currency: "BSV", Network: "testnet", Records: []protocol.ChargeRecord{}, NextCursor: ""}, nil
	}
	sourceRequestID, err := protocol.NewRequestIDFromBytes(mustHex("4a4a4a4a4a4a4a4a4a4a4a4a4a4a4a4a4a4a4a4a4a4a4a4a4a4a4a4a4a4a4a4a"))
	if err != nil {
		return protocol.BillingPage{}, err
	}
	return protocol.BillingPage{
		Currency: "BSV",
		Network:  "testnet",
		Records: []protocol.ChargeRecord{{
			ChargeID: "operation-1", OccurredAtMS: 1_700_000_000_123, Action: "publish", Channel: "news",
			SourceRequestID: sourceRequestID, ChargedAmount: protocol.Amount("0.001"),
		}},
		NextCursor: "cursor-2",
	}, nil
}

func (backend *billingBackend) Subscribe(ctx context.Context, key protocol.PublicKey, requestID protocol.RequestID, channel string) (protocol.Amount, error) {
	if channel == "reject" {
		return "", protocol.ErrRejected
	}
	if _, err := backend.memory.Subscribe(ctx, key, requestID, channel); err != nil {
		return "", err
	}
	return protocol.ZeroAmount, nil
}

func (backend *billingBackend) Unsubscribe(ctx context.Context, key protocol.PublicKey, requestID protocol.RequestID, channel string) (protocol.Amount, error) {
	if _, err := backend.memory.Unsubscribe(ctx, key, requestID, channel); err != nil {
		return "", err
	}
	return protocol.Amount("0.001"), nil
}

func (backend *billingBackend) Subscriptions(ctx context.Context, key protocol.PublicKey, requestID protocol.RequestID) ([]string, protocol.Amount, error) {
	channels, _, err := backend.memory.Subscriptions(ctx, key, requestID)
	if err != nil {
		return nil, "", err
	}
	return channels, protocol.Amount("12.34"), nil
}

func (backend *billingBackend) ListChargeRecords(ctx context.Context, key protocol.PublicKey, request protocol.BillingRequest) (protocol.BillingPage, error) {
	if err := protocol.ValidateBillingRequest(request); err != nil {
		return protocol.BillingPage{}, err
	}
	return protocol.BillingPage{Currency: "BSV", Network: "mainnet", Records: []protocol.ChargeRecord{}, NextCursor: ""}, nil
}

type billingDispatcher struct{}

func (billingDispatcher) Dispatch(_ context.Context, event service.PublishEvent) (protocol.Amount, error) {
	if event.Channel == "internal" {
		return "", fmt.Errorf("broadcast dispatcher unavailable")
	}
	return protocol.Amount("1.2"), nil
}

func dispatchExchangeResult() (dispatchExchange, error) {
	var fixture dispatchFixture
	if err := readJSON(fixturePath("dispatch-v1.json"), &fixture); err != nil {
		return dispatchExchange{}, err
	}
	result := dispatchExchange{MatchCases: make([]matchResult, 0, len(fixture.MatchCases))}
	for _, item := range fixture.MatchCases {
		matched, err := subscription.Matches(item.Subscriptions, item.PublishChannel)
		if err != nil {
			return dispatchExchange{}, err
		}
		if matched != item.Expected {
			return dispatchExchange{}, fmt.Errorf("dispatch fixture %s expected matched=%t, got %t", item.Name, item.Expected, matched)
		}
		result.MatchCases = append(result.MatchCases, matchResult{Name: item.Name, Matched: matched})
	}
	seen := make(map[protocol.PublicKey]bool)
	for _, identity := range fixture.DuplicateIdentityCase.OnlineIdentities {
		key, err := protocol.NewPublicKey(mustHex(identity.RemotePublicKeyHex))
		if err != nil {
			return dispatchExchange{}, err
		}
		matched, err := subscription.Matches(identity.Subscriptions, fixture.DuplicateIdentityCase.PublishChannel)
		if err != nil {
			return dispatchExchange{}, err
		}
		if matched && !seen[key] {
			seen[key] = true
			result.UniqueMatchingIdentityHex = append(result.UniqueMatchingIdentityHex, hex.EncodeToString(key.Bytes()))
		}
	}
	if !sameStrings(result.UniqueMatchingIdentityHex, fixture.DuplicateIdentityCase.ExpectedUniqueMatchingIdentity) {
		return dispatchExchange{}, fmt.Errorf("dispatch fixture unique identities do not match expected result")
	}
	return result, nil
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

func mustHex(value string) []byte {
	result, err := hex.DecodeString(value)
	if err != nil {
		panic(err)
	}
	return result
}
