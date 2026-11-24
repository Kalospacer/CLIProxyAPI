package cache

import (
	"context"
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MoeForge/moego/json"
	"github.com/google/uuid"
	homekv "github.com/router-for-me/CLIProxyAPI/v8/internal/home"
	log "github.com/sirupsen/logrus"
)

const (
	antigravityLifecycleVersion     uint8 = 1
	antigravityLifecycleTTL               = 1 * time.Hour
	antigravityLifecycleMaxEntries        = 10240
	antigravityLifecycleEvictBatch        = 128
	antigravityLifecycleCASRetries        = 8
	antigravityLifecycleKVKeyPrefix       = "cpa:antigravity:lifecycle:v1:"
)

// AntigravityLifecycleState 表示单个可信会话的最小轨迹持久化状态，以及当前请求计算出的标签快照。
// 与 reasoning replay 缓存完全独立存储，不保存任何对话正文。
type AntigravityLifecycleState struct {
	SessionKey             string `json:"-"`
	TurnID                 string `json:"-"`
	SessionID              string `json:"-"`
	RequestID              string `json:"-"`
	CascadeID              string `json:"cascade_id"`
	TrajectoryID           string `json:"trajectory_id"`
	RequestOrdinal         uint64 `json:"-"`
	HasRequestOrdinal      bool   `json:"-"`
	LastStepIndex          uint64 `json:"-"`
	HasLastStepIndex       bool   `json:"-"`
	LastExecutionID        string `json:"last_execution_id,omitempty"`
	UsedClaude             bool   `json:"used_claude"`
	UsedClaudeConservative bool   `json:"used_claude_conservative"`
	UsedNonGeminiModel     bool   `json:"used_non_gemini_model"`
	Degraded               bool   `json:"-"`

	// 以下为跨轮次轨迹演化所需的最小持久化状态字段。
	Version       uint8          `json:"version"`
	NextOrdinal   uint64         `json:"next_ordinal"`
	OrdinalKnown  bool           `json:"ordinal_known"`
	NextStep      uint64         `json:"next_step"`
	StepKnown     bool           `json:"step_known"`
	ActiveTurnID  string         `json:"active_turn_id,omitempty"`
	ActiveInput   uint64         `json:"active_input_step"`
	ActiveHasStep bool           `json:"active_has_step"`
	ActiveDone    bool           `json:"active_done"`
	PendingTools  map[string]int `json:"pending_tools,omitempty"`
	UpdatedAtUnix int64          `json:"updated_at_unix"`
}

func (s AntigravityLifecycleState) clone() AntigravityLifecycleState {
	out := s
	if len(s.PendingTools) > 0 {
		out.PendingTools = make(map[string]int, len(s.PendingTools))
		for k, v := range s.PendingTools {
			out.PendingTools[k] = v
		}
	}
	return out
}

func (s AntigravityLifecycleState) withDegradedSnapshot(requestType, sessionID string, isClaude, isGemini bool) AntigravityLifecycleState {
	cascade := strings.TrimSpace(s.CascadeID)
	if cascade == "" {
		cascade = uuid.NewString()
	}
	trajectory := strings.TrimSpace(s.TrajectoryID)
	if trajectory == "" {
		trajectory = uuid.NewString()
	}
	if requestType == "" {
		requestType = "agent"
	}
	return AntigravityLifecycleState{
		SessionKey:             s.SessionKey,
		TurnID:                 uuid.NewString(),
		SessionID:              sessionID,
		RequestID:              requestType + "/" + uuid.NewString(),
		CascadeID:              cascade,
		TrajectoryID:           trajectory,
		HasRequestOrdinal:      false,
		HasLastStepIndex:       false,
		LastExecutionID:        "",
		UsedClaude:             s.UsedClaude || isClaude,
		UsedClaudeConservative: true,
		UsedNonGeminiModel:     true,
		Degraded:               true,
		Version:                antigravityLifecycleVersion,
	}
}

var (
	antigravityLifecycleMu      sync.Mutex
	antigravityLifecycleEntries = make(map[string]AntigravityLifecycleState)
	// 独立于 reasoning replay 的 KV 钩子，避免共享测试拦截点或相互干扰。
	currentAntigravityLifecycleKVClient = func() (antigravityReasoningReplayKVClient, bool, error) {
		return homekv.CurrentKVClient()
	}
)

