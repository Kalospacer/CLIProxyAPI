package pluginhost

import (
	"context"
	"testing"

	coreusage "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func completeUsageBreakdownFixture() coreusage.TokenBreakdown {
	return coreusage.TokenBreakdown{
		SchemaVersion: coreusage.TokenAccountingSchemaVersion,
		Quality:       coreusage.TokenAccountingQualityComplete,
		TotalTokens:   248,
		Input: coreusage.TokenInputBreakdown{
			TotalTokens:    90,
			UncachedTokens: 90,
		},
		Output: coreusage.TokenOutputBreakdown{
			TotalTokens:        158,
			NonReasoningTokens: 21,
			ReasoningTokens:    137,
		},
	}
}

func TestUsageAdapterForwardsHostTokenBreakdown(t *testing.T) {
	var got *pluginapi.UsageTokenBreakdown
	plugin := usagePluginFunc(func(_ context.Context, record pluginapi.UsageRecord) {
		got = record.Detail.Breakdown
	})
	host := newHostWithRecords(capabilityRecord{
		id: "usage-breakdown",
		plugin: pluginapi.Plugin{Capabilities: pluginapi.Capabilities{
			UsagePlugin: plugin,
		}},
	})
	adapter := &usageAdapter{host: host, pluginID: "usage-breakdown"}

	breakdown := completeUsageBreakdownFixture()
	if !breakdown.Valid() {
		t.Fatalf("fixture breakdown does not validate: %+v", breakdown)
	}
	// 插件执行器的记录按 mirasim␣+␣executorAdapter 命名，名字里没有协议信息，
	// 所以只有过界的 breakdown 能告诉计费插件这次用的是哪套口径。
	adapter.HandleUsage(context.Background(), coreusage.Record{
		Provider: "mirasim", ExecutorType: "executorAdapter", Model: "kimi-k3",
		Detail: coreusage.Detail{
			InputTokens: 90, OutputTokens: 158, ReasoningTokens: 137, TotalTokens: 248,
			TokenBreakdown: breakdown,
		},
	})
	if got == nil {
		t.Fatal("plugin did not receive the host token breakdown")
	}
	if got.SchemaVersion != coreusage.TokenAccountingSchemaVersion || got.Quality != "complete" || got.TotalTokens != 248 {
		t.Fatalf("top level breakdown = %+v", got)
	}
	if got.Input.TotalTokens != 90 || got.Input.UncachedTokens != 90 || got.Input.CacheReadTokens != 0 || got.Input.CacheWriteTokens != 0 {
		t.Fatalf("input breakdown = %+v", got.Input)
	}
	if got.Output.TotalTokens != 158 || got.Output.NonReasoningTokens != 21 || got.Output.ReasoningTokens != 137 {
		t.Fatalf("output breakdown = %+v", got.Output)
	}
	if got.UnclassifiedTokens != 0 {
		t.Fatalf("unclassified tokens = %d", got.UnclassifiedTokens)
	}
}

func TestUsageAdapterOmitsUnvalidatedHostTokenBreakdown(t *testing.T) {
	var got *pluginapi.UsageTokenBreakdown
	plugin := usagePluginFunc(func(_ context.Context, record pluginapi.UsageRecord) {
		got = record.Detail.Breakdown
	})
	host := newHostWithRecords(capabilityRecord{
		id: "usage-breakdown-invalid",
		plugin: pluginapi.Plugin{Capabilities: pluginapi.Capabilities{
			UsagePlugin: plugin,
		}},
	})
	adapter := &usageAdapter{host: host, pluginID: "usage-breakdown-invalid"}

	// 零值与自相矛盾的 breakdown 都不过界：插件保留自己的兜底判定。
	inconsistent := coreusage.TokenBreakdown{
		SchemaVersion: coreusage.TokenAccountingSchemaVersion,
		Quality:       coreusage.TokenAccountingQualityComplete,
		TotalTokens:   999,
	}
	for _, detail := range []coreusage.Detail{
		{TotalTokens: 10},
		{InputTokens: 5, OutputTokens: 5, TotalTokens: 999, TokenBreakdown: inconsistent},
	} {
		got = nil
		adapter.HandleUsage(context.Background(), coreusage.Record{Provider: "mirasim", ExecutorType: "executorAdapter", Detail: detail})
		if got != nil {
			t.Fatalf("unvalidated breakdown reached the plugin: %+v", got)
		}
	}
}
