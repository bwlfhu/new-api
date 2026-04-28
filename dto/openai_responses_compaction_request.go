package dto

import (
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

type OpenAIResponsesCompactionRequest struct {
	OpenAIResponsesRequest
}

func (r *OpenAIResponsesCompactionRequest) GetTokenCountMeta() *types.TokenCountMeta {
	return r.OpenAIResponsesRequest.GetTokenCountMeta()
}

func (r *OpenAIResponsesCompactionRequest) IsStream(c *gin.Context) bool {
	// Responses compaction is still handled as a JSON response locally even when
	// some upstreams require `"stream": true` in the request payload.
	return false
}

func (r *OpenAIResponsesCompactionRequest) SetModelName(modelName string) {
	if modelName != "" {
		r.Model = modelName
	}
}
