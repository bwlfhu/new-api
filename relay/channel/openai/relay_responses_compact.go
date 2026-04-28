package openai

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func OaiResponsesCompactionHandler(c *gin.Context, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}

	trimmedBody := strings.TrimSpace(string(responseBody))
	if strings.HasPrefix(trimmedBody, "event:") || strings.HasPrefix(trimmedBody, "data:") {
		return handleResponsesCompactionStreamBody(c, responseBody)
	}

	var compactResp dto.OpenAIResponsesCompactionResponse
	if err := common.Unmarshal(responseBody, &compactResp); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := compactResp.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}

	service.IOCopyBytesGracefully(c, resp, responseBody)

	usage := dto.Usage{}
	if compactResp.Usage != nil {
		usage.PromptTokens = compactResp.Usage.InputTokens
		usage.CompletionTokens = compactResp.Usage.OutputTokens
		usage.TotalTokens = compactResp.Usage.TotalTokens
		if compactResp.Usage.InputTokensDetails != nil {
			usage.PromptTokensDetails.CachedTokens = compactResp.Usage.InputTokensDetails.CachedTokens
		}
	}

	return &usage, nil
}

func handleResponsesCompactionStreamBody(c *gin.Context, responseBody []byte) (*dto.Usage, *types.NewAPIError) {
	var finalResp *dto.OpenAIResponsesCompactionResponse
	var usage = &dto.Usage{}
	type compactStreamResponse struct {
		Type     string `json:"type"`
		Response *struct {
			ID        string          `json:"id"`
			Object    string          `json:"object"`
			CreatedAt int             `json:"created_at"`
			Output    json.RawMessage `json:"output"`
			Usage     *dto.Usage      `json:"usage"`
			Error     any             `json:"error,omitempty"`
		} `json:"response,omitempty"`
	}

	for _, line := range strings.Split(string(responseBody), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}

		var streamResponse compactStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResponse); err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
		}

		switch streamResponse.Type {
		case "response.error", "response.failed":
			if streamResponse.Response != nil {
				if oaiErr := dto.GetOpenAIError(streamResponse.Response.Error); oaiErr != nil && oaiErr.Type != "" {
					return nil, types.WithOpenAIError(*oaiErr, http.StatusInternalServerError)
				}
			}
			return nil, types.NewOpenAIError(
				fmt.Errorf("responses compact stream error: %s", streamResponse.Type),
				types.ErrorCodeBadResponse,
				http.StatusInternalServerError,
			)
		case "response.completed":
			if streamResponse.Response == nil {
				continue
			}

			finalResp = &dto.OpenAIResponsesCompactionResponse{
				ID:        streamResponse.Response.ID,
				Object:    streamResponse.Response.Object,
				CreatedAt: streamResponse.Response.CreatedAt,
				Output:    streamResponse.Response.Output,
				Usage:     streamResponse.Response.Usage,
				Error:     streamResponse.Response.Error,
			}
			if finalResp.Usage != nil {
				usage.PromptTokens = finalResp.Usage.InputTokens
				usage.CompletionTokens = finalResp.Usage.OutputTokens
				usage.TotalTokens = finalResp.Usage.TotalTokens
				if finalResp.Usage.InputTokensDetails != nil {
					usage.PromptTokensDetails.CachedTokens = finalResp.Usage.InputTokensDetails.CachedTokens
				}
			}
		}
	}

	if finalResp == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("responses compact stream missing completed event"), types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	jsonData, err := common.Marshal(finalResp)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	c.Header("Content-Type", "application/json")
	service.IOCopyBytesGracefully(c, nil, jsonData)
	return usage, nil
}
