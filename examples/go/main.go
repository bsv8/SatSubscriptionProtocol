// 示例只调用进程内 SSP SDK，不建立网络连接。
package main

import (
	"context"
	"fmt"
	"sync"

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
	session := service.NewSession(remotePublicKey, backend, dispatcher)

	idNews := requestID(1)
	idAlerts := requestID(2)
	newsWire, err := client.NewSubscribe(idNews, "news")
	if err != nil {
		panic(err)
	}
	alertsWire, err := client.NewSubscribe(idAlerts, "alerts")
	if err != nil {
		panic(err)
	}

	// 两个不同 request_id 可以并行；响应不能按到达顺序猜请求，要按编号关联。
	responses := make(chan []byte, 2)
	var waitGroup sync.WaitGroup
	for _, requestWire := range [][]byte{newsWire, alertsWire} {
		waitGroup.Add(1)
		go func(requestWire []byte) {
			defer waitGroup.Done()
			result, handleErr := session.HandleMessage(context.Background(), requestWire)
			if handleErr != nil {
				panic(handleErr)
			}
			responses <- result.Response
		}(requestWire)
	}
	waitGroup.Wait()
	close(responses)
	for responseWire := range responses {
		result, parseErr := client.ParseActionResult(responseWire)
		if parseErr != nil {
			panic(parseErr)
		}
		fmt.Println("action result（动作结果）:", result.RequestID.Bytes(), result.ChargedAmount.String())
	}

	queryID := requestID(3)
	queryWire, err := client.NewSubscriptionsRequest(queryID)
	if err != nil {
		panic(err)
	}
	queryResult, err := session.HandleMessage(context.Background(), queryWire)
	if err != nil {
		panic(err)
	}
	response, err := client.ParseSubscriptionsResponse(queryResult.Response)
	if err != nil {
		panic(err)
	}
	fmt.Println("subscriptions（订阅列表）:", response.Channels, "charged_amount（实际扣费）:", response.ChargedAmount)

	// 同一编号重复到达会静默丢弃，不执行、不扣费、不返回历史结果。
	duplicate, err := session.HandleMessage(context.Background(), newsWire)
	if err != nil {
		panic(err)
	}
	fmt.Println("duplicate suppressed（重复编号已丢弃）:", duplicate.Suppressed)

	publishID := requestID(4)
	publishWire, err := client.NewPublish(publishID, "news", []byte(` {"title":"hello"} `))
	if err != nil {
		panic(err)
	}
	publishResult, err := session.HandleMessage(context.Background(), publishWire)
	if err != nil {
		panic(err)
	}
	action, err := client.ParseActionResult(publishResult.Response)
	if err != nil {
		panic(err)
	}
	fmt.Println("publish accepted（发布已接受）:", action.Success, "events（广播事件）:", len(dispatcher.Events()))
	// Publish 成功表示服务端已接受、扣费并开始广播，不代表订阅者已经收到。
}

func requestID(value byte) protocol.RequestID {
	var result protocol.RequestID
	for index := range result {
		result[index] = value
	}
	return result
}
