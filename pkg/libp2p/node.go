// Fully Decentralized P2P Chat System
// Best of both worlds: Secure, Scalable, and Serverless.

package libp2p

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/baderanaas/GoHush/pkg/crypto"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-pubsub"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"
	"github.com/libp2p/go-libp2p/p2p/host/autorelay"
	discovery "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/libp2p/go-libp2p/p2p/discovery/util"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	"github.com/multiformats/go-multiaddr"
)

const (
	// Protocol IDs for different services
	DiscoveryProtocol   = "/gohush/discovery/1.0.0"
	ExchangeProtocol    = "/gohush/exchange/1.0.0"
	PrivateChatProtocol = "/gohush/private-chat/1.0.0"

	// DHT namespaces for different discovery types
	GlobalNamespace   = "gohush-global"
	TopicNamespace    = "gohush-topic"
	InterestNamespace = "gohush-interest"
)

// DecentralizedNode - Fully autonomous P2P node
type DecentralizedNode struct {
	host   host.Host
	ctx    context.Context
	cancel context.CancelFunc
	dht    *dht.IpfsDHT
	pubsub *pubsub.PubSub

	// Peer management
	peers    map[peer.ID]*PeerInfo
	peersMux sync.RWMutex

	// Content and discovery
	joinedTopics    map[string]*pubsub.Topic
	joinedTopicsMux sync.RWMutex

	// Message handling
	messageHistory map[string]time.Time // Prevent message loops and clean up
	historyMux     sync.RWMutex

	// Bootstrap peers (discovered dynamically)
	bootstrapPeers []peer.AddrInfo
	bootstrapMux   sync.RWMutex
}

type PeerInfo struct {
	ID          peer.ID
	LastSeen    time.Time
	Interests   []string
	Reliability float64 // Trust score based on interactions
	IsBootstrap bool
}

type DiscoveryMessage struct {
	Type         string   `json:"type"` // "announce", "query", "response"
	PeerID       string   `json:"peer_id"`
	Interests    []string `json:"interests"`
	Timestamp    time.Time `json:"timestamp"`
	BootstrapPeers []string `json:"bootstrap_peers,omitempty"`
}

// ChatMessage now contains encrypted content
type ChatMessage struct {
	ID        string    `json:"id"`
	From      string    `json:"from"`
	Content   string    `json:"content"` // Base64-encoded AES-GCM ciphertext
	Topic     string    `json:"topic"`   // Topic for pubsub, empty for private
	Timestamp time.Time `json:"timestamp"`
}

// NewDecentralizedNode creates a fully decentralized node
func NewDecentralizedNode(port int) (*DecentralizedNode, error) {
	ctx, cancel := context.WithCancel(context.Background())

	var idht *dht.IpfsDHT

	cm, err := connmgr.NewConnManager(50, 200, connmgr.WithGracePeriod(time.Minute))
	if err != nil {
		cancel()
		return nil, err
	}

	var staticRelays []peer.AddrInfo
	for _, addr := range dht.DefaultBootstrapPeers {
		pi, err := peer.AddrInfoFromP2pAddr(addr)
		if err != nil {
			log.Printf("Failed to parse bootstrap peer: %v", err)
			continue
		}
		staticRelays = append(staticRelays, *pi)
	}

	opts := []libp2p.Option{
		libp2p.ListenAddrStrings(
			fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", port),
			fmt.Sprintf("/ip4/0.0.0.0/udp/%d/quic", port+1),
		),
		libp2p.RandomIdentity,
		libp2p.ConnectionManager(cm),
		libp2p.EnableAutoRelay(autorelay.WithStaticRelays(staticRelays)),
		libp2p.EnableHolePunching(),
		libp2p.NATPortMap(),
		libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
			var err error
			idht, err = dht.New(ctx, h, dht.Mode(dht.ModeServer))
			return idht, err
		}),
	}

	h, err := libp2p.New(opts...)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create libp2p host: %w", err)
	}

	ps, err := pubsub.NewGossipSub(ctx, h)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create pubsub: %w", err)
	}

	node := &DecentralizedNode{
		host:           h,
		ctx:            ctx,
		cancel:         cancel,
		dht:            idht,
		pubsub:         ps,
		peers:          make(map[peer.ID]*PeerInfo),
		joinedTopics:   make(map[string]*pubsub.Topic),
		messageHistory: make(map[string]time.Time),
		bootstrapPeers: make([]peer.AddrInfo, 0),
	}

	// Set up protocol handlers
	h.SetStreamHandler(DiscoveryProtocol, node.handleDiscoveryStream)
	h.SetStreamHandler(ExchangeProtocol, node.handleExchangeStream)
	h.SetStreamHandler(PrivateChatProtocol, node.handlePrivateChatStream) // New handler for private messages

	fmt.Printf("✅ Decentralized Node ID: %s\n", h.ID().String())
	fmt.Printf("✅ Listening on:\n")
	for _, addr := range h.Addrs() {
		fmt.Printf("   %s/p2p/%s\n", addr, h.ID().String())
	}

	go node.cleanupMessageHistory()

	return node, nil
}

