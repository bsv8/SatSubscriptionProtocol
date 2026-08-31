package subscription_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/bsv8/SatSubscriptionProtocol/protocol"
	"github.com/bsv8/SatSubscriptionProtocol/subscription"
)

type subscriptionFixture struct {
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

func readFixture(t *testing.T) subscriptionFixture {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "testdata", "v1", "subscriptions-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture subscriptionFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func TestSharedSubscriptionVectors(t *testing.T) {
	fixture := readFixture(t)
	for _, testCase := range fixture.Normalization {
		t.Run("normalize/"+testCase.Name, func(t *testing.T) {
			got, err := subscription.NormalizeSubscriptions(testCase.Input)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, testCase.Expected) {
				t.Fatalf("got %#v, want %#v", got, testCase.Expected)
			}
		})
	}
	for _, testCase := range fixture.Transitions {
		t.Run("transition/"+testCase.Name, func(t *testing.T) {
			var (
				got []string
				err error
			)
			switch testCase.Operation {
			case "subscribe":
				got, err = subscription.Subscribe(testCase.Initial, testCase.Channel)
			case "unsubscribe":
				got, err = subscription.Unsubscribe(testCase.Initial, testCase.Channel)
			default:
				t.Fatalf("unknown operation %q", testCase.Operation)
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, testCase.Expected) {
				t.Fatalf("got %#v, want %#v", got, testCase.Expected)
			}
		})
	}
}

func TestMatchesRejectsInvalidState(t *testing.T) {
	if matched, err := subscription.Matches([]string{"news"}, "news"); err != nil || !matched {
		t.Fatalf("ordinary match = %v, %v", matched, err)
	}
	if matched, err := subscription.Matches([]string{"*"}, "anything"); err != nil || !matched {
		t.Fatalf("wildcard match = %v, %v", matched, err)
	}
	if _, err := subscription.Matches([]string{"news", "news"}, "news"); !errorsIsInvalidSubscriptions(err) {
		t.Fatalf("duplicate state error = %v", err)
	}
}

func errorsIsInvalidSubscriptions(err error) bool {
	return err != nil && protocol.ErrorCodeOf(err) == protocol.CodeInvalidSubscriptions
}

func TestMemoryBackendConcurrentUpdatesAreAtomic(t *testing.T) {
	backend := subscription.NewMemoryBackend()
	var key protocol.PublicKey
	key[0] = 0x02
	for index := 1; index < len(key); index++ {
		key[index] = byte(index)
	}

	const operations = 100
	var waitGroup sync.WaitGroup
	waitGroup.Add(operations)
	for index := 0; index < operations; index++ {
		channel := "channel-" + string(rune('a'+index%26)) + string(rune('0'+index/26))
		go func() {
			defer waitGroup.Done()
			if _, err := backend.Subscribe(context.Background(), key, testRequestID(), channel); err != nil {
				t.Errorf("Subscribe failed: %v", err)
			}
		}()
	}
	waitGroup.Wait()
	channels, _, err := backend.Subscriptions(context.Background(), key, testRequestID())
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != operations {
		t.Fatalf("concurrent channel count = %d, want %d", len(channels), operations)
	}

	for index := 0; index < operations; index++ {
		channel := "channel-" + string(rune('a'+index%26)) + string(rune('0'+index/26))
		if _, err := backend.Subscribe(context.Background(), key, testRequestID(), channel); err != nil {
			t.Fatal(err)
		}
	}
	channelsAfterRetry, _, err := backend.Subscriptions(context.Background(), key, testRequestID())
	if err != nil {
		t.Fatal(err)
	}
	if len(channelsAfterRetry) != operations {
		t.Fatalf("idempotent channel count = %d, want %d", len(channelsAfterRetry), operations)
	}
}

func testRequestID() protocol.RequestID {
	var result protocol.RequestID
	result[0] = 0x02
	return result
}
