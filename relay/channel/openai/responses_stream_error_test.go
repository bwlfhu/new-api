package openai

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
}

type errorAfterReader struct {
	reader *strings.Reader
	err    error
}

func (r *errorAfterReader) Read(p []byte) (int, error) {
	if r.reader.Len() == 0 {
		return 0, r.err
	}
	n, readErr := r.reader.Read(p)
	if readErr == io.EOF {
		return n, r.err
	}
	return n, readErr
}

func newResponsesStreamTestContext(body string) (*gin.Context, *httptest.ResponseRecorder, *relaycommon.RelayInfo, *http.Response) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-4o-mini",
		},
	}

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	return c, recorder, info, resp
}

func TestOaiResponsesStreamHandler_ReturnsErrorBeforeFirstWrite(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	defer func() { constant.StreamingTimeout = oldTimeout }()

	body := "data: {\"type\":\"response.error\",\"response\":{\"error\":{\"message\":\"bad request\",\"type\":\"invalid_request_error\",\"code\":\"invalid_request_error\"}}}\n" +
		"data: [DONE]\n"
	c, recorder, info, resp := newResponsesStreamTestContext(body)

	usage, streamErr := OaiResponsesStreamHandler(c, info, resp)
	require.Nil(t, usage)
	require.NotNil(t, streamErr)
	require.Equal(t, http.StatusInternalServerError, streamErr.StatusCode)
	require.Zero(t, recorder.Body.Len())
}

func TestOaiResponsesStreamHandler_ReturnsErrorWhenScannerFailsBeforeCompleted(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	defer func() { constant.StreamingTimeout = oldTimeout }()

	body := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n"
	c, recorder, info, _ := newResponsesStreamTestContext("")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(&errorAfterReader{
			reader: strings.NewReader(body),
			err:    errors.New("upstream stream dropped"),
		}),
	}

	usage, streamErr := OaiResponsesStreamHandler(c, info, resp)
	require.Nil(t, usage)
	require.NotNil(t, streamErr)
	require.Equal(t, http.StatusInternalServerError, streamErr.StatusCode)
	require.Contains(t, streamErr.Error(), "upstream stream dropped")
	require.Contains(t, recorder.Body.String(), "\"type\":\"response.output_text.delta\"")
}

func TestOaiResponsesToChatStreamHandler_ReturnsErrorBeforeFirstWrite(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	defer func() { constant.StreamingTimeout = oldTimeout }()

	body := "data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"message\":\"upstream failed\",\"type\":\"server_error\",\"code\":\"server_error\"}}}\n" +
		"data: [DONE]\n"
	c, recorder, info, resp := newResponsesStreamTestContext(body)

	usage, streamErr := OaiResponsesToChatStreamHandler(c, info, resp)
	require.Nil(t, usage)
	require.NotNil(t, streamErr)
	require.Equal(t, http.StatusInternalServerError, streamErr.StatusCode)
	require.Zero(t, recorder.Body.Len())
}
