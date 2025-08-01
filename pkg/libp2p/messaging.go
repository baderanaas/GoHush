package libp2p

import (
	"encoding/json"
	"fmt"
	"io"
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

		// Log the plaintext message for local history
		logMsg := &ChatMessage{
			ID:        chatMsg.ID,
			From:      chatMsg.From,
			Content:   string(plaintext),
			Topic:     topic,
			Timestamp: chatMsg.Timestamp,
		}
		if err := LogMessage(topic, logMsg, n.hushDir); err != nil {
			log.Printf("⚠️ Failed to log received message: %v", err)
		}

		fromShort := chatMsg.From
		if len(fromShort) > 12 {
			fromShort = fromShort[:12]
		}
		fmt.Printf("\r[%s] %s: %s\n> ", topic, fromShort, string(plaintext))
	}
}

// handlePrivateChatStream handles incoming private messages from a persistent stream.
func (n *DecentralizedNode) handlePrivateChatStream(s network.Stream) {
	remotePeerID := s.Conn().RemotePeer()
	defer func() {
		s.Close()
		n.privateStreamsMux.Lock()
		delete(n.privateStreams, remotePeerID)
		n.privateStreamsMux.Unlock()
		log.Printf("Closed private chat stream with %s", remotePeerID.String()[:12])
	}()

	decoder := json.NewDecoder(s)
	for {
		var msg ChatMessage
		if err := decoder.Decode(&msg); err != nil {
			if err != io.EOF {
				log.Printf("Failed to decode private message from %s: %v", remotePeerID.String()[:12], err)
			}
			return
		}

		// Prevent message loops
		n.historyMux.Lock()
		if _, exists := n.messageHistory[msg.ID]; exists {
			n.historyMux.Unlock()
			continue
		}
		n.messageHistory[msg.ID] = time.Now()
		n.historyMux.Unlock()

		// Derive shared secret for decryption
		key := crypto.KeyFromPeers(n.host.ID(), remotePeerID)

		plaintext, err := crypto.Decrypt(msg.Content, key)
		if err != nil {
			log.Printf("⚠️ Failed to decrypt private message from %s", msg.From[:12])
			continue
		}

		// Log the plaintext message for local history
		logMsg := &ChatMessage{
			ID:        msg.ID,
			From:      msg.From,
			Content:   string(plaintext),
			Topic:     remotePeerID.String(),
			Timestamp: msg.Timestamp,
		}
		if err := LogMessage(remotePeerID.String(), logMsg, n.hushDir); err != nil {
			log.Printf("⚠️ Failed to log received private message: %v", err)
		}

		fromShort := msg.From
		if len(fromShort) > 12 {
			fromShort = fromShort[:12]
		}
		fmt.Printf("\r[private from %s] %s\n> ", fromShort, string(plaintext))
	}
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

	// Log the plaintext message for local history
	logMsg := &ChatMessage{
		ID:        msgID,
		From:      n.host.ID().String(),
		Content:   content, // Log plaintext
		Topic:     topic,
		Timestamp: msg.Timestamp,
	}
	if err := LogMessage(topic, logMsg, n.hushDir); err != nil {
		log.Printf("⚠️ Failed to log sent message: %v", err)
	}

	return pubsubTopic.Publish(n.ctx, data)
}

// getOrCreateStream finds an existing stream or creates a new one for a peer.
func (n *DecentralizedNode) getOrCreateStream(to peer.ID) (network.Stream, error) {
	n.privateStreamsMux.Lock()
	defer n.privateStreamsMux.Unlock()

	s, exists := n.privateStreams[to]
	if exists {
		// Check if the stream is still valid. A simple way is to try a write,
		// but that's complex. For now, we assume it's good if it exists.
		// A better check might be needed in a production system.
		return s, nil
	}

	s, err := n.host.NewStream(n.ctx, to, PrivateChatProtocol)
	if err != nil {
		return nil, fmt.Errorf("failed to open stream to peer: %w", err)
	}

	n.privateStreams[to] = s
	return s, nil
}

// SendPrivateMessage sends an encrypted message directly to a peer using a persistent stream.
func (n *DecentralizedNode) SendPrivateMessage(content string, to peer.ID) error {
	s, err := n.getOrCreateStream(to)
	if err != nil {
		return err
	}

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

	encoder := json.NewEncoder(s)
	if err := encoder.Encode(msg); err != nil {
		// If encoding fails, the stream is likely broken. Close and remove it.
		s.Close()
		n.privateStreamsMux.Lock()
		delete(n.privateStreams, to)
		n.privateStreamsMux.Unlock()
		return fmt.Errorf("failed to send private message: %w", err)
	}

	// Log the plaintext message for local history
	logMsg := &ChatMessage{
		ID:        msgID,
		From:      n.host.ID().String(),
		Content:   content, // Log plaintext
		Topic:     to.String(),
		Timestamp: msg.Timestamp,
	}
	if err := LogMessage(to.String(), logMsg, n.hushDir); err != nil {
		log.Printf("⚠️ Failed to log sent private message: %v", err)
	}

	return nil
}