// BeginAntigravityLifecycle 在请求发送前读取或初始化会话轨迹，并生成 sessionId、requestId 和 labels 所需字段。
// 缺少可信 sessionKey 或状态存储异常时优雅降级：退回单次 requestId、省略不确定步号、保守标记置 true。
func BeginAntigravityLifecycle(ctx context.Context, sessionKey, requestType, modelName string, toolResultCounts map[string]int) AntigravityLifecycleState {
	if ctx == nil {
		ctx = context.Background()
	}
	requestType = strings.TrimSpace(requestType)
	if requestType == "" {
		requestType = "agent"
	}
	lowerModel := strings.ToLower(strings.TrimSpace(modelName))
	isClaude := strings.Contains(lowerModel, "claude")
	isGemini := strings.Contains(lowerModel, "gemini")

	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return (AntigravityLifecycleState{}).withDegradedSnapshot(requestType, "", isClaude, isGemini)
	}

	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(sessionKey))
	sessionID := strconv.FormatInt(int64(hasher.Sum64()), 10)
	now := time.Now()
	nowUnix := now.Unix()
	nowMilli := now.UnixMilli()
	turnID := uuid.NewString()

	applyBegin := func(existing *AntigravityLifecycleState) (AntigravityLifecycleState, AntigravityLifecycleState) {
		var st AntigravityLifecycleState
		fresh := existing == nil || existing.Version != antigravityLifecycleVersion ||
			(existing.UpdatedAtUnix > 0 && now.Sub(time.Unix(existing.UpdatedAtUnix, 0)) > antigravityLifecycleTTL)
		if fresh {
			st = AntigravityLifecycleState{
				Version:                antigravityLifecycleVersion,
				CascadeID:              sessionKey,
				TrajectoryID:           uuid.NewString(),
				NextOrdinal:            0,
				OrdinalKnown:           true,
				NextStep:               0,
				StepKnown:              true,
				UsedClaude:             isClaude,
				UsedClaudeConservative: isClaude,
				UsedNonGeminiModel:     !isGemini,
				ActiveDone:             true,
			}
		} else {
			st = existing.clone()
			if !st.ActiveDone {
				// 上一轮未正常收尾，放弃推断确定步号并开启保守标记，绝不编造步进。
				st.StepKnown = false
				st.PendingTools = nil
				st.UsedClaudeConservative = true
			} else if st.StepKnown {
				if len(st.PendingTools) > 0 {
					consumedAny := false
					for toolID, priorCount := range st.PendingTools {
						gotCount := toolResultCounts[toolID]
						if gotCount < priorCount {
							st.StepKnown = false
							st.PendingTools = nil
							st.UsedClaudeConservative = true
							break
						}
						if gotCount > priorCount {
							st.NextStep++
							delete(st.PendingTools, toolID)
							consumedAny = true
						}
					}
					if st.StepKnown && !consumedAny {
						// 用户跳过工具结果直接发新输入时按新用户消息推进 1 步。
						st.NextStep++
					}
				} else {
					// 正常多轮文本续聊：为本轮新输入推进 1 步。
					st.NextStep++
				}
			}
			st.UsedClaude = st.UsedClaude || isClaude
			st.UsedClaudeConservative = st.UsedClaudeConservative || isClaude
			st.UsedNonGeminiModel = st.UsedNonGeminiModel || !isGemini
		}

		ordinal := st.NextOrdinal
		hasOrdinal := st.OrdinalKnown
		if st.OrdinalKnown {
			st.NextOrdinal++
		}
		inputStep := st.NextStep
		hasStep := st.StepKnown

		st.ActiveTurnID = turnID
		st.ActiveInput = inputStep
		st.ActiveHasStep = hasStep
		st.ActiveDone = false
		st.UpdatedAtUnix = nowUnix

		snapshot := st.clone()
		snapshot.SessionKey = sessionKey
		snapshot.TurnID = turnID
		snapshot.SessionID = sessionID
		snapshot.RequestOrdinal = ordinal
		snapshot.HasRequestOrdinal = hasOrdinal
		snapshot.LastStepIndex = inputStep
		snapshot.HasLastStepIndex = hasStep
		if hasStep {
			snapshot.RequestID = fmt.Sprintf("%s/%s/%d/%s/%d", requestType, st.CascadeID, nowMilli, st.TrajectoryID, inputStep+1)
		} else {
			snapshot.RequestID = requestType + "/" + uuid.NewString()
		}
		return st, snapshot
	}

	if client, homeMode, errClient := currentAntigravityLifecycleKVClient(); homeMode {
		if errClient != nil {
			log.Errorf("home kv antigravity lifecycle begin failed: %v", errClient)
			return (AntigravityLifecycleState{SessionKey: sessionKey, CascadeID: sessionKey}).withDegradedSnapshot(requestType, sessionID, isClaude, isGemini)
		}
		kvKey := antigravityLifecycleKVKeyPrefix + sessionKey
		for attempt := 0; attempt < antigravityLifecycleCASRetries; attempt++ {
			raw, found, errGet := client.KVGet(ctx, kvKey)
			if errGet != nil {
				log.Errorf("home kv antigravity lifecycle get failed: %v", errGet)
				return (AntigravityLifecycleState{SessionKey: sessionKey, CascadeID: sessionKey}).withDegradedSnapshot(requestType, sessionID, isClaude, isGemini)
			}
			var loaded *AntigravityLifecycleState
			if found && len(raw) > 0 {
				var parsed AntigravityLifecycleState
				if errUnmarshal := json.Unmarshal(raw, &parsed); errUnmarshal == nil && parsed.Version == antigravityLifecycleVersion {
					loaded = &parsed
				}
			}
			nextState, snapshot := applyBegin(loaded)
			encoded, errMarshal := json.Marshal(nextState)
			if errMarshal != nil {
				return (AntigravityLifecycleState{SessionKey: sessionKey, CascadeID: sessionKey}).withDegradedSnapshot(requestType, sessionID, isClaude, isGemini)
			}
			swapped, errCAS := client.KVCompareAndSwap(ctx, kvKey, raw, found, encoded, antigravityLifecycleTTL)
			if errCAS != nil {
				log.Errorf("home kv antigravity lifecycle cas failed: %v", errCAS)
				return (AntigravityLifecycleState{SessionKey: sessionKey, CascadeID: sessionKey}).withDegradedSnapshot(requestType, sessionID, isClaude, isGemini)
			}
			if swapped {
				return snapshot
			}
		}
		return (AntigravityLifecycleState{SessionKey: sessionKey, CascadeID: sessionKey}).withDegradedSnapshot(requestType, sessionID, isClaude, isGemini)
	}

	antigravityLifecycleMu.Lock()
	defer antigravityLifecycleMu.Unlock()

	var loaded *AntigravityLifecycleState
	if existing, ok := antigravityLifecycleEntries[sessionKey]; ok {
		copied := existing.clone()
		loaded = &copied
	}
	nextState, snapshot := applyBegin(loaded)
	antigravityLifecycleEntries[sessionKey] = nextState

	if len(antigravityLifecycleEntries) > antigravityLifecycleMaxEntries {
		type keyAge struct {
			k string
			t int64
		}
		list := make([]keyAge, 0, len(antigravityLifecycleEntries))
		for k, v := range antigravityLifecycleEntries {
			list = append(list, keyAge{k: k, t: v.UpdatedAtUnix})
		}
		sort.Slice(list, func(i, j int) bool { return list[i].t < list[j].t })
		limit := min(antigravityLifecycleEvictBatch, len(list))
		for i := 0; i < limit; i++ {
			delete(antigravityLifecycleEntries, list[i].k)
		}
	}
	return snapshot
}

