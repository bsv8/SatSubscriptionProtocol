package wire_test

import (
	"encoding/hex"
	"testing"

	"github.com/bsv8/SatSubscriptionProtocol/protocol"
	"github.com/bsv8/SatSubscriptionProtocol/wire"
)

// FuzzParseRejectsOrParses 确认随机输入不会让严格解析器 panic 或产生未分类错误。
func FuzzParseRejectsOrParses(f *testing.F) {
	for _, seed := range []string{
		"8301044180",
		"830101581882646e657773517b227469746c65223a2268656c6c6f227d",
		"830105428180",
	} {
		data, err := hex.DecodeString(seed)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		_, err := wire.Parse(input)
		if err != nil && protocol.ErrorCodeOf(err) == "" {
			t.Fatalf("unclassified parser error: %v", err)
		}
	})
}
