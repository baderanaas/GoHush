package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/libp2p/go-libp2p/core/peer"
)

// Derive symmetric key from topic name (AES-256)
func KeyFromTopic(topic string) []byte {
	hash := sha256.Sum256([]byte(topic))
	return hash[:]
}

// Derive symmetric key from two peer IDs (sorted)
func KeyFromPeers(p1, p2 peer.ID) []byte {
	id1, _ := p1.Marshal()
	id2, _ := p2.Marshal()

	var combined []byte
	if strings.Compare(string(id1), string(id2)) < 0 {
		combined = append(id1, id2...)
	} else {
		combined = append(id2, id1...)
	}

	hash := sha256.Sum256(combined)
	return hash[:]
}

// Encrypt using AES-GCM and return base64
func Encrypt(plaintext, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize()) // static nonce (for simplicity)
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt base64 AES-GCM
func Decrypt(ciphertextB64 string, key []byte) ([]byte, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ct := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, ct, nil)
}
