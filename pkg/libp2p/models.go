
package libp2p

import (
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// PeerInfo stores metadata about a discovered peer.
type PeerInfo struct {
	ID          peer.ID
	LastSeen    time.Time
	Interests   []string
	Reliability float64 // Trust score based on interactions
	IsBootstrap bool
}

// DiscoveryMessage is used for the gossip-based peer exchange protocol.
type DiscoveryMessage struct {
	Type           string   `json:"type"` // "announce", "query", "response"
	PeerID         string   `json:"peer_id"`
	Interests      []string `json:"interests"`
	Timestamp      time.Time `json:"timestamp"`
	BootstrapPeers []string `json:"bootstrap_peers,omitempty"`
}

// ChatMessage is the structure for an actual chat message.
// The Content field holds base64-encoded AES-GCM ciphertext.
type ChatMessage struct {
	ID        string    `json:"id"`
	From      string    `json:"from"`
	Content   string    `json:"content"` // Base64-encoded AES-GCM ciphertext
	Topic     string    `json:"topic"`   // Topic for pubsub, empty for private
	Timestamp time.Time `json:"timestamp"`
}
