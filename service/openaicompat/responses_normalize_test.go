package openaicompat

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestNormalizeResponsesSystemMessagesMovesGuidanceToInstructions(t *testing.T) {
	t.Parallel()

	req := &dto.OpenAIResponsesRequest{
		Model:        "gpt-5.5",
		Instructions: mustRawMessage(t, `"existing instruction"`),
		Input: mustRawMessage(t, `[
			{"role":"system","content":"system instruction"},
			{"role":"user","content":"hello"},
			{"type":"message","role":"developer","content":[{"type":"input_text","text":"developer instruction"}]},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]}
		]`),
	}

	err := NormalizeResponsesSystemMessages(req)
	require.NoError(t, err)

	var instructions string
	require.NoError(t, common.Unmarshal(req.Instructions, &instructions))
	require.Equal(t, "existing instruction\n\nsystem instruction\n\ndeveloper instruction", instructions)

	var input []map[string]any
	require.NoError(t, common.Unmarshal(req.Input, &input))
	require.Len(t, input, 2)
	require.Equal(t, "user", input[0]["role"])
	require.Equal(t, "assistant", input[1]["role"])
}

func mustRawMessage(t *testing.T, value string) []byte {
	t.Helper()
	return []byte(value)
}
