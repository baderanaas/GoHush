package libp2p

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGenerateMessageID(t *testing.T) {
	content := "hello"
	from := "peer1"
	timestamp := time.Now()

	id1 := generateMessageID(content, from, timestamp)
	id2 := generateMessageID(content, from, timestamp)
	require.Equal(t, id1, id2, "message IDs should be deterministic")

	id3 := generateMessageID("different content", from, timestamp)
	require.NotEqual(t, id1, id3, "message IDs should be different for different content")
}

func TestMin(t *testing.T) {
	require.Equal(t, 1, min(1, 2))
	require.Equal(t, 1, min(2, 1))
	require.Equal(t, -2, min(-1, -2))
	require.Equal(t, 0, min(0, 1))
}

func TestMinFloat(t *testing.T) {
	require.Equal(t, 1.1, minFloat(1.1, 2.2))
	require.Equal(t, 1.1, minFloat(2.2, 1.1))
	require.Equal(t, -2.2, minFloat(-1.1, -2.2))
	require.Equal(t, 0.0, minFloat(0.0, 1.1))
}
