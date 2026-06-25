package openai

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestConvertOpenAIResponsesRequestRemovesReasoningInputWhenChannelSettingEnabled(t *testing.T) {
	t.Parallel()

	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelOtherSettings: dto.ChannelOtherSettings{
				RemoveResponsesReasoningInput: true,
			},
		},
	}
	request := dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]},
			{"type":"reasoning","id":"rs_123","encrypted_content":"stale"},
			{"type":"reasoning_summary","id":"rs_456","summary":[]},
			{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{}"}
		]`),
	}

	converted, err := adaptor.ConvertOpenAIResponsesRequest(nil, info, request)
	require.NoError(t, err)

	data, err := common.Marshal(converted)
	require.NoError(t, err)

	var payload map[string]json.RawMessage
	require.NoError(t, common.Unmarshal(data, &payload))

	var input []map[string]any
	require.NoError(t, common.Unmarshal(payload["input"], &input))
	require.Len(t, input, 2)
	require.Equal(t, "message", input[0]["type"])
	require.Equal(t, "function_call", input[1]["type"])
}

func TestConvertOpenAIResponsesRequestKeepsReasoningInputByDefault(t *testing.T) {
	t.Parallel()

	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{}
	request := dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]},
			{"type":"reasoning","id":"rs_123","encrypted_content":"stale"}
		]`),
	}

	converted, err := adaptor.ConvertOpenAIResponsesRequest(nil, info, request)
	require.NoError(t, err)

	data, err := common.Marshal(converted)
	require.NoError(t, err)

	var payload map[string]json.RawMessage
	require.NoError(t, common.Unmarshal(data, &payload))

	var input []map[string]any
	require.NoError(t, common.Unmarshal(payload["input"], &input))
	require.Len(t, input, 2)
	require.Equal(t, "reasoning", input[1]["type"])
}
