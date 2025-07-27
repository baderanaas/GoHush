package libp2p

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// newTestDir creates a temporary directory for testing and returns its path.
func newTestDir(t *testing.T) string {
	dir, err := os.MkdirTemp("", "gohush-test-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(dir)) })
	return dir
}

func TestSaveLoadIdentity(t *testing.T) {
	dir := newTestDir(t)

	// 1. Test key generation when none exists
	privKey, err := LoadIdentity(dir)
	require.NoError(t, err)
	require.NotNil(t, privKey)

	// 2. Test loading the same key
	loadedKey, err := LoadIdentity(dir)
	require.NoError(t, err)
	require.Equal(t, privKey, loadedKey, "loaded key should be the same as the saved key")
}

func TestLogAndLoadMessages(t *testing.T) {
	dir := newTestDir(t)
	logID := "test-log"

	// 1. Test loading from a non-existent log
	messages, err := LoadRecentMessages(logID, 10, dir)
	require.NoError(t, err)
	require.Empty(t, messages, "should be no messages in a new log")

	// 2. Log some messages
	msg1 := &ChatMessage{ID: "1", Content: "hello", Timestamp: time.Now()}
	msg2 := &ChatMessage{ID: "2", Content: "world", Timestamp: time.Now()}
	require.NoError(t, LogMessage(logID, msg1, dir))
	require.NoError(t, LogMessage(logID, msg2, dir))

	// 3. Load the messages back
	loaded, err := LoadRecentMessages(logID, 10, dir)
	require.NoError(t, err)
	require.Len(t, loaded, 2)
	require.Equal(t, "hello", loaded[0].Content)
	require.Equal(t, "world", loaded[1].Content)

	// 4. Test loading only the most recent N messages
	msg3 := &ChatMessage{ID: "3", Content: "newest", Timestamp: time.Now()}
	require.NoError(t, LogMessage(logID, msg3, dir))
	loaded, err = LoadRecentMessages(logID, 2, dir)
	require.NoError(t, err)
	require.Len(t, loaded, 2)
	require.Equal(t, "world", loaded[0].Content)
	require.Equal(t, "newest", loaded[1].Content)
}