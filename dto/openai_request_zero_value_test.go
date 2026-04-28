package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGeneralOpenAIRequestPreserveExplicitZeroValues(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-4.1",
		"stream":false,
		"max_tokens":0,
		"max_completion_tokens":0,
		"top_p":0,
		"top_k":0,
		"n":0,
		"frequency_penalty":0,
		"presence_penalty":0,
		"seed":0,
		"logprobs":false,
		"top_logprobs":0,
		"dimensions":0,
		"return_images":false,
		"return_related_questions":false
	}`)

	var req GeneralOpenAIRequest
	err := common.Unmarshal(raw, &req)
	require.NoError(t, err)

	encoded, err := common.Marshal(req)
	require.NoError(t, err)

	require.True(t, gjson.GetBytes(encoded, "stream").Exists())
	require.True(t, gjson.GetBytes(encoded, "max_tokens").Exists())
	require.True(t, gjson.GetBytes(encoded, "max_completion_tokens").Exists())
	require.True(t, gjson.GetBytes(encoded, "top_p").Exists())
	require.True(t, gjson.GetBytes(encoded, "top_k").Exists())
	require.True(t, gjson.GetBytes(encoded, "n").Exists())
	require.True(t, gjson.GetBytes(encoded, "frequency_penalty").Exists())
	require.True(t, gjson.GetBytes(encoded, "presence_penalty").Exists())
	require.True(t, gjson.GetBytes(encoded, "seed").Exists())
	require.True(t, gjson.GetBytes(encoded, "logprobs").Exists())
	require.True(t, gjson.GetBytes(encoded, "top_logprobs").Exists())
	require.True(t, gjson.GetBytes(encoded, "dimensions").Exists())
	require.True(t, gjson.GetBytes(encoded, "return_images").Exists())
	require.True(t, gjson.GetBytes(encoded, "return_related_questions").Exists())
}

func TestOpenAIResponsesRequestPreserveExplicitZeroValues(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-4.1",
		"max_output_tokens":0,
		"max_tool_calls":0,
		"stream":false,
		"top_p":0
	}`)

	var req OpenAIResponsesRequest
	err := common.Unmarshal(raw, &req)
	require.NoError(t, err)

	encoded, err := common.Marshal(req)
	require.NoError(t, err)

	require.True(t, gjson.GetBytes(encoded, "max_output_tokens").Exists())
	require.True(t, gjson.GetBytes(encoded, "max_tool_calls").Exists())
	require.True(t, gjson.GetBytes(encoded, "stream").Exists())
	require.True(t, gjson.GetBytes(encoded, "top_p").Exists())
}

func TestOpenAIResponsesCompactionRequestPreserveExplicitStreamValue(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-5.4-openai-compact",
		"input":[{"role":"user","content":"hi"}],
		"stream":true
	}`)

	var req OpenAIResponsesCompactionRequest
	err := common.Unmarshal(raw, &req)
	require.NoError(t, err)

	encoded, err := common.Marshal(req)
	require.NoError(t, err)

	require.True(t, gjson.GetBytes(encoded, "stream").Exists())
	require.True(t, gjson.GetBytes(encoded, "stream").Bool())
}

func TestOpenAIResponsesCompactionRequestPreservesResponsesContextFields(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-5.5-openai-compact",
		"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"keep context"}]}],
		"instructions":"preserve project instructions",
		"include":["reasoning.encrypted_content"],
		"conversation":"none",
		"context_management":{"truncation":"auto"},
		"metadata":{"project":"pdep"},
		"parallel_tool_calls":true,
		"previous_response_id":"resp_previous",
		"reasoning":{"effort":"high","summary":"auto"},
		"prompt_cache_key":"cache-key",
		"prompt_cache_retention":"24h",
		"safety_identifier":"user-123",
		"text":{"format":{"type":"text"},"verbosity":"medium"},
		"tool_choice":"auto",
		"tools":[{"type":"function","name":"shell"}],
		"truncation":"disabled",
		"user":"codex"
	}`)

	var req OpenAIResponsesCompactionRequest
	err := common.Unmarshal(raw, &req)
	require.NoError(t, err)

	encoded, err := common.Marshal(req)
	require.NoError(t, err)

	for _, path := range []string{
		"include.0",
		"conversation",
		"context_management.truncation",
		"metadata.project",
		"parallel_tool_calls",
		"previous_response_id",
		"reasoning.effort",
		"prompt_cache_key",
		"prompt_cache_retention",
		"safety_identifier",
		"text.verbosity",
		"tool_choice",
		"tools.0.name",
		"truncation",
		"user",
	} {
		require.True(t, gjson.GetBytes(encoded, path).Exists(), "missing compact request field %s in %s", path, string(encoded))
	}
}
