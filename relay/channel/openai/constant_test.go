package openai

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModelListIncludesGPT55(t *testing.T) {
	t.Parallel()

	require.Contains(t, ModelList, "gpt-5.5")
}
