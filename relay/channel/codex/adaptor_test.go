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

func ptrUint(v uint) *uint {
	return &v
}

func ptrFloat64(v float64) *float64 {
	return &v
}
