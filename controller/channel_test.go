package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestReadStoredChannelTestConfigUsesConfiguredValues(t *testing.T) {
	testModel := "gpt-4o-mini"
	testEndpointType := "responses"
	testStream := true
	channel := &model.Channel{
		TestModel:        &testModel,
		TestEndpointType: &testEndpointType,
		TestStream:       &testStream,
	}

	resolvedModel, resolvedEndpointType, resolvedStream := readStoredChannelTestConfig(channel)

	require.Equal(t, testModel, resolvedModel)
	require.Equal(t, testEndpointType, resolvedEndpointType)
	require.True(t, resolvedStream)
}

func TestReadStoredChannelTestConfigReturnsEmptyValuesWhenConfigMissing(t *testing.T) {
	empty := "   "
	channel := &model.Channel{
		TestModel:        &empty,
		TestEndpointType: &empty,
	}

	resolvedModel, resolvedEndpointType, resolvedStream := readStoredChannelTestConfig(channel)

	require.Empty(t, resolvedModel)
	require.Empty(t, resolvedEndpointType)
	require.False(t, resolvedStream)
}

func TestResolveAutoChannelTestArgsUsesStoredConfig(t *testing.T) {
	testModel := "gpt-4o-mini"
	testEndpointType := "responses"
	testStream := true
	channel := &model.Channel{
		TestModel:        &testModel,
		TestEndpointType: &testEndpointType,
		TestStream:       &testStream,
		Models:           "claude-3-5-sonnet,gpt-4o-mini",
	}

	resolvedModel, resolvedEndpointType, resolvedStream := resolveAutoChannelTestArgs(channel)

	require.Equal(t, testModel, resolvedModel)
	require.Equal(t, testEndpointType, resolvedEndpointType)
	require.True(t, resolvedStream)
}

func TestResolveAutoChannelTestArgsFallsBackToFirstChannelModelWhenStoredModelIsBlank(t *testing.T) {
	blankTestModel := "   "
	testEndpointType := "responses"
	testStream := true
	channel := &model.Channel{
		TestModel:        &blankTestModel,
		TestEndpointType: &testEndpointType,
		TestStream:       &testStream,
		Models:           "claude-3-5-sonnet,gpt-4o-mini",
	}

	resolvedModel, resolvedEndpointType, resolvedStream := resolveAutoChannelTestArgs(channel)

	require.Equal(t, "claude-3-5-sonnet", resolvedModel)
	require.Equal(t, testEndpointType, resolvedEndpointType)
	require.True(t, resolvedStream)
}
