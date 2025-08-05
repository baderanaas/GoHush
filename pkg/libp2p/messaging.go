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

// deriveNetworkTopic creates a unique, private network-level topic string from a topic's name and secret.
func deriveNetworkTopic(info TopicInfo) string {
	return fmt.Sprintf("gohush-topic-%s-%s", info.Name, info.Secret)
}

// JoinTopic discovers peers for a topic and subscribes to it for messaging.
func (n *DecentralizedNode) JoinTopic(info TopicInfo) error {
	networkTopic := deriveNetworkTopic(info)

	n.joinedTopicsMux.Lock()
	if _, exists := n.joinedTopics[networkTopic]; exists {
		n.joinedTopicsMux.Unlock()
		fmt.Printf("✅ Already in topic: %s\n", info.Name)
		return nil
	}
	n.joinedTopicsMux.Unlock()

	// Subscribe to the pubsub topic
	pubsubTopic, err := n.pubsub.Join(networkTopic)
	if err != nil {
		return fmt.Errorf("failed to join pubsub topic: %w", err)
	}

	sub, err := pubsubTopic.Subscribe()
	if err != nil {
		return fmt.Errorf("failed to subscribe to pubsub topic: %w", err)
	}

	n.joinedTopicsMux.Lock()
	n.joinedTopics[networkTopic] = pubsubTopic
	n.joinedTopicsMux.Unlock()

	// Start handling incoming messages for this topic
	go n.handlePubSubMessages(sub, info)

	// Discover peers for the topic via DHT
	routingDiscovery := discovery.NewRoutingDiscovery(n.dht)
	util.Advertise(n.ctx, routingDiscovery, networkTopic)

	go func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-n.ctx.Done():
				return
			case <-ticker.C:
				peerChan, err := routingDiscovery.FindPeers(n.ctx, networkTopic)
				if err != nil {
					continue
				}
				n.processPeerDiscovery(peerChan, fmt.Sprintf("topic:%s", info.Name))
			}
		}
	}()

	fmt.Printf("✅ Joined and started discovery for topic: %s\n", info.Name)
	return nil
}

// handlePubSubMessages receives and processes messages from a subscribed topic.
func (n *DecentralizedNode) handlePubSubMessages(sub *pubsub.Subscription, info TopicInfo) {
	networkTopic := deriveNetworkTopic(info)
	key := crypto.KeyFromTopic(networkTopic) // Encryption key is derived from the unique network topic

	for {
		msg, err := sub.Next(n.ctx)
		if err != nil {
			log.Printf("Error receiving pubsub message for topic %s: %v", info.Name, err)
			return
		}

		if msg.GetFrom() == n.host.ID() {
			continue
		}

		var chatMsg ChatMessage
		if err := json.Unmarshal(msg.GetData(), &chatMsg); err != nil {
			continue
		}

		n.messageHandler(chatMsg, info.Name, key)
	}
}

func (n *DecentralizedNode) handleIncomingMessage(chatMsg ChatMessage, topicDisplayName string, key []byte) {
	n.historyMux.Lock()
	if _, exists := n.messageHistory[chatMsg.ID]; exists {
		n.historyMux.Unlock()
		return
	}
	n.messageHistory[chatMsg.ID] = time.Now()
	n.historyMux.Unlock()

	plaintext, err := crypto.Decrypt(chatMsg.Content, key)
	if err != nil {
		log.Printf("⚠️ Failed to decrypt message on topic '%s' from %s", topicDisplayName, chatMsg.From[:12])
		return
	}

	// Log the plaintext message for local history
	logMsg := &ChatMessage{
		ID:        chatMsg.ID,
		From:      chatMsg.From,
		Content:   string(plaintext),
		Topic:     topicDisplayName,
		Timestamp: chatMsg.Timestamp,
	}
	if err := LogMessage(topicDisplayName, logMsg, n.hushDir); err != nil {
		log.Printf("⚠️ Failed to log received message: %v", err)
	}

	// Check if the sender is a known contact
	fromDisplay := ""
	contact, found := n.contactManager.GetContactByPeerID(chatMsg.From)
	if found {
		fromDisplay = contact.Name
	} else {
		fromDisplay = chatMsg.From
		if len(fromDisplay) > 12 {
			fromDisplay = fromDisplay[:12]
		}
	}
	fmt.Printf("\r[%s] %s: %s\n> ", topicDisplayName, fromDisplay, string(plaintext))
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

		// Derive shared secret for decryption
		key := crypto.KeyFromPeers(n.host.ID(), remotePeerID)

		n.messageHandler(msg, remotePeerID.String(), key)
	}
}

func (n *DecentralizedNode) createMessage(content, topicDisplayName string, key []byte) (*ChatMessage, error) {
	encryptedContent, err := crypto.Encrypt([]byte(content), key)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt message: %w", err)
	}

	msgID := generateMessageID(encryptedContent, n.host.ID().String(), time.Now())
	msg := &ChatMessage{
		ID:        msgID,
		From:      n.host.ID().String(),
		Content:   encryptedContent,
		Topic:     topicDisplayName,
		Timestamp: time.Now(),
	}

	n.historyMux.Lock()
	n.messageHistory[msgID] = time.Now()
	n.historyMux.Unlock()

	// Log the plaintext message for local history
	logMsg := &ChatMessage{
		ID:        msgID,
		From:      n.host.ID().String(),
		Content:   content, // Log plaintext
		Topic:     topicDisplayName,
		Timestamp: msg.Timestamp,
	}
	if err := LogMessage(topicDisplayName, logMsg, n.hushDir); err != nil {
		log.Printf("⚠️ Failed to log sent message: %v", err)
	}

	return msg, nil
}

// SendMessage encrypts and publishes a message to a topic.
func (n *DecentralizedNode) SendMessage(content, topicDisplayName string) error {
	topicInfo, found := n.topicManager.GetTopic(topicDisplayName)
	if !found {
		return fmt.Errorf("you are not a member of topic '%s'", topicDisplayName)
	}

	networkTopic := deriveNetworkTopic(topicInfo)

	n.joinedTopicsMux.RLock()
	pubsubTopic, exists := n.joinedTopics[networkTopic]
	n.joinedTopicsMux.RUnlock()

	if !exists {
		return fmt.Errorf("not joined to topic on the network: %s", topicDisplayName)
	}

	key := crypto.KeyFromTopic(networkTopic)
	msg, err := n.createMessage(content, topicDisplayName, key)
	if err != nil {
		return err
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	return pubsubTopic.Publish(n.ctx, data)
}

// getOrCreateStream finds an existing stream or creates a new one for a peer.
func (n *DecentralizedNode) getOrCreateStream(to peer.ID) (network.Stream, error) {
	n.privateStreamsMux.Lock()
	defer n.privateStreamsMux.Unlock()

	s, exists := n.privateStreams[to]
	if exists {
		// The stream is assumed to be valid if it exists.
		// SendPrivateMessage handles broken streams by retrying.
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
		// If we can't connect, store the message for later.
		log.Printf("Peer %s is offline. Storing message.", to.String()[:12])
		return n.storeOfflineMessage(content, to)
	}

	key := crypto.KeyFromPeers(n.host.ID(), to)
	msg, err := n.createMessage(content, to.String(), key)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(s)
	if err := encoder.Encode(msg); err != nil {
		// If encoding fails, the stream is likely broken. Reset and remove it.
		s.Reset()
		n.privateStreamsMux.Lock()
		delete(n.privateStreams, to)
		n.privateStreamsMux.Unlock()

		log.Printf("Stream to %s broken, storing message and retrying connection later.", to.String()[:12])

		// Store the message for later delivery.
		return n.storeOfflineMessage(content, to)
	}

	return nil
}

// storeOfflineMessage encrypts the message, stores it in the DHT, and updates the recipient's message index.
func (n *DecentralizedNode) storeOfflineMessage(content string, to peer.ID) error {
	key := crypto.KeyFromPeers(n.host.ID(), to)
	msg, err := n.createMessage(content, to.String(), key)
	if err != nil {
		return err
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	// Store the actual message
	dhtKey := getOfflineMessageKey(to, msg.ID)
	err = n.dht.PutValue(n.ctx, dhtKey, data)
	if err != nil {
		return fmt.Errorf("failed to store offline message in DHT: %w", err)
	}

	// Add the message ID to the recipient's index
	indexKey := getOfflineIndexKey(to)
	indexBytes, err := n.dht.GetValue(n.ctx, indexKey)
	var msgIDs []string
	if err == nil {
		if err := json.Unmarshal(indexBytes, &msgIDs); err != nil {
			log.Printf("Error unmarshalling message index for %s: %v", to.String()[:12], err)
			// If unmarshalling fails, start with a fresh index
			msgIDs = []string{}
		}
	}

	msgIDs = append(msgIDs, msg.ID)
	newIndexBytes, err := json.Marshal(msgIDs)
	if err != nil {
		return fmt.Errorf("failed to marshal message index: %w", err)
	}

	err = n.dht.PutValue(n.ctx, indexKey, newIndexBytes)
	if err != nil {
		// If this fails, the message is stored but won't be found automatically.
		// This is a trade-off for simplicity. A more robust system would handle this.
		return fmt.Errorf("failed to update offline message index in DHT: %w", err)
	}

	log.Printf("Stored message for %s in DHT with key %s", to.String()[:12], dhtKey)
	return nil
}

// CheckForOfflineMessages queries the DHT for messages stored for the current user.
func (n *DecentralizedNode) CheckForOfflineMessages() {
	log.Println("Checking for offline messages...")
	indexKey := getOfflineIndexKey(n.host.ID())
	indexBytes, err := n.dht.GetValue(n.ctx, indexKey)
	if err != nil {
		// This is expected if there are no messages
		return
	}

	var msgIDs []string
	if err := json.Unmarshal(indexBytes, &msgIDs); err != nil {
		log.Printf("Error unmarshalling offline message index: %v", err)
		return
	}

	if len(msgIDs) == 0 {
		return
	}

	log.Printf("Found %d offline message(s).", len(msgIDs))

	var remainingMsgIDs []string

	for _, msgID := range msgIDs {
		dhtKey := getOfflineMessageKey(n.host.ID(), msgID)
		data, err := n.dht.GetValue(n.ctx, dhtKey)
		if err != nil {
			log.Printf("Error getting offline message %s: %v", msgID, err)
			// Keep the message ID to retry later
			remainingMsgIDs = append(remainingMsgIDs, msgID)
			continue
		}

		var msg ChatMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			log.Printf("Error unmarshalling offline message %s: %v", msgID, err)
			continue
		}

		fromPeer, err := peer.Decode(msg.From)
		if err != nil {
			log.Printf("Error decoding from peer for message %s: %v", msgID, err)
			continue
		}
		dkey := crypto.KeyFromPeers(n.host.ID(), fromPeer)

		n.messageHandler(msg, fromPeer.String(), dkey)

		// "Delete" the message from the DHT by putting a nil value.
		// This is not guaranteed to remove it, but it's a common practice.
		err = n.dht.PutValue(n.ctx, dhtKey, nil)
		if err != nil {
			log.Printf("Error deleting offline message %s from DHT: %v", msgID, err)
		}
	}

	// Update the index with any remaining messages
	newIndexBytes, err := json.Marshal(remainingMsgIDs)
	if err != nil {
		log.Printf("Error marshalling remaining message index: %v", err)
		return
	}

	err = n.dht.PutValue(n.ctx, indexKey, newIndexBytes)
	if err != nil {
		log.Printf("Error updating offline message index: %v", err)
	}
}

func getOfflineMessageKey(to peer.ID, msgID string) string {
	return fmt.Sprintf("/offline/%s/%s", to.String(), msgID)
}

func getOfflineIndexKey(to peer.ID) string {
	return fmt.Sprintf("/offline-index/%s", to.String())
}
