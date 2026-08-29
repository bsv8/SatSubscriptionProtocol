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
	Name           string   `json:"name"`
	Kind           uint64   `json:"kind"`
	Channel        string   `json:"channel"`
	ContentJSONHex string   `json:"content_json_hex"`
	Channels       []string `json:"channels"`
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
	PublisherPublicKeyHex string `json:"publisher_public_key_hex"`
	Channel               string `json:"channel"`
	ContentJSONHex        string `json:"content_json_hex"`
	WireHex               string `json:"wire_hex"`
}

type eventExchange struct {
	Response    string          `json:"response_hex"`
	HasResponse bool            `json:"has_response"`
	Events      []eventSnapshot `json:"events"`
}

type eventSnapshot struct {
	PublisherPublicKeyHex string `json:"publisher_public_key_hex"`
	Channel               string `json:"channel"`
	ContentJSONHex        string `json:"content_json_hex"`
	WireHex               string `json:"wire_hex"`
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
	_ = flag.Bool("billing", false, "ignored external billing gate used to prove Wire stability")
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
			wireBytes, err = client.NewPublish(item.Channel, mustHex(item.ContentJSONHex))
		case protocol.SubscribeKind:
			wireBytes, err = client.NewSubscribe(item.Channel)
		case protocol.UnsubscribeKind:
			wireBytes, err = client.NewUnsubscribe(item.Channel)
		case protocol.SubscriptionsRequestKind:
			wireBytes, err = client.NewSubscriptionsRequest()
		case protocol.SubscriptionsResponseKind:
			var document []byte
			document, err = wire.EncodeSubscriptionsResponseDocument(item.Channels)
			if err == nil {
				wireBytes, err = wire.EncodeWire(protocol.SubscriptionsResponseKind, document)
			}
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
	key, err := protocol.NewPublicKey(mustHex(fixture.PublishEvent.PublisherPublicKeyHex))
	if err != nil {
		return eventExchange{}, err
	}
	backend := subscription.NewMemoryBackend()
	dispatcher := service.NewRecordingDispatcher()
	response, hasResponse, err := service.HandleMessage(context.Background(), key, mustHex(fixture.PublishEvent.WireHex), backend, dispatcher)
	if err != nil {
		return eventExchange{}, err
	}
	result := eventExchange{HasResponse: hasResponse}
	result.Response = hex.EncodeToString(response)
	for _, event := range dispatcher.Events() {
		result.Events = append(result.Events, eventSnapshot{
			PublisherPublicKeyHex: hex.EncodeToString(event.PublisherPublicKey.Bytes()),
			Channel:               event.Channel,
			ContentJSONHex:        hex.EncodeToString(event.ContentJSON),
			WireHex:               hex.EncodeToString(event.Wire),
		})
	}
	return result, nil
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
