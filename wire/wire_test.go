package wire_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/bsv8/SatSubscriptionProtocol/client"
	"github.com/bsv8/SatSubscriptionProtocol/protocol"
	"github.com/bsv8/SatSubscriptionProtocol/wire"
)

type wireFixture struct {
	Version int        `json:"version"`
	Cases   []wireCase `json:"cases"`
}

type wireCase struct {
	Name           string   `json:"name"`
	Kind           uint64   `json:"kind"`
	RequestIDHex   string   `json:"request_id_hex"`
	Channel        string   `json:"channel"`
	ContentJSONHex string   `json:"content_json_hex"`
	ChargedAmount  string   `json:"charged_amount"`
	Channels       []string `json:"channels"`
	Success        bool     `json:"success"`
	ErrorCode      string   `json:"error_code"`
	WireHex        string   `json:"wire_hex"`
}

type invalidFixture struct {
	Cases []invalidCase `json:"cases"`
}

type invalidCase struct {
	Name         string `json:"name"`
	WireHex      string `json:"wire_hex"`
	ExpectedCode string `json:"expected_code"`
}

func readJSONFixture[T any](t *testing.T, name string, target *T) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "testdata", "v1", name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}

func TestWireFixturesRoundTrip(t *testing.T) {
	var fixture wireFixture
	readJSONFixture(t, "wire-v1.json", &fixture)
	if fixture.Version != 1 {
		t.Fatalf("fixture version = %d", fixture.Version)
	}
	seenKinds := make(map[protocol.Kind]bool)
	for _, testCase := range fixture.Cases {
		t.Run(testCase.Name, func(t *testing.T) {
			input, err := hex.DecodeString(testCase.WireHex)
			if err != nil {
				t.Fatal(err)
			}
			message, err := wire.Parse(input)
			if err != nil {
				t.Fatal(err)
			}
			if message.Kind != protocol.Kind(testCase.Kind) {
				t.Fatalf("kind = %d, want %d", message.Kind, testCase.Kind)
			}
			encoded, err := wire.EncodeWire(message.Kind, message.Document)
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != string(input) {
				t.Fatalf("re-encoded Wire differs from fixture")
			}
			seenKinds[message.Kind] = true

			requestID, err := hex.DecodeString(testCase.RequestIDHex)
			if err != nil {
				t.Fatal(err)
			}
			switch message.Kind {
			case protocol.PublishKind:
				publish, decodeErr := client.ParsePublish(input)
				if decodeErr != nil {
					t.Fatal(decodeErr)
				}
				contentJSON, decodeErr := hex.DecodeString(testCase.ContentJSONHex)
				if decodeErr != nil {
					t.Fatal(decodeErr)
				}
				if publish.RequestID != mustRequestID(t, requestID) || publish.Channel != testCase.Channel || string(publish.ContentJSON) != string(contentJSON) {
					t.Fatalf("Publish fields do not match fixture")
				}
			case protocol.SubscribeKind:
				subscribe, decodeErr := wire.DecodeSubscribeDocument(message.Document)
				if decodeErr != nil || subscribe.RequestID != mustRequestID(t, requestID) || subscribe.Channel != testCase.Channel {
					t.Fatalf("Subscribe fields = %#v, err=%v", subscribe, decodeErr)
				}
			case protocol.UnsubscribeKind:
				unsubscribe, decodeErr := wire.DecodeUnsubscribeDocument(message.Document)
				if decodeErr != nil || unsubscribe.RequestID != mustRequestID(t, requestID) || unsubscribe.Channel != testCase.Channel {
					t.Fatalf("Unsubscribe fields = %#v, err=%v", unsubscribe, decodeErr)
				}
			case protocol.SubscriptionsRequestKind:
				request, decodeErr := wire.DecodeSubscriptionsRequestDocument(message.Document)
				if decodeErr != nil || request.RequestID != mustRequestID(t, requestID) {
					t.Fatalf("SubscriptionsRequest fields = %#v, err=%v", request, decodeErr)
				}
			case protocol.SubscriptionsResponseKind:
				response, decodeErr := client.ParseSubscriptionsResponse(input)
				if decodeErr != nil {
					t.Fatal(decodeErr)
				}
				if response.RequestID != mustRequestID(t, requestID) || response.ChargedAmount.String() != testCase.ChargedAmount || !sameStrings(response.Channels, testCase.Channels) {
					t.Fatalf("SubscriptionsResponse = %#v", response)
				}
			case protocol.ActionResultKind:
				result, decodeErr := client.ParseActionResult(input)
				if decodeErr != nil {
					t.Fatal(decodeErr)
				}
				if result.RequestID != mustRequestID(t, requestID) || result.Success != testCase.Success || result.ChargedAmount.String() != testCase.ChargedAmount || result.ErrorCode != testCase.ErrorCode {
					t.Fatalf("ActionResult = %#v", result)
				}
			}
		})
	}
	for _, kind := range []protocol.Kind{
		protocol.PublishKind,
		protocol.SubscribeKind,
		protocol.UnsubscribeKind,
		protocol.SubscriptionsRequestKind,
		protocol.SubscriptionsResponseKind,
		protocol.ActionResultKind,
	} {
		if !seenKinds[kind] {
			t.Errorf("fixture does not cover kind %d", kind)
		}
	}
}

