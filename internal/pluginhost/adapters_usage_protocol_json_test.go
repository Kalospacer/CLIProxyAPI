package pluginhost

import (
	"encoding/json"
	"reflect"
	"testing"

	coreusage "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func TestPluginUsageBreakdownPreservesProtocolAccountingAcrossJSON(t *testing.T) {
	cases := map[string]coreusage.TokenBreakdown{
		"subset":             coreusage.NewSubsetTokenBreakdown(100, 30, 10, 50, 20, 150),
		"independent":        coreusage.NewIndependentTokenBreakdown(100, 30, 10, 30, 20, 190),
		"separate_reasoning": coreusage.NewSeparateReasoningTokenBreakdown(100, 30, 10, 50, 20, 170),
		"unclassified":       coreusage.NewUnclassifiedTokenBreakdown(190),
	}
	for name, original := range cases {
		t.Run(name, func(t *testing.T) {
			if !original.Valid() {
				t.Fatalf("invalid host fixture: %+v", original)
			}
			mapped := pluginUsageBreakdown(original)
			raw, err := json.Marshal(pluginapi.UsageRecord{
				Provider: "mirasim", ExecutorType: "executorAdapter", Model: "same-model",
				Detail: pluginapi.UsageDetail{Breakdown: mapped},
			})
			if err != nil {
				t.Fatal(err)
			}
			var received pluginapi.UsageRecord
			if err := json.Unmarshal(raw, &received); err != nil {
				t.Fatal(err)
			}
			if received.Detail.Breakdown == nil || !reflect.DeepEqual(received.Detail.Breakdown, mapped) {
				t.Fatalf("JSON round trip changed accounting: %s", raw)
			}
			if received.Detail.Breakdown.Input.CacheReadTokens != original.Input.CacheReadTokens ||
				received.Detail.Breakdown.Input.CacheWriteTokens != original.Input.CacheWriteTokens ||
				received.Detail.Breakdown.Output.ReasoningTokens != original.Output.ReasoningTokens {
				t.Fatalf("buckets changed: %+v", received.Detail.Breakdown)
			}
		})
	}
}