// Bootstrap from the network itself
func (n *DecentralizedNode) Bootstrap() error {
	fmt.Println("⏳ Starting decentralized bootstrap...")

	publicDHT := []string{
		"/ip4/104.131.131.82/tcp/4001/p2p/QmaCpDMGvV2BGHeYERUEnRQAwe3N8SzbUtfsmvsqQLuvuJ", // IPFS
		"/ip4/104.236.179.241/tcp/4001/p2p/QmSoLPppuBtQSGwKDZT2M73ULpjvfd3aZ6ha4oFGL1KrGM", // IPFS
	}

	connected := false
	for _, addr := range publicDHT {
		if err := n.connectToPeer(addr); err == nil {
			connected = true
			fmt.Printf("✅ Connected to public DHT node\n")
			break
		}
	}

	if err := n.dht.Bootstrap(n.ctx); err != nil {
		log.Printf("⚠️ DHT bootstrap warning: %v\n", err)
	}

	go n.startGlobalDiscovery()
	go n.startPeerExchange()
	go n.maintainNetwork()

	if !connected {
		fmt.Println("⚠️ No initial DHT connection - will discover peers organically")
	}

	n.announcePresence()
	return nil
}

// Global peer discovery
func (n *DecentralizedNode) startGlobalDiscovery() {
	routingDiscovery := discovery.NewRoutingDiscovery(n.dht)
	util.Advertise(n.ctx, routingDiscovery, GlobalNamespace)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-n.ctx.Done():
			return
		case <-ticker.C:
			peerChan, err := routingDiscovery.FindPeers(n.ctx, GlobalNamespace)
			if err != nil {
				continue
			}
			n.processPeerDiscovery(peerChan, "global")
		}
	}
}

// JoinTopic discovers peers for a topic and subscribes to it for messaging
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

func (n *DecentralizedNode) processPeerDiscovery(peerChan <-chan peer.AddrInfo, source string) {
	for p := range peerChan {
		if p.ID == n.host.ID() || len(p.Addrs) == 0 {
			continue
		}

		n.peersMux.Lock()
		if _, exists := n.peers[p.ID]; exists {
			n.peers[p.ID].LastSeen = time.Now()
			n.peersMux.Unlock()
			continue
		}

		peerInfo := &PeerInfo{
			ID:          p.ID,
			LastSeen:    time.Now(),
			Interests:   []string{},
			Reliability: 0.5, // Start neutral
		}
		n.peers[p.ID] = peerInfo
		n.peersMux.Unlock()

		go func(peer peer.AddrInfo) {
			ctx, cancel := context.WithTimeout(n.ctx, 15*time.Second)
			defer cancel()
			if err := n.host.Connect(ctx, peer); err == nil {
				fmt.Printf("✅ Connected to peer via %s: %s\n", source, peer.ID.String()[:12])
				n.exchangeDiscoveryInfo(peer.ID)
			}
		}(p)
	}
}