func TestInvalidFixtureErrorCodes(t *testing.T) {
	var fixture invalidFixture
	readJSONFixture(t, "invalid-v1.json", &fixture)
	for _, testCase := range fixture.Cases {
		t.Run(testCase.Name, func(t *testing.T) {
			input, err := hex.DecodeString(testCase.WireHex)
			if err != nil {
				t.Fatal(err)
			}
			_, parseErr := wire.Parse(input)
			if parseErr == nil {
				t.Fatal("invalid fixture unexpectedly parsed")
			}
			if got := string(protocol.ErrorCodeOf(parseErr)); got != testCase.ExpectedCode {
				t.Fatalf("error code = %s, want %s (%v)", got, testCase.ExpectedCode, parseErr)
			}
		})
	}
}

func TestDefensiveCopies(t *testing.T) {
	id := mustRequestID(t, bytesOf(0x11))
	content := []byte(" {\"n\":1} ")
	originalContent := append([]byte(nil), content...)
	encoded, err := client.NewPublish(id, "news", content)
	if err != nil {
		t.Fatal(err)
	}
	content[0] = 'x'
	publish, err := client.ParsePublish(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if string(publish.ContentJSON) != string(originalContent) {
		t.Fatalf("content_json changed after caller mutation")
	}
	publish.ContentJSON[0] = 'x'
	parsedAgain, err := client.ParsePublish(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if string(parsedAgain.ContentJSON) != string(originalContent) {
		t.Fatalf("ParsePublish returned shared content bytes")
	}

	message, err := wire.Parse(encoded)
	if err != nil {
		t.Fatal(err)
	}
	message.Document[0] = 0
	if _, err := wire.Parse(encoded); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(protocol.ErrInvalidJSON, protocol.ErrInvalidJSON) {
		t.Fatal("sentinel sanity check failed")
	}
}

func TestPublishForwardingUsesNewRequestIDAndExactContentBytes(t *testing.T) {
	inboundRequestID := mustRequestID(t, bytesOf(0x21))
	deliveryRequestID := mustRequestID(t, bytesOf(0x22))
	contentJSON := []byte("  {\"n\":1,\"text\":\"exact\"} \n")

	inboundWire, err := client.NewPublish(inboundRequestID, "news", contentJSON)
	if err != nil {
		t.Fatal(err)
	}
	inbound, err := client.ParsePublish(inboundWire)
	if err != nil {
		t.Fatal(err)
	}
	deliveryWire, err := client.NewPublish(deliveryRequestID, inbound.Channel, inbound.ContentJSON)
	if err != nil {
		t.Fatal(err)
	}
	delivery, err := client.ParsePublish(deliveryWire)
	if err != nil {
		t.Fatal(err)
	}

	if delivery.RequestID == inbound.RequestID {
		t.Fatal("delivery request_id must be newly generated")
	}
	if delivery.Channel != inbound.Channel {
		t.Fatalf("delivery channel = %q, want %q", delivery.Channel, inbound.Channel)
	}
	if !bytes.Equal(delivery.ContentJSON, contentJSON) || !bytes.Equal(delivery.ContentJSON, inbound.ContentJSON) {
		t.Fatal("delivery content_json bytes changed")
	}
	if bytes.Equal(deliveryWire, inboundWire) {
		t.Fatal("different request IDs must produce different SSP Wire bytes")
	}
}

func mustRequestID(t *testing.T, raw []byte) protocol.RequestID {
	t.Helper()
	result, err := protocol.NewRequestIDFromBytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func bytesOf(value byte) []byte {
	result := make([]byte, protocol.RequestIDBytes)
	for index := range result {
		result[index] = value
	}
	return result
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