// FinishAntigravityLifecycle 按请求最终成败和模型发出的工具调用 ID 更新轨迹状态。
// 若请求失败或取消，不推进成功步号，使下一轮能够安全保守降级。
func FinishAntigravityLifecycle(ctx context.Context, state AntigravityLifecycleState, succeeded bool, emittedToolCallIDs []string) {
	sessionKey := strings.TrimSpace(state.SessionKey)
	if sessionKey == "" || state.Degraded || strings.TrimSpace(state.TurnID) == "" {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	nowUnix := time.Now().Unix()

	applyFinish := func(current *AntigravityLifecycleState) (AntigravityLifecycleState, bool) {
		if current == nil || current.Version != antigravityLifecycleVersion || current.ActiveTurnID != state.TurnID {
			return AntigravityLifecycleState{}, false
		}
		next := current.clone()
		next.UpdatedAtUnix = nowUnix
		if !succeeded {
			next.ActiveDone = false
			next.StepKnown = false
			next.PendingTools = nil
			next.UsedClaudeConservative = true
			return next, true
		}

		next.ActiveDone = true
		next.LastExecutionID = state.TurnID
		if next.ActiveHasStep {
			next.NextStep = next.ActiveInput + 1
			next.StepKnown = true
		} else {
			next.StepKnown = false
		}

		if next.StepKnown && len(emittedToolCallIDs) > 0 {
			if next.PendingTools == nil {
				next.PendingTools = make(map[string]int, len(emittedToolCallIDs))
			}
			for _, id := range emittedToolCallIDs {
				trimmed := strings.TrimSpace(id)
				if trimmed == "" {
					continue
				}
				if _, exists := next.PendingTools[trimmed]; !exists {
					baseline := int(0)
					next.PendingTools[trimmed] = baseline
				}
			}
		} else if !next.StepKnown {
			next.PendingTools = nil
		}
		return next, true
	}

	if client, homeMode, errClient := currentAntigravityLifecycleKVClient(); homeMode {
		if errClient != nil {
			log.Errorf("home kv antigravity lifecycle finish failed: %v", errClient)
			return
		}
		kvKey := antigravityLifecycleKVKeyPrefix + sessionKey
		for attempt := 0; attempt < antigravityLifecycleCASRetries; attempt++ {
			raw, found, errGet := client.KVGet(ctx, kvKey)
			if errGet != nil || !found || len(raw) == 0 {
				return
			}
			var parsed AntigravityLifecycleState
			if errUnmarshal := json.Unmarshal(raw, &parsed); errUnmarshal != nil {
				return
			}
			next, ok := applyFinish(&parsed)
			if !ok {
				return
			}
			encoded, errMarshal := json.Marshal(next)
			if errMarshal != nil {
				return
			}
			swapped, errCAS := client.KVCompareAndSwap(ctx, kvKey, raw, true, encoded, antigravityLifecycleTTL)
			if errCAS != nil || swapped {
				return
			}
		}
		return
	}

	antigravityLifecycleMu.Lock()
	defer antigravityLifecycleMu.Unlock()
	existing, ok := antigravityLifecycleEntries[sessionKey]
	if !ok {
		return
	}
	if next, changed := applyFinish(&existing); changed {
		antigravityLifecycleEntries[sessionKey] = next
	}
}
