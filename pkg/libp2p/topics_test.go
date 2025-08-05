package libp2p

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateAndParseInvite(t *testing.T) {
	topicName := "test-topic"
	secret := "a-very-secret-key"
	peerID := "QmSoLnSGccFuZQJzRadHn95W2CrSFmMCKRYExzCGETCF9V"

	invite := GenerateInvite(topicName, secret, peerID)
	require.NotEmpty(t, invite)

	parsedTopic, parsedSecret, parsedPeerID, err := ParseInvite(invite)
	require.NoError(t, err)
	require.Equal(t, topicName, parsedTopic)
	require.Equal(t, secret, parsedSecret)
	require.Equal(t, peerID, parsedPeerID)
}

func TestParseInvalidInvite(t *testing.T) {
	_, _, _, err := ParseInvite("invalid-invite")
	require.Error(t, err)
}
