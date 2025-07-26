package crypto

import (
	"testing"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
)

func TestKeyFromTopic(t *testing.T) {
	topic1 := "test-topic"
	topic2 := "test-topic"
	topic3 := "different-topic"

	key1 := KeyFromTopic(topic1)
	key2 := KeyFromTopic(topic2)
	key3 := KeyFromTopic(topic3)

	require.Equal(t, key1, key2, "keys from the same topic should be equal")
	require.NotEqual(t, key1, key3, "keys from different topics should not be equal")
	require.Len(t, key1, 32, "key should be 32 bytes for AES-256")
}

func TestKeyFromPeers(t *testing.T) {
	// Generate two random peer IDs
	priv1, _, err := crypto.GenerateEd25519Key(nil)
	require.NoError(t, err)
	p1, err := peer.IDFromPrivateKey(priv1)
	require.NoError(t, err)

	priv2, _, err := crypto.GenerateEd25519Key(nil)
	require.NoError(t, err)
	p2, err := peer.IDFromPrivateKey(priv2)
	require.NoError(t, err)

	key1 := KeyFromPeers(p1, p2)
	key2 := KeyFromPeers(p2, p1) // Order should not matter

	require.Equal(t, key1, key2, "keys from the same peer pair should be equal regardless of order")
	require.Len(t, key1, 32, "key should be 32 bytes for AES-256")

	// Generate a third peer ID
	priv3, _, err := crypto.GenerateEd25519Key(nil)
	require.NoError(t, err)
	p3, err := peer.IDFromPrivateKey(priv3)
	require.NoError(t, err)

	key3 := KeyFromPeers(p1, p3)
	require.NotEqual(t, key1, key3, "keys from different peer pairs should not be equal")
}

func TestEncryptDecrypt(t *testing.T) {
	key := KeyFromTopic("a-secure-topic")
	plaintext := []byte("this is a super secret message")

	ciphertext, err := Encrypt(plaintext, key)
	require.NoError(t, err)
	require.NotEmpty(t, ciphertext)

	decrypted, err := Decrypt(ciphertext, key)
	require.NoError(t, err)
	require.Equal(t, plaintext, decrypted, "decrypted text should match original plaintext")
}

func TestDecryptFailure(t *testing.T) {
	key1 := KeyFromTopic("topic1")
	key2 := KeyFromTopic("topic2")
	plaintext := []byte("another secret")

	ciphertext, err := Encrypt(plaintext, key1)
	require.NoError(t, err)

	// Try to decrypt with the wrong key
	_, err = Decrypt(ciphertext, key2)
	require.Error(t, err, "decryption with the wrong key should fail")

	// Try to decrypt corrupted ciphertext
	corruptedCiphertext := "not_a_valid_base64_string"
	_, err = Decrypt(corruptedCiphertext, key1)
	require.Error(t, err, "decryption of corrupted ciphertext should fail")
}
