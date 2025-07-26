package libp2p

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/libp2p/go-libp2p/core/crypto"
)

const (
	identityFileName = "identity.key"
	logsDirName      = "logs"
)

// getHushDir returns the path to the application's data directory.
// If baseDir is provided, it's used instead of the default user home directory.
func getHushDir(baseDir string) (string, error) {
	if baseDir != "" {
		return baseDir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".gohush"), nil
}

// SaveIdentity saves the private key to the data directory.
func SaveIdentity(key crypto.PrivKey, baseDir string) error {
	hushDir, err := getHushDir(baseDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(hushDir, 0700); err != nil {
		return err
	}

	keyBytes, err := crypto.MarshalPrivateKey(key)
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(hushDir, identityFileName), keyBytes, 0600)
}

// LoadIdentity loads the private key from the data directory.
// If the key doesn't exist, it generates a new one and saves it.
func LoadIdentity(baseDir string) (crypto.PrivKey, error) {
	hushDir, err := getHushDir(baseDir)
	if err != nil {
		return nil, err
	}
	keyPath := filepath.Join(hushDir, identityFileName)

	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		if os.IsNotExist(err) {
			privKey, _, err := crypto.GenerateEd25519Key(nil)
			if err != nil {
				return nil, err
			}
			if err := SaveIdentity(privKey, baseDir); err != nil {
				return nil, err
			}
			return privKey, nil
		}
		return nil, err
	}

	return crypto.UnmarshalPrivateKey(keyBytes)
}

// LogMessage appends a message to the appropriate log file.
func LogMessage(logID string, msg *ChatMessage, baseDir string) error {
	hushDir, err := getHushDir(baseDir)
	if err != nil {
		return err
	}
	logsDir := filepath.Join(hushDir, logsDirName)
	if err := os.MkdirAll(logsDir, 0700); err != nil {
		return err
	}

	logFile := filepath.Join(logsDir, fmt.Sprintf("%s.jsonl", logID))
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if err := f.Close(); err != nil {
			// We can't return this error, so we should log it.
			// For now, we'll just ignore it in this context.
		}
	}()

	jsonBytes, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	_, err = f.WriteString(string(jsonBytes) + "\n")
	return err
}

// LoadRecentMessages loads the last N messages from a log file.
func LoadRecentMessages(logID string, count int, baseDir string) ([]ChatMessage, error) {
	hushDir, err := getHushDir(baseDir)
	if err != nil {
		return nil, err
	}
	logFile := filepath.Join(hushDir, logsDirName, fmt.Sprintf("%s.jsonl", logID))

	file, err := os.Open(logFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No history yet, which is not an error
		}
		return nil, err
	}
	defer func() {
		if err := file.Close(); err != nil {
			// We can't return this error, so we should log it.
			// For now, we'll just ignore it in this context.
		}
	}()

	var messages []ChatMessage
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var msg ChatMessage
		if err := json.Unmarshal(scanner.Bytes(), &msg); err == nil {
			messages = append(messages, msg)
		}
	}

	if len(messages) > count {
		return messages[len(messages)-count:], nil
	}
	return messages, nil
}
