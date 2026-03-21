package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestChannelAutoMigratePersistsTestConfigFields(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Channel{}))

	testModel := "gpt-4o-mini"
	testEndpointType := "chat_completions"
	testStream := true

	channel := Channel{
		Type:             1,
		Key:              "test-key",
		Name:             "test-channel",
		Status:           1,
		Group:            "default",
		Models:           "gpt-4o-mini",
		TestModel:        &testModel,
		TestEndpointType: &testEndpointType,
		TestStream:       &testStream,
	}

	require.NoError(t, db.Create(&channel).Error)

	var reloaded Channel
	require.NoError(t, db.First(&reloaded, channel.Id).Error)
	require.NotNil(t, reloaded.TestModel)
	require.Equal(t, testModel, *reloaded.TestModel)
	require.NotNil(t, reloaded.TestEndpointType)
	require.Equal(t, testEndpointType, *reloaded.TestEndpointType)
	require.NotNil(t, reloaded.TestStream)
	require.Equal(t, testStream, *reloaded.TestStream)
}

func TestChannelAutoMigratePersistsFalseTestStream(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Channel{}))

	testStream := false

	channel := Channel{
		Type:       1,
		Key:        "test-key",
		Name:       "test-channel-false-stream",
		Status:     1,
		Group:      "default",
		Models:     "gpt-4o-mini",
		TestStream: &testStream,
	}

	require.NoError(t, db.Create(&channel).Error)

	var reloaded Channel
	require.NoError(t, db.First(&reloaded, channel.Id).Error)
	require.NotNil(t, reloaded.TestStream)
	require.False(t, *reloaded.TestStream)
}
