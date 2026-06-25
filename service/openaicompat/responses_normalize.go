package openaicompat

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

func NormalizeResponsesSystemMessages(req *dto.OpenAIResponsesRequest) error {
	if req == nil || len(req.Input) == 0 || common.GetJsonType(req.Input) != "array" {
		return nil
	}

	var inputItems []any
	if err := common.Unmarshal(req.Input, &inputItems); err != nil {
		return err
	}

	keptItems := make([]any, 0, len(inputItems))
	instructionParts := make([]string, 0)
	removedGuidance := false

	for _, item := range inputItems {
		itemMap, ok := item.(map[string]any)
		if !ok {
			keptItems = append(keptItems, item)
			continue
		}

		role, _ := itemMap["role"].(string)
		role = strings.TrimSpace(role)
		if role != "system" && role != "developer" {
			keptItems = append(keptItems, item)
			continue
		}

		removedGuidance = true
		instructionParts = append(instructionParts, responsesGuidanceTextParts(itemMap["content"])...)
	}

	if !removedGuidance {
		return nil
	}

	inputRaw, err := common.Marshal(keptItems)
	if err != nil {
		return err
	}
	req.Input = inputRaw

	return appendResponsesInstructions(req, instructionParts)
}

func appendResponsesInstructions(req *dto.OpenAIResponsesRequest, parts []string) error {
	instructions := make([]string, 0, len(parts)+1)

	if len(req.Instructions) > 0 && common.GetJsonType(req.Instructions) != "null" {
		var existing string
		if err := common.Unmarshal(req.Instructions, &existing); err != nil {
			return fmt.Errorf("responses instructions must be a JSON string: %w", err)
		}
		if existing = strings.TrimSpace(existing); existing != "" {
			instructions = append(instructions, existing)
		}
	}

	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			instructions = append(instructions, part)
		}
	}
	if len(instructions) == 0 {
		return nil
	}

	raw, err := common.Marshal(strings.Join(instructions, "\n\n"))
	if err != nil {
		return err
	}
	req.Instructions = raw
	return nil
}

func responsesGuidanceTextParts(content any) []string {
	switch v := content.(type) {
	case string:
		return []string{v}
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			parts = append(parts, responsesGuidanceTextParts(item)...)
		}
		return parts
	case map[string]any:
		if text, ok := v["text"].(string); ok {
			return []string{text}
		}
		if content, ok := v["content"]; ok {
			return responsesGuidanceTextParts(content)
		}
	}
	return nil
}
