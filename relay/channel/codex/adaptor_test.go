package codex

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/stretchr/testify/require"
)

func TestConvertOpenAIResponsesRequest_SetsStoreFalseForResponses(t *testing.T) {
	t.Parallel()

	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	request := dto.OpenAIResponsesRequest{
		Model:           "gpt-5.4",
		Input:           json.RawMessage(`"hello"`),
		MaxOutputTokens: ptrUint(128),
		Temperature:     ptrFloat64(0.7),
	}

	converted, err := adaptor.ConvertOpenAIResponsesRequest(nil, info, request)
	require.NoError(t, err)

	data, err := common.Marshal(converted)
	require.NoError(t, err)

	var payload map[string]any
	err = common.Unmarshal(data, &payload)
	require.NoError(t, err)
	require.Equal(t, false, payload["store"])
	require.Equal(t, true, payload["stream"])
	_, hasMaxOutputTokens := payload["max_output_tokens"]
	require.False(t, hasMaxOutputTokens)
	_, hasTemperature := payload["temperature"]
	require.False(t, hasTemperature)
	require.True(t, info.IsStream)
}

func TestConvertOpenAIResponsesRequest_SetsStoreFalseForResponsesCompact(t *testing.T) {
	t.Parallel()

	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeResponsesCompact,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	request := dto.OpenAIResponsesRequest{
		Model:              "compact-2026-01-12",
		Input:              json.RawMessage(`[{"role":"user","content":[{"type":"input_text","text":"hello"}]}]`),
		Instructions:       json.RawMessage(`"compact please"`),
		PreviousResponseID: "resp_123",
	}

	converted, err := adaptor.ConvertOpenAIResponsesRequest(nil, info, request)
	require.NoError(t, err)

	data, err := common.Marshal(converted)
	require.NoError(t, err)

	var payload map[string]any
	err = common.Unmarshal(data, &payload)
	require.NoError(t, err)
	require.Equal(t, false, payload["store"])
	require.Equal(t, true, payload["stream"])
	require.Equal(t, "resp_123", payload["previous_response_id"])
	require.Equal(t, "compact please", payload["instructions"])
	require.False(t, info.IsStream)
}

func TestConvertOpenAIResponsesRequest_PreservesCompactContextFields(t *testing.T) {
	t.Parallel()

	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeResponsesCompact,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	request := dto.OpenAIResponsesRequest{
		Model:                "compact-2026-01-12",
		Input:                json.RawMessage(`[{"type":"message","role":"user","content":[{"type":"input_text","text":"keep context"}]}]`),
		Include:              json.RawMessage(`["reasoning.encrypted_content"]`),
		Conversation:         json.RawMessage(`"none"`),
		ContextManagement:    json.RawMessage(`{"truncation":"auto"}`),
		Metadata:             json.RawMessage(`{"project":"pdep"}`),
		ParallelToolCalls:    json.RawMessage(`true`),
		PromptCacheKey:       json.RawMessage(`"cache-key"`),
		PromptCacheRetention: json.RawMessage(`"24h"`),
		SafetyIdentifier:     json.RawMessage(`"user-123"`),
		Text:                 json.RawMessage(`{"format":{"type":"text"},"verbosity":"medium"}`),
		ToolChoice:           json.RawMessage(`"auto"`),
		Tools:                json.RawMessage(`[{"type":"function","name":"shell"}]`),
		Truncation:           json.RawMessage(`"disabled"`),
		User:                 json.RawMessage(`"codex"`),
	}

	converted, err := adaptor.ConvertOpenAIResponsesRequest(nil, info, request)
	require.NoError(t, err)

	data, err := common.Marshal(converted)
	require.NoError(t, err)

	var payload map[string]any
	err = common.Unmarshal(data, &payload)
	require.NoError(t, err)
	require.Equal(t, []any{"reasoning.encrypted_content"}, payload["include"])
	require.Equal(t, "none", payload["conversation"])
	require.Equal(t, map[string]any{"truncation": "auto"}, payload["context_management"])
	require.Equal(t, map[string]any{"project": "pdep"}, payload["metadata"])
	require.Equal(t, true, payload["parallel_tool_calls"])
	require.Equal(t, "cache-key", payload["prompt_cache_key"])
	require.Equal(t, "24h", payload["prompt_cache_retention"])
	require.Equal(t, "user-123", payload["safety_identifier"])
	require.Equal(t, map[string]any{"format": map[string]any{"type": "text"}, "verbosity": "medium"}, payload["text"])
	require.Equal(t, "auto", payload["tool_choice"])
	require.Equal(t, []any{map[string]any{"type": "function", "name": "shell"}}, payload["tools"])
	require.Equal(t, "disabled", payload["truncation"])
	require.Equal(t, "codex", payload["user"])
}

func ptrUint(v uint) *uint {
	return &v
}

func ptrFloat64(v float64) *float64 {
	return &v
}