// Peer exchange gossip protocol
func (n *DecentralizedNode) startPeerExchange() {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-n.ctx.Done():
			return
		case <-ticker.C:
			n.exchangePeersWithNetwork()
		}
	}
}

func (n *DecentralizedNode) exchangePeersWithNetwork() {
	n.peersMux.RLock()
	var connectedPeers []peer.ID
	for id := range n.peers {
		if n.host.Network().Connectedness(id) == network.Connected {
			connectedPeers = append(connectedPeers, id)
		}
	}
	n.peersMux.RUnlock()

	if len(connectedPeers) == 0 {
		return
	}

	for i := 0; i < min(3, len(connectedPeers)); i++ {
		go n.exchangeDiscoveryInfo(connectedPeers[i])
	}
}

func (n *DecentralizedNode) exchangeDiscoveryInfo(peerID peer.ID) {
	s, err := n.host.NewStream(n.ctx, peerID, DiscoveryProtocol)
	if err != nil {
		log.Printf("Failed to open discovery stream to %s: %v\n", peerID, err)
		return
	}
	defer s.Close()

	n.bootstrapMux.RLock()
	var bootstrapAddrs []string
	for _, p := range n.bootstrapPeers {
		if len(p.Addrs) > 0 {
			addr := fmt.Sprintf("%s/p2p/%s", p.Addrs[0], p.ID)
			bootstrapAddrs = append(bootstrapAddrs, addr)
		}
	}
	n.bootstrapMux.RUnlock()

	n.joinedTopicsMux.RLock()
	var interests []string
	for topic := range n.joinedTopics {
		interests = append(interests, topic)
	}
	n.joinedTopicsMux.RUnlock()

	msg := DiscoveryMessage{
		Type:           "announce",
		PeerID:         n.host.ID().String(),
		Interests:      interests,
		Timestamp:      time.Now(),
		BootstrapPeers: bootstrapAddrs[:min(5, len(bootstrapAddrs))],
	}

	encoder := json.NewEncoder(s)
	if err := encoder.Encode(msg); err != nil {
		log.Printf("Failed to send discovery message to %s: %v\n", peerID, err)
	}
}

func (n *DecentralizedNode) handleDiscoveryStream(s network.Stream) {
	defer s.Close()
	decoder := json.NewDecoder(s)
	var msg DiscoveryMessage
	if err := decoder.Decode(&msg); err != nil {
		return
	}

	remotePeer := s.Conn().RemotePeer()
	n.peersMux.Lock()
	if peerInfo, exists := n.peers[remotePeer]; exists {
		peerInfo.LastSeen = time.Now()
		peerInfo.Interests = msg.Interests
		peerInfo.Reliability = minFloat(1.0, peerInfo.Reliability+0.1)
	}
	n.peersMux.Unlock()

	for _, peerAddr := range msg.BootstrapPeers {
		go n.connectToPeer(peerAddr)
	}
}

// handleExchangeStream is functionally similar to discovery in this context
func (n *DecentralizedNode) handleExchangeStream(s network.Stream) {
	n.handleDiscoveryStream(s)
}

// Network maintenance
func (n *DecentralizedNode) maintainNetwork() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-n.ctx.Done():
			return
		case <-ticker.C:
			n.cleanupStaleConnections()
			n.ensureConnectivity()
		}
	}
}

func (n *DecentralizedNode) cleanupStaleConnections() {
	n.peersMux.Lock()
	defer n.peersMux.Unlock()
	staleThreshold := time.Now().Add(-10 * time.Minute)
	for id, peerInfo := range n.peers {
		if peerInfo.LastSeen.Before(staleThreshold) && n.host.Network().Connectedness(id) == network.NotConnected {
			delete(n.peers, id)
		}
	}
}

