package codex

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/require"
)

func TestModelListIncludesGPT55(t *testing.T) {
	t.Parallel()

	require.Contains(t, ModelList, "gpt-5.5")
	require.Contains(t, ModelList, ratio_setting.WithCompactModelSuffix("gpt-5.5"))
}
