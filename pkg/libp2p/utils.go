package libp2p

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"time"
)

// generateMessageID creates a unique ID for a message to prevent loops.
func generateMessageID(content, from string, timestamp time.Time) string {
	data := fmt.Sprintf("%s-%s-%d", content, from, timestamp.UnixNano())
	hash := sha256.Sum256([]byte(data))
	return base64.URLEncoding.EncodeToString(hash[:])
}

// cleanupMessageHistory periodically cleans the message history cache.
func (n *DecentralizedNode) cleanupMessageHistory() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-n.ctx.Done():
			return
		case <-ticker.C:
			n.historyMux.Lock()
			limit := time.Now().Add(-5 * time.Minute)
			for id, ts := range n.messageHistory {
				if ts.Before(limit) {
					delete(n.messageHistory, id)
				}
			}
			n.historyMux.Unlock()
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
