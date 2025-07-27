package libp2p

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/baderanaas/GoHush/pkg/crypto"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	discovery "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/libp2p/go-libp2p/p2p/discovery/util"
)

// JoinTopic discovers peers for a topic and subscribes to it for messaging.
func (n *DecentralizedNode) JoinTopic(topic string) error {
	n.joinedTopicsMux.Lock()
	if _, exists := n.joinedTopics[topic]; exists {
		n.joinedTopicsMux.Unlock()
		fmt.Printf("✅ Already joined topic: %s\n", topic)
		return nil
	}
	n.joinedTopicsMux.Unlock()

	// Subscribe to the pubsub topic
	pubsubTopic, err := n.pubsub.Join(topic)
	if err != nil {
		return fmt.Errorf("failed to join pubsub topic: %w", err)
	}

	sub, err := pubsubTopic.Subscribe()
	if err != nil {
		return fmt.Errorf("failed to subscribe to pubsub topic: %w", err)
	}

	n.joinedTopicsMux.Lock()
	n.joinedTopics[topic] = pubsubTopic
	n.joinedTopicsMux.Unlock()

	// Start handling incoming messages for this topic
	go n.handlePubSubMessages(sub, topic)

	// Discover peers for the topic via DHT
	topicKey := fmt.Sprintf("%s-%s", TopicNamespace, topic)
	routingDiscovery := discovery.NewRoutingDiscovery(n.dht)
	util.Advertise(n.ctx, routingDiscovery, topicKey)

	go func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-n.ctx.Done():
				return
			case <-ticker.C:
				peerChan, err := routingDiscovery.FindPeers(n.ctx, topicKey)
				if err != nil {
					continue
				}
				n.processPeerDiscovery(peerChan, fmt.Sprintf("topic:%s", topic))
			}
		}
	}()

	fmt.Printf("✅ Joined and started discovery for topic: %s\n", topic)
	return nil
}

// handlePubSubMessages receives and processes messages from a subscribed topic.
func (n *DecentralizedNode) handlePubSubMessages(sub *pubsub.Subscription, topic string) {
	key := crypto.KeyFromTopic(topic)
	for {
		msg, err := sub.Next(n.ctx)
		if err != nil {
			log.Printf("Error receiving pubsub message for topic %s: %v", topic, err)
			return
		}

		if msg.GetFrom() == n.host.ID() {
			continue
		}

		var chatMsg ChatMessage
		if err := json.Unmarshal(msg.GetData(), &chatMsg); err != nil {
			continue
		}

		n.historyMux.Lock()
		if _, exists := n.messageHistory[chatMsg.ID]; exists {
			n.historyMux.Unlock()
			continue
		}
		n.messageHistory[chatMsg.ID] = time.Now()
		n.historyMux.Unlock()

		plaintext, err := crypto.Decrypt(chatMsg.Content, key)
		if err != nil {
			log.Printf("⚠️ Failed to decrypt message on topic '%s' from %s", topic, chatMsg.From[:12])
			continue
		}

		fromShort := chatMsg.From
		if len(fromShort) > 12 {
			fromShort = fromShort[:12]
		}
		fmt.Printf("\r[%s] %s: %s\n> ", topic, fromShort, string(plaintext))
	}
}

// handlePrivateChatStream handles incoming private messages.
func (n *DecentralizedNode) handlePrivateChatStream(s network.Stream) {
	defer func() {
		if err := s.Close(); err != nil {
			log.Printf("Error closing stream: %v", err)
		}
	}()
	decoder := json.NewDecoder(s)
	var msg ChatMessage
	if err := decoder.Decode(&msg); err != nil {
		log.Printf("Failed to decode private message: %v", err)
		return
	}

	// Prevent message loops
	n.historyMux.Lock()
	if _, exists := n.messageHistory[msg.ID]; exists {
		n.historyMux.Unlock()
		return
	}
	n.messageHistory[msg.ID] = time.Now()
	n.historyMux.Unlock()

	// Derive shared secret for decryption
	remotePeerID, err := peer.Decode(msg.From)
	if err != nil {
		log.Printf("Failed to decode sender peer ID: %v", err)
		return
	}
	key := crypto.KeyFromPeers(n.host.ID(), remotePeerID)

	log.Printf("RECEIVING (encrypted private): %s\n", msg.Content)
	plaintext, err := crypto.Decrypt(msg.Content, key)
	if err != nil {
		log.Printf("⚠️ Failed to decrypt private message from %s", msg.From[:12])
		return
	}
	log.Printf("DECRYPTED (private): %s\n", string(plaintext))

	fromShort := msg.From
	if len(fromShort) > 12 {
		fromShort = fromShort[:12]
	}
	fmt.Printf("\r[private from %s] %s\n> ", fromShort, string(plaintext))
}

// SendMessage encrypts and publishes a message to a topic.
func (n *DecentralizedNode) SendMessage(content, topic string) error {
	n.joinedTopicsMux.RLock()
	pubsubTopic, exists := n.joinedTopics[topic]
	n.joinedTopicsMux.RUnlock()

	if !exists {
		return fmt.Errorf("not joined to topic: %s", topic)
	}

	key := crypto.KeyFromTopic(topic)
	encryptedContent, err := crypto.Encrypt([]byte(content), key)
	if err != nil {
		return fmt.Errorf("failed to encrypt message: %w", err)
	}

	msgID := generateMessageID(encryptedContent, n.host.ID().String(), time.Now())
	msg := &ChatMessage{
		ID:        msgID,
		From:      n.host.ID().String(),
		Content:   encryptedContent,
		Topic:     topic,
		Timestamp: time.Now(),
	}

	n.historyMux.Lock()
	n.messageHistory[msgID] = time.Now()
	n.historyMux.Unlock()

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	return pubsubTopic.Publish(n.ctx, data)
}

// SendPrivateMessage sends an encrypted message directly to a peer.
func (n *DecentralizedNode) SendPrivateMessage(content string, to peer.ID) error {
	key := crypto.KeyFromPeers(n.host.ID(), to)
	encryptedContent, err := crypto.Encrypt([]byte(content), key)
	if err != nil {
		return fmt.Errorf("failed to encrypt private message: %w", err)
	}

	msgID := generateMessageID(encryptedContent, n.host.ID().String(), time.Now())
	msg := &ChatMessage{
		ID:        msgID,
		From:      n.host.ID().String(),
		Content:   encryptedContent,
		Topic:     "", // No topic for private messages
		Timestamp: time.Now(),
	}

	s, err := n.host.NewStream(n.ctx, to, PrivateChatProtocol)
	if err != nil {
		return fmt.Errorf("failed to open stream to peer: %w", err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			// We can't return an error here, so we log it.
			log.Printf("Error closing stream for private message: %v", err)
		}
	}()

	encoder := json.NewEncoder(s)
	if err := encoder.Encode(msg); err != nil {
		return fmt.Errorf("failed to send private message: %w", err)
	}

	return nil
}
