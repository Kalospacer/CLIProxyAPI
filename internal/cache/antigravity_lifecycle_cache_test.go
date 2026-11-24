package cache

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// TestAntigravityLifecycleState 验证 Antigravity 会话轨迹生命周期的会话隔离、FNV-1a64 sessionId、
// 多轮步进、工具续接、缺会话优雅降级、失败保守处理、并发安全与 Home KV CAS 独立存储。
func TestAntigravityLifecycleState(t *testing.T) {
	cases := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "session_isolation_and_fnv1a64_session_id",
			run: func(t *testing.T) {
				ctx := context.Background()
				sA1 := BeginAntigravityLifecycle(ctx, "session-iso-A", "agent", "gemini-4-hydrogen-xhigh", nil)
				sB1 := BeginAntigravityLifecycle(ctx, "session-iso-B", "agent", "gemini-4-hydrogen-xhigh", nil)
				if sA1.SessionID == "" || sB1.SessionID == "" {
					t.Fatalf("可信会话应生成非空 sessionId: A=%q B=%q", sA1.SessionID, sB1.SessionID)
				}
				if sA1.SessionID == sB1.SessionID {
					t.Fatalf("不同会话不应共享 sessionId: %q", sA1.SessionID)
				}
				if sA1.SessionID == "-3750763034362895579" {
					t.Fatalf("非空会话不应退回空串 FNV 哈希: %q", sA1.SessionID)
				}
				if sA1.TrajectoryID == sB1.TrajectoryID {
					t.Fatalf("不同会话不应共享 trajectory_id: %q", sA1.TrajectoryID)
				}
				FinishAntigravityLifecycle(ctx, sA1, true, nil)
				sA2 := BeginAntigravityLifecycle(ctx, "session-iso-A", "agent", "gemini-4-hydrogen-xhigh", nil)
				if sA2.SessionID != sA1.SessionID || sA2.TrajectoryID != sA1.TrajectoryID {
					t.Fatalf("同一会话多轮应保持 sessionId 与 trajectory_id 稳定: turn1=(%s,%s) turn2=(%s,%s)", sA1.SessionID, sA1.TrajectoryID, sA2.SessionID, sA2.TrajectoryID)
				}
			},
		},
		{
			name: "multi_turn_and_tool_continuation_progression",
			run: func(t *testing.T) {
				ctx := context.Background()
				key := "session-multi-turn-1"
				t1 := BeginAntigravityLifecycle(ctx, key, "agent", "gemini-4-hydrogen-xhigh", nil)
				if !t1.HasRequestOrdinal || t1.RequestOrdinal != 0 {
					t.Fatalf("第 1 轮 ordinal = (%v, %d), want (true, 0)", t1.HasRequestOrdinal, t1.RequestOrdinal)
				}
				if !t1.HasLastStepIndex || t1.LastStepIndex != 0 {
					t.Fatalf("第 1 轮 last_step_index = (%v, %d), want (true, 0)", t1.HasLastStepIndex, t1.LastStepIndex)
				}
				if t1.LastExecutionID != "" {
					t.Fatalf("第 1 轮不应包含 last_execution_id: %q", t1.LastExecutionID)
				}
				if !strings.HasSuffix(t1.RequestID, "/1") {
					t.Fatalf("第 1 轮 requestId 应以 /1 结尾: %q", t1.RequestID)
				}
				FinishAntigravityLifecycle(ctx, t1, true, []string{"call_read_1"})

				// 第 2 轮带回 call_read_1 工具结果，应推进 ordinal 与步号并记录上一轮 execution_id。
				t2 := BeginAntigravityLifecycle(ctx, key, "agent", "gemini-4-hydrogen-xhigh", map[string]int{"call_read_1": 1})
				if !t2.HasRequestOrdinal || t2.RequestOrdinal != 1 {
					t.Fatalf("第 2 轮 ordinal = (%v, %d), want (true, 1)", t2.HasRequestOrdinal, t2.RequestOrdinal)
				}
				if !t2.HasLastStepIndex || t2.LastStepIndex < 2 {
					t.Fatalf("第 2 轮带工具结果时 last_step_index 应至少推进至 2: got=(%v, %d)", t2.HasLastStepIndex, t2.LastStepIndex)
				}
				if t2.LastExecutionID == "" {
					t.Fatal("第 2 轮应携带上一轮的 last_execution_id")
				}
				FinishAntigravityLifecycle(ctx, t2, true, nil)

				// 第 3 轮切到 Claude 模型后，used_claude / used_non_gemini_model 应置 true。
				t3 := BeginAntigravityLifecycle(ctx, key, "agent", "claude-sonnet-4-6", nil)
				if !t3.UsedClaude || !t3.UsedClaudeConservative || !t3.UsedNonGeminiModel {
					t.Fatalf("切到 Claude 后标记未更新: %+v", t3)
				}
			},
		},
		{
			name: "missing_session_gracefully_degrades_without_fabricating_steps",
			run: func(t *testing.T) {
				ctx := context.Background()
				st := BeginAntigravityLifecycle(ctx, "", "agent", "gemini-3.8-flash-high", nil)
				if !st.Degraded {
					t.Fatal("缺少会话标识时必须标记 Degraded=true")
				}
				if st.SessionID != "" {
					t.Fatalf("缺少会话标识时必须省略 sessionId，got=%q", st.SessionID)
				}
				if st.HasRequestOrdinal || st.HasLastStepIndex || st.LastExecutionID != "" {
					t.Fatalf("缺会话降级时不得编造 ordinal/last_step_index/last_execution_id: %+v", st)
				}
				if !st.UsedClaudeConservative || !st.UsedNonGeminiModel {
					t.Fatalf("缺会话降级时保守标记应置 true: %+v", st)
				}
				parts := strings.Split(st.RequestID, "/")
				if len(parts) != 2 || parts[0] != "agent" {
					t.Fatalf("缺会话时 requestId 应退化为 agent/<uuid>，got=%q", st.RequestID)
				}
				FinishAntigravityLifecycle(ctx, st, true, nil)
			},
		},
		{
			name: "failed_turn_clears_uncertain_step_conservatively",
			run: func(t *testing.T) {
				ctx := context.Background()
				key := "session-failure-conservative"
				t1 := BeginAntigravityLifecycle(ctx, key, "agent", "gemini-4-carbon-high", nil)
				// 上游失败未完成时不得伪装推进步号。
				FinishAntigravityLifecycle(ctx, t1, false, nil)
				t2 := BeginAntigravityLifecycle(ctx, key, "agent", "gemini-4-carbon-high", nil)
				if t2.HasLastStepIndex {
					t.Fatalf("前序轮次失败后不应编造确定的 last_step_index: %+v", t2)
				}
				if !t2.UsedClaudeConservative {
					t.Fatalf("状态断档后保守标记应置 true: %+v", t2)
				}
			},
		},
		{
			name: "concurrent_begin_finish_is_race_free",
			run: func(t *testing.T) {
				ctx := context.Background()
				key := "session-concurrent-1"
				var wg sync.WaitGroup
				for i := 0; i < 8; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						st := BeginAntigravityLifecycle(ctx, key, "agent", "gemini-4-argon-high", nil)
						FinishAntigravityLifecycle(ctx, st, true, nil)
					}()
				}
				wg.Wait()
				final := BeginAntigravityLifecycle(ctx, key, "agent", "gemini-4-argon-high", nil)
				if final.TrajectoryID == "" {
					t.Fatal("并发后轨迹 ID 丢失")
				}
			},
		},
		{
			name: "home_kv_separate_key_and_error_degrade",
			run: func(t *testing.T) {
				fakeKV := newFakeAntigravityReasoningReplayKVClient()
				prevKV := currentAntigravityLifecycleKVClient
				currentAntigravityLifecycleKVClient = func() (antigravityReasoningReplayKVClient, bool, error) {
					return fakeKV, true, nil
				}
				t.Cleanup(func() { currentAntigravityLifecycleKVClient = prevKV })
				ctx := context.Background()
				key := "session-home-kv-1"

				t1 := BeginAntigravityLifecycle(ctx, key, "agent", "gemini-4-neon-xhigh", nil)
				FinishAntigravityLifecycle(ctx, t1, true, nil)

				fakeKV.mu.Lock()
				var lifecycleKeys int
				for k := range fakeKV.values {
					if strings.HasPrefix(k, "cpa:antigravity:lifecycle:") {
						lifecycleKeys++
					}
					if strings.Contains(k, "reasoning_replay") {
						t.Errorf("生命周期状态污染了 reasoning replay 缓存键: %s", k)
					}
				}
				fakeKV.mu.Unlock()
				if lifecycleKeys != 1 {
					t.Fatalf("Home KV 中应存在 1 条独立生命周期记录，got=%d", lifecycleKeys)
				}

				// 当 Home KV CAS 故障时应优雅降级，不得编造步号或 panic。
				fakeKV.mu.Lock()
				fakeKV.casErr = errors.New("simulated kv outage")
				fakeKV.mu.Unlock()
				degraded := BeginAntigravityLifecycle(ctx, key, "agent", "gemini-4-neon-xhigh", nil)
				if !degraded.Degraded || degraded.HasLastStepIndex || degraded.HasRequestOrdinal {
					t.Fatalf("Home KV 异常时应降级且省略不确定步号: %+v", degraded)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}
