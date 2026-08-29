package wire_test

import (
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
	Channel        string   `json:"channel"`
	ContentJSONHex string   `json:"content_json_hex"`
	Channels       []string `json:"channels"`
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

			switch message.Kind {
			case protocol.PublishKind:
				publish, decodeErr := wire.DecodePublishDocument(message.Document)
				if decodeErr != nil {
					t.Fatal(decodeErr)
				}
				contentJSON, decodeErr := hex.DecodeString(testCase.ContentJSONHex)
				if decodeErr != nil {
					t.Fatal(decodeErr)
				}
				if publish.Channel != testCase.Channel || string(publish.ContentJSON) != string(contentJSON) {
					t.Fatalf("Publish fields do not match fixture")
				}
			case protocol.SubscriptionsResponseKind:
				channels, decodeErr := wire.DecodeSubscriptionsResponseDocument(message.Document)
				if decodeErr != nil {
					t.Fatal(decodeErr)
				}
				if len(channels) != len(testCase.Channels) {
					t.Fatalf("channels = %#v, want %#v", channels, testCase.Channels)
				}
				for index := range channels {
					if channels[index] != testCase.Channels[index] {
						t.Fatalf("channels = %#v, want %#v", channels, testCase.Channels)
					}
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
	content := []byte(" {\"n\":1} ")
	originalContent := append([]byte(nil), content...)
	encoded, err := client.NewPublish("news", content)
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