func (n *DecentralizedNode) ensureConnectivity() {
	connected := len(n.host.Network().Peers())
	if connected < 3 {
		fmt.Printf("⚠️ Low connectivity (%d peers), boosting discovery...\n", connected)
		n.announcePresence()
	}
}

func (n *DecentralizedNode) announcePresence() {
	routingDiscovery := discovery.NewRoutingDiscovery(n.dht)
	go util.Advertise(n.ctx, routingDiscovery, GlobalNamespace)

	n.joinedTopicsMux.RLock()
	for topic := range n.joinedTopics {
		topicKey := fmt.Sprintf("%s-%s", TopicNamespace, topic)
		go util.Advertise(n.ctx, routingDiscovery, topicKey)
	}
	n.joinedTopicsMux.RUnlock()
}

// handlePubSubMessages receives and processes messages from a subscribed topic
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

		log.Printf("RECEIVING (encrypted pubsub): %s\n", chatMsg.Content)
		plaintext, err := crypto.Decrypt(chatMsg.Content, key)
		if err != nil {
			log.Printf("⚠️ Failed to decrypt message on topic '%s' from %s", topic, chatMsg.From[:12])
			continue
		}
		log.Printf("DECRYPTED (pubsub): %s\n", string(plaintext))

		fromShort := chatMsg.From
		if len(fromShort) > 12 {
			fromShort = fromShort[:12]
		}
		fmt.Printf("\r[%s] %s: %s\n> ", topic, fromShort, string(plaintext))
	}
}

// handlePrivateChatStream handles incoming private messages
func (n *DecentralizedNode) handlePrivateChatStream(s network.Stream) {
	defer s.Close()
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

// SendMessage encrypts and publishes a message to a topic
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

// SendPrivateMessage sends an encrypted message directly to a peer
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
	defer s.Close()

	encoder := json.NewEncoder(s)
	if err := encoder.Encode(msg); err != nil {
		return fmt.Errorf("failed to send private message: %w", err)
	}

	return nil
}

func (n *DecentralizedNode) connectToPeer(addrStr string) error {
	addr, err := multiaddr.NewMultiaddr(addrStr)
	if err != nil {
		return err
	}
	peerInfo, err := peer.AddrInfoFromP2pAddr(addr)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(n.ctx, 15*time.Second)
	defer cancel()
	if err := n.host.Connect(ctx, *peerInfo); err != nil {
		return err
	}

	n.bootstrapMux.Lock()
	n.bootstrapPeers = append(n.bootstrapPeers, *peerInfo)
	if len(n.bootstrapPeers) > 20 {
		n.bootstrapPeers = n.bootstrapPeers[1:]
	}
	n.bootstrapMux.Unlock()
	return nil
}

func (n *DecentralizedNode) ListPeers() {
	n.peersMux.RLock()
	defer n.peersMux.RUnlock()
	connected := len(n.host.Network().Peers())
	fmt.Printf("📊 Network Status: %d connected, %d known peers\n", connected, len(n.peers))

	for id, peerInfo := range n.peers {
		status := "disconnected"
		if n.host.Network().Connectedness(id) == network.Connected {
			status = "connected"
		}
		interests := strings.Join(peerInfo.Interests, ", ")
		if interests == "" {
			interests = "none"
		}
		fmt.Printf("  - %s (%s) - interests: %s, trust: %.1f\n",
			id.String(), status, interests, peerInfo.Reliability)
	}
}

func (n *DecentralizedNode) Close() error {
	n.cancel()
	return n.host.Close()
}

// --- Utility and Crypto Functions ---

func generateMessageID(content, from string, timestamp time.Time) string {
	data := fmt.Sprintf("%s-%s-%d", content, from, timestamp.UnixNano())
	hash := sha256.Sum256([]byte(data))
	return base64.URLEncoding.EncodeToString(hash[:])
}

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

// --- CLI ---

