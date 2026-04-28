package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newResponsesCompactTestContext(body string) (*gin.Context, *httptest.ResponseRecorder, *http.Response) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
	return c, recorder, resp
}

func TestOaiResponsesCompactionHandler_AggregatesResponsesStreamBody(t *testing.T) {
	t.Parallel()

	body := strings.Join([]string{
		"event: response.created",
		`data: {"type":"response.created","response":{"id":"resp_123","object":"response","created_at":1774243071}}`,
		"event: response.completed",
		`data: {"type":"response.completed","response":{"id":"resp_123","object":"response","created_at":1774243072,"output":[{"type":"message","id":"msg_123","status":"completed","role":"assistant","content":[{"type":"output_text","text":"done","annotations":[]}]}],"usage":{"input_tokens":12,"input_tokens_details":{"cached_tokens":0},"output_tokens":6,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":18}}}`,
		"data: [DONE]",
	}, "\n") + "\n"

	c, recorder, resp := newResponsesCompactTestContext(body)

	usage, compactErr := OaiResponsesCompactionHandler(c, resp)
	require.Nil(t, compactErr)
	require.NotNil(t, usage)
	require.Equal(t, 12, usage.PromptTokens)
	require.Equal(t, 6, usage.CompletionTokens)
	require.Equal(t, 18, usage.TotalTokens)
	require.Contains(t, recorder.Body.String(), `"id":"resp_123"`)
	require.Contains(t, recorder.Body.String(), `"total_tokens":18`)
}

func TestOaiResponsesCompactionHandler_PreservesCompactionSummaryEncryptedContent(t *testing.T) {
	t.Parallel()

	body := strings.Join([]string{
		"event: response.completed",
		`data: {"type":"response.completed","response":{"id":"resp_123","object":"response","created_at":1774243072,"output":[{"type":"compaction_summary","encrypted_content":"encrypted-summary"}],"usage":{"input_tokens":12,"output_tokens":6,"total_tokens":18}}}`,
		"data: [DONE]",
	}, "\n") + "\n"

	c, recorder, resp := newResponsesCompactTestContext(body)

	usage, compactErr := OaiResponsesCompactionHandler(c, resp)
	require.Nil(t, compactErr)
	require.NotNil(t, usage)
	require.Contains(t, recorder.Body.String(), `"type":"compaction_summary"`)
	require.Contains(t, recorder.Body.String(), `"encrypted_content":"encrypted-summary"`)
}

func TestOaiResponsesCompactionHandler_ReturnsStreamErrorBeforeWrite(t *testing.T) {
	t.Parallel()

	body := strings.Join([]string{
		"event: response.failed",
		`data: {"type":"response.failed","response":{"error":{"message":"We're currently experiencing high demand, which may cause temporary errors.","type":"server_error","code":"server_error"}}}`,
		"data: [DONE]",
	}, "\n") + "\n"

	c, recorder, resp := newResponsesCompactTestContext(body)

	usage, compactErr := OaiResponsesCompactionHandler(c, resp)
	require.Nil(t, usage)
	require.NotNil(t, compactErr)
	require.Equal(t, http.StatusInternalServerError, compactErr.StatusCode)
	require.Zero(t, recorder.Body.Len())
	require.Contains(t, compactErr.Error(), "high demand")
}
