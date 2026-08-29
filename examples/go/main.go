// 示例只调用进程内 SSP SDK，不建立网络连接。
package main

import (
	"context"
	"fmt"

	"github.com/bsv8/SatSubscriptionProtocol/client"
	"github.com/bsv8/SatSubscriptionProtocol/protocol"
	"github.com/bsv8/SatSubscriptionProtocol/service"
	"github.com/bsv8/SatSubscriptionProtocol/subscription"
)

func main() {
	// remotePublicKey 必须来自外部已认证长期连接；示例使用固定测试公钥。
	remotePublicKeyBytes := make([]byte, 33)
	remotePublicKeyBytes[0] = 0x02
	remotePublicKey, err := protocol.NewPublicKey(remotePublicKeyBytes)
	if err != nil {
		panic(err)
	}

	backend := subscription.NewMemoryBackend()
	dispatcher := service.NewRecordingDispatcher()

	subscribeWire, err := client.NewSubscribe("news") // 创建订阅 Wire（订阅报文）
	if err != nil {
		panic(err)
	}
	_, _, err = service.HandleMessage(context.Background(), remotePublicKey, subscribeWire, backend, dispatcher)
	if err != nil {
		panic(err)
	}

	requestWire, err := client.NewSubscriptionsRequest() // 创建查询 Wire（查询订阅列表）
	if err != nil {
		panic(err)
	}
	responseWire, hasResponse, err := service.HandleMessage(context.Background(), remotePublicKey, requestWire, backend, dispatcher)
	if err != nil || !hasResponse {
		panic(err)
	}
	channels, err := client.ParseSubscriptionsResponse(responseWire) // 解析规范订阅列表
	if err != nil {
		panic(err)
	}
	fmt.Println("subscriptions（订阅列表）:", channels)

	publishWire, err := client.NewPublish("news", []byte(` {"title":"hello"} `)) // JSON exact bytes
	if err != nil {
		panic(err)
	}
	_, _, err = service.HandleMessage(context.Background(), remotePublicKey, publishWire, backend, dispatcher)
	if err != nil {
		panic(err)
	}
	fmt.Println("recorded events（记录的广播事件）:", len(dispatcher.Events()))
}