func (n *DecentralizedNode) StartDecentralizedCLI() {
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Printf("\n✅ Fully Decentralized & Encrypted P2P Chat Started!\n")
	fmt.Printf("Commands:\n")
	fmt.Printf("  /join <topic>              - Join an encrypted chat topic\n")
	fmt.Printf("  /topics                    - Show joined topics\n")
	fmt.Printf("  /peers                     - List network peers and their full IDs\n")
	fmt.Printf("  /connect <addr>            - Connect to a specific peer\n")
	fmt.Printf("  /msg <topic> <msg>         - Send a message to a specific topic\n")
	fmt.Printf("  /private <peerID> <msg>    - Send a private message to a peer\n")
	fmt.Printf("  /quit                      - Exit\n")
	fmt.Printf("  <message>                  - Send to all joined topics\n")
	fmt.Printf("\nNetwork is fully decentralized - no servers needed!\n")
	fmt.Print("> ")

	for scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			fmt.Print("> ")
			continue
		}

		switch {
		case input == "/quit":
			fmt.Println("🔌 Shutting down decentralized node...")
			return

		case strings.HasPrefix(input, "/join "):
			topic := strings.TrimSpace(input[6:])
			if err := n.JoinTopic(topic); err != nil {
				log.Printf("❌ Failed to join topic: %v\n", err)
			}

		case input == "/topics":
			n.joinedTopicsMux.RLock()
			if len(n.joinedTopics) == 0 {
				fmt.Println("No active topics. Use /join <topic> to start.")
			} else {
				fmt.Println("Joined topics:")
				for topic := range n.joinedTopics {
					fmt.Printf("  - %s\n", topic)
				}
			}
			n.joinedTopicsMux.RUnlock()

		case input == "/peers":
			n.ListPeers()

		case strings.HasPrefix(input, "/connect "):
			addr := strings.TrimSpace(input[9:])
			if err := n.connectToPeer(addr); err != nil {
				fmt.Printf("❌ Connection failed: %v\n", err)
			} else {
				fmt.Printf("✅ Connected successfully\n")
			}

		case strings.HasPrefix(input, "/msg "):
			parts := strings.SplitN(input[5:], " ", 2)
			if len(parts) < 2 {
				fmt.Println("Usage: /msg <topic> <message>")
				continue
			}
			if err := n.SendMessage(parts[1], parts[0]); err != nil {
				log.Printf("❌ Failed to send message: %v\n", err)
			}

		case strings.HasPrefix(input, "/private "):
			parts := strings.SplitN(input[9:], " ", 2)
			if len(parts) < 2 {
				fmt.Println("Usage: /private <peerID> <message>")
				continue
			}
			peerID, err := peer.Decode(parts[0])
			if err != nil {
				fmt.Printf("❌ Invalid peer ID: %v\n", err)
				continue
			}
			if err := n.SendPrivateMessage(parts[1], peerID); err != nil {
				log.Printf("❌ Failed to send private message: %v\n", err)
			}

		default:
			n.joinedTopicsMux.RLock()
			if len(n.joinedTopics) == 0 {
				fmt.Println("No topics joined. Use /join <topic> to send a message.")
				n.joinedTopicsMux.RUnlock()
				continue
			}
			// Send to all joined topics
			for topic := range n.joinedTopics {
				if err := n.SendMessage(input, topic); err != nil {
					log.Printf("❌ Failed to send message to topic %s: %v\n", topic, err)
				}
			}
			n.joinedTopicsMux.RUnlock()
		}
		fmt.Print("> ")
	}
}

// Main function for decentralized mode
func StartDecentralized(port int) {
	if port == 0 {
		// Use a random port to allow multiple nodes on the same machine
		port = 8000 + int(time.Now().Unix()%1000)
	}

	node, err := NewDecentralizedNode(port)
	if err != nil {
		log.Fatalf("❌ Failed to create decentralized node: %v", err)
	}
	defer node.Close()

	if err := node.Bootstrap(); err != nil {
		log.Fatalf("❌ Failed to bootstrap: %v", err)
	}

	time.Sleep(3 * time.Second)

	node.StartDecentralizedCLI()
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
