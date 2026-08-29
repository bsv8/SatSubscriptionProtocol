package service_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/bsv8/SatSubscriptionProtocol/client"
	"github.com/bsv8/SatSubscriptionProtocol/protocol"
	"github.com/bsv8/SatSubscriptionProtocol/service"
	"github.com/bsv8/SatSubscriptionProtocol/subscription"
)

type dispatchFixture struct {
	PublishEvent publishEventFixture `json:"publish_event"`
}

type publishEventFixture struct {
	PublisherPublicKeyHex string `json:"publisher_public_key_hex"`
	Channel               string `json:"channel"`
	ContentJSONHex        string `json:"content_json_hex"`
	WireHex               string `json:"wire_hex"`
}

func readDispatchFixture(t *testing.T) dispatchFixture {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "testdata", "v1", "dispatch-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture dispatchFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func fixedKey(t *testing.T) protocol.PublicKey {
	t.Helper()
	key, err := protocol.NewPublicKey(mustHex(t, "023333333333333333333333333333333333333333333333333333333333333333"))
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func mustHex(t *testing.T, value string) []byte {
	t.Helper()
	result, err := hex.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestPublishDispatcherReceivesExactBytes(t *testing.T) {
	fixture := readDispatchFixture(t)
	key, err := protocol.NewPublicKey(mustHex(t, fixture.PublishEvent.PublisherPublicKeyHex))
	if err != nil {
		t.Fatal(err)
	}
	wireBytes := mustHex(t, fixture.PublishEvent.WireHex)
	backend := subscription.NewMemoryBackend()
	dispatcher := service.NewRecordingDispatcher()
	response, hasResponse, err := service.HandleMessage(context.Background(), key, wireBytes, backend, dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	if hasResponse || response != nil {
		t.Fatalf("Publish returned a direct response: %x", response)
	}
	events := dispatcher.Events()
	if len(events) != 1 {
		t.Fatalf("recorded events = %d, want 1", len(events))
	}
	event := events[0]
	if event.Channel != fixture.PublishEvent.Channel || string(event.ContentJSON) != string(mustHex(t, fixture.PublishEvent.ContentJSONHex)) || string(event.Wire) != string(wireBytes) || event.PublisherPublicKey != key {
		t.Fatalf("dispatcher event does not preserve authenticated identity and exact bytes")
	}
	event.ContentJSON[0] = 'x'
	event.Wire[0] = 0
	if string(dispatcher.Events()[0].ContentJSON) != string(mustHex(t, fixture.PublishEvent.ContentJSONHex)) {
		t.Fatalf("RecordingDispatcher leaked mutable ContentJSON")
	}
}

func TestSubscribeQueryUnsubscribeSequence(t *testing.T) {
	key := fixedKey(t)
	backend := subscription.NewMemoryBackend()
	dispatcher := service.NewRecordingDispatcher()
	ctx := context.Background()

	subscribeWire, err := client.NewSubscribe("news")
	if err != nil {
		t.Fatal(err)
	}
	if response, hasResponse, handleErr := service.HandleMessage(ctx, key, subscribeWire, backend, dispatcher); handleErr != nil || hasResponse || response != nil {
		t.Fatalf("Subscribe result = %x, %v, %v", response, hasResponse, handleErr)
	}
	requestWire, err := client.NewSubscriptionsRequest()
	if err != nil {
		t.Fatal(err)
	}
	response, hasResponse, err := service.HandleMessage(ctx, key, requestWire, backend, dispatcher)
	if err != nil || !hasResponse {
		t.Fatalf("SubscriptionsRequest result = %x, %v, %v", response, hasResponse, err)
	}
	channels, err := client.ParseSubscriptionsResponse(response)
	if err != nil || len(channels) != 1 || channels[0] != "news" {
		t.Fatalf("queried channels = %#v, %v", channels, err)
	}

	unsubscribeWire, err := client.NewUnsubscribe("news")
	if err != nil {
		t.Fatal(err)
	}
	if response, hasResponse, handleErr := service.HandleMessage(ctx, key, unsubscribeWire, backend, dispatcher); handleErr != nil || hasResponse || response != nil {
		t.Fatalf("Unsubscribe result = %x, %v, %v", response, hasResponse, handleErr)
	}
	response, hasResponse, err = service.HandleMessage(ctx, key, requestWire, backend, dispatcher)
	if err != nil || !hasResponse {
		t.Fatalf("second SubscriptionsRequest result = %x, %v, %v", response, hasResponse, err)
	}
	channels, err = client.ParseSubscriptionsResponse(response)
	if err != nil || len(channels) != 0 {
		t.Fatalf("channels after Unsubscribe = %#v, %v", channels, err)
	}
}

func TestServiceRejectsResponseCommand(t *testing.T) {
	key := fixedKey(t)
	backend := subscription.NewMemoryBackend()
	dispatcher := service.NewRecordingDispatcher()
	request, err := client.NewSubscriptionsRequest()
	if err != nil {
		t.Fatal(err)
	}
	responseWire, hasResponse, err := service.HandleMessage(context.Background(), key, request, backend, dispatcher)
	if err != nil || !hasResponse || responseWire == nil {
		t.Fatalf("request result = %x, %v, %v", responseWire, hasResponse, err)
	}
	_, hasResponse, err = service.HandleMessage(context.Background(), key, responseWire, backend, dispatcher)
	if err == nil || protocol.ErrorCodeOf(err) != protocol.CodeInvalidWire {
		t.Fatalf("SubscriptionsResponse command result = %v, %v", hasResponse, err)
	}
}
