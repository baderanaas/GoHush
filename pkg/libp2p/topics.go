package libp2p

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
)

// TopicInfo represents a user's topic with a name and a secret key.
type TopicInfo struct {
	Name   string `json:"name"`
	Secret string `json:"secret"`
}

// TopicManager manages the topics.
type TopicManager struct {
	topics   map[string]TopicInfo // Map topic name to TopicInfo
	lock     sync.RWMutex
	filePath string
}

// NewTopicManager creates a new TopicManager.
func NewTopicManager(filePath string) (*TopicManager, error) {
	tm := &TopicManager{
		filePath: filePath,
		topics:   make(map[string]TopicInfo),
	}
	if err := tm.LoadTopics(); err != nil {
		// If the file doesn't exist, it's fine, we start with an empty map.
		if !os.IsNotExist(err) {
			return nil, err
		}
	}
	return tm, nil
}

// LoadTopics loads topics from the JSON file.
func (tm *TopicManager) LoadTopics() error {
	tm.lock.Lock()
	defer tm.lock.Unlock()

	file, err := os.ReadFile(tm.filePath)
	if err != nil {
		return err
	}

	var topicsList []TopicInfo
	if err := json.Unmarshal(file, &topicsList); err != nil {
		return err
	}

	// Populate the map from the list
	for _, topic := range topicsList {
		tm.topics[topic.Name] = topic
	}
	return nil
}

// SaveTopics saves topics to the JSON file.
func (tm *TopicManager) SaveTopics() error {
	tm.lock.RLock()
	defer tm.lock.RUnlock()

	// Convert map to a slice for JSON array storage
	var topicsList []TopicInfo
	for _, topic := range tm.topics {
		topicsList = append(topicsList, topic)
	}

	file, err := json.MarshalIndent(topicsList, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(tm.filePath, file, 0644)
}

// CreateTopic creates a new topic with a unique secret.
func (tm *TopicManager) CreateTopic(name string) (TopicInfo, error) {
	tm.lock.Lock()
	defer tm.lock.Unlock()

	if _, exists := tm.topics[name]; exists {
		return TopicInfo{}, fmt.Errorf("topic '%s' already exists", name)
	}

	// Generate a random secret
	secretBytes := make([]byte, 16)
	if _, err := rand.Read(secretBytes); err != nil {
		return TopicInfo{}, fmt.Errorf("failed to generate secret: %w", err)
	}
	secret := base64.URLEncoding.EncodeToString(secretBytes)

	topicInfo := TopicInfo{Name: name, Secret: secret}
	tm.topics[name] = topicInfo

	if err := tm.saveTopicsUnlocked(); err != nil {
		// Rollback
		delete(tm.topics, name)
		return TopicInfo{}, err
	}

	return topicInfo, nil
}

// AddTopic adds a topic from an invite.
func (tm *TopicManager) AddTopic(info TopicInfo) error {
	tm.lock.Lock()
	defer tm.lock.Unlock()

	tm.topics[info.Name] = info
	return tm.saveTopicsUnlocked()
}

// GetTopic returns a topic by name.
func (tm *TopicManager) GetTopic(name string) (TopicInfo, bool) {
	tm.lock.RLock()
	defer tm.lock.RUnlock()
	topic, exists := tm.topics[name]
	return topic, exists
}

// ListTopics returns all topic names.
func (tm *TopicManager) ListTopics() []string {
	tm.lock.RLock()
	defer tm.lock.RUnlock()
	var names []string
	for name := range tm.topics {
		names = append(names, name)
	}
	return names
}

// saveTopicsUnlocked is an internal helper to save without re-locking.
func (tm *TopicManager) saveTopicsUnlocked() error {
	var topicsList []TopicInfo
	for _, topic := range tm.topics {
		topicsList = append(topicsList, topic)
	}
	file, err := json.MarshalIndent(topicsList, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(tm.filePath, file, 0644)
}

// GenerateInvite generates a base64 encoded invite string.
func GenerateInvite(topicName, secret, peerID string) string {
	invite := fmt.Sprintf("%s:%s:%s", topicName, secret, peerID)
	return base64.URLEncoding.EncodeToString([]byte(invite))
}

// ParseInvite decodes and parses an invite string.
func ParseInvite(invite string) (string, string, string, error) {
	decoded, err := base64.URLEncoding.DecodeString(invite)
	if err != nil {
		return "", "", "", err
	}
	parts := strings.SplitN(string(decoded), ":", 3)
	if len(parts) != 3 {
		return "", "", "", fmt.Errorf("invalid invite format")
	}
	return parts[0], parts[1], parts[2], nil
}
