package wire_test

import (
	"fmt"
	"testing"

	"github.com/bsv8/SatSubscriptionProtocol/client"
	"github.com/bsv8/SatSubscriptionProtocol/protocol"
	"github.com/bsv8/SatSubscriptionProtocol/subscription"
	"github.com/bsv8/SatSubscriptionProtocol/wire"
)

func TestFrozenLimitBoundaries(t *testing.T) {
	t.Run("channel UTF-8 bytes", func(t *testing.T) {
		atLimit := repeatedChannel(protocol.MaxChannelUTF8Bytes)
		if _, err := client.NewSubscribe(atLimit); err != nil {
			t.Fatalf("channel at limit was rejected: %v", err)
		}

		_, err := client.NewSubscribe(repeatedChannel(protocol.MaxChannelUTF8Bytes + 1))
		requireErrorCode(t, err, protocol.CodeInvalidChannel)
	})

	t.Run("content JSON bytes", func(t *testing.T) {
		atLimit := validJSONString(protocol.MaxContentJSONBytes)
		if _, err := client.NewPublish("news", atLimit); err != nil {
			t.Fatalf("content_json at limit was rejected: %v", err)
		}

		_, err := client.NewPublish("news", validJSONString(protocol.MaxContentJSONBytes+1))
		requireErrorCode(t, err, protocol.CodeMessageTooLarge)
	})

	t.Run("subscription count", func(t *testing.T) {
		atLimit := boundaryChannels(protocol.MaxSubscriptions)
		normalized, err := subscription.NormalizeSubscriptions(atLimit)
		if err != nil {
			t.Fatalf("subscriptions at limit were rejected: %v", err)
		}
		if len(normalized) != protocol.MaxSubscriptions {
			t.Fatalf("normalized subscription count = %d, want %d", len(normalized), protocol.MaxSubscriptions)
		}
		if _, err := wire.EncodeSubscriptionsResponseDocument(atLimit); err != nil {
			t.Fatalf("response at subscription limit was rejected: %v", err)
		}

		overLimit := append(append([]string(nil), atLimit...), fmt.Sprintf("channel-%04d", protocol.MaxSubscriptions))
		_, err = subscription.NormalizeSubscriptions(overLimit)
		requireErrorCode(t, err, protocol.CodeInvalidSubscriptions)
		_, err = wire.EncodeSubscriptionsResponseDocument(overLimit)
		requireErrorCode(t, err, protocol.CodeInvalidSubscriptions)
	})
}

func TestFullWireLimitBoundary(t *testing.T) {
	validWire, err := client.NewSubscribe("x")
	if err != nil {
		t.Fatal(err)
	}

	// document 和字段还有更小的冻结上限，不存在合法 Wire 恰好填满 MaxWireBytes；
	// 使用合法前缀加尾随字节，专门验证完整 Wire 的外层长度门槛是“超过”而不是“达到”。
	atLimit := make([]byte, protocol.MaxWireBytes)
	copy(atLimit, validWire)
	_, err = wire.Parse(atLimit)
	requireErrorCode(t, err, protocol.CodeInvalidWire)

	overLimit := append(append([]byte(nil), atLimit...), 0)
	_, err = wire.Parse(overLimit)
	requireErrorCode(t, err, protocol.CodeMessageTooLarge)
}

func requireErrorCode(t *testing.T, err error, want protocol.ErrorCode) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error code %s, got nil", want)
	}
	if got := protocol.ErrorCodeOf(err); got != want {
		t.Fatalf("error code = %s, want %s (%v)", got, want, err)
	}
}

func repeatedChannel(length int) string {
	result := make([]byte, length)
	for index := range result {
		result[index] = 'a'
	}
	return string(result)
}

func validJSONString(length int) []byte {
	result := make([]byte, length)
	result[0] = '"'
	for index := 1; index < len(result)-1; index++ {
		result[index] = 'a'
	}
	result[len(result)-1] = '"'
	return result
}

func boundaryChannels(count int) []string {
	result := make([]string, count)
	for index := range result {
		result[index] = fmt.Sprintf("channel-%04d", index)
	}
	return result
}
