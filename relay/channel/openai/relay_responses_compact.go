package openai

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
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

	logResponsesCompactionSummary(c, "json", len(responseBody), compactResp.Output, compactResp.Usage)
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
	outputItems := make([]json.RawMessage, 0)
	type compactStreamResponse struct {
		Type     string          `json:"type"`
		Item     json.RawMessage `json:"item,omitempty"`
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

			output := streamResponse.Response.Output
			if len(outputItems) > 0 && isEmptyJSONArray(output) {
				mergedOutput, err := common.Marshal(outputItems)
				if err != nil {
					return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
				}
				output = mergedOutput
			}

			finalResp = &dto.OpenAIResponsesCompactionResponse{
				ID:        streamResponse.Response.ID,
				Object:    streamResponse.Response.Object,
				CreatedAt: streamResponse.Response.CreatedAt,
				Output:    output,
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
		case "response.output_item.done":
			if len(streamResponse.Item) > 0 && string(streamResponse.Item) != "null" {
				outputItems = append(outputItems, append(json.RawMessage(nil), streamResponse.Item...))
			}
		}
	}

	if finalResp == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("responses compact stream missing completed event"), types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	logResponsesCompactionSummary(c, "stream", len(responseBody), finalResp.Output, finalResp.Usage)
	jsonData, err := common.Marshal(finalResp)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	c.Header("Content-Type", "application/json")
	service.IOCopyBytesGracefully(c, nil, jsonData)
	return usage, nil
}

func isEmptyJSONArray(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return true
	}
	var items []json.RawMessage
	if err := common.Unmarshal(raw, &items); err != nil {
		return false
	}
	return len(items) == 0
}

func logResponsesCompactionSummary(c *gin.Context, mode string, bodyBytes int, output json.RawMessage, usage *dto.Usage) {
	type outputItem struct {
		Type string `json:"type"`
	}

	outputCount := 0
	outputTypes := make([]string, 0)
	encryptedItemCount := 0
	if len(output) > 0 {
		var items []map[string]json.RawMessage
		if err := common.Unmarshal(output, &items); err == nil {
			outputCount = len(items)
			for _, item := range items {
				if rawType, ok := item["type"]; ok {
					var out outputItem
					if err := common.Unmarshal(rawType, &out.Type); err == nil && out.Type != "" {
						outputTypes = append(outputTypes, out.Type)
					}
				}
				if _, ok := item["encrypted_content"]; ok {
					encryptedItemCount++
				}
			}
		}
	}

	usageTotal := 0
	inputTokens := 0
	outputTokens := 0
	if usage != nil {
		usageTotal = usage.TotalTokens
		inputTokens = usage.InputTokens
		outputTokens = usage.OutputTokens
	}

	logger.LogInfo(c, fmt.Sprintf(
		"responses compact summary: mode=%s body_bytes=%d output_count=%d output_types=%s encrypted_item_count=%d input_tokens=%d output_tokens=%d total_tokens=%d",
		mode,
		bodyBytes,
		outputCount,
		strings.Join(outputTypes, ","),
		encryptedItemCount,
		inputTokens,
		outputTokens,
		usageTotal,
	))
}
