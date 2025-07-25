package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/multiformats/go-multiaddr"
)

const (
	// Protocol ID for our messaging service
	ProtocolID = "/gohush/chat/1.0.0"
	// Service name for mDNS discovery
	ServiceName = "gohush-chat"
)

// GoHushNode represents our P2P messaging node
type GoHushNode struct {
	host     host.Host
	ctx      context.Context
	cancel   context.CancelFunc
	peers    map[peer.ID]bool
	peersMux sync.RWMutex
}

// discoveryNotifee handles peer discovery events
type discoveryNotifee struct {
	node *GoHushNode
}

func (n *discoveryNotifee) HandlePeerFound(pi peer.AddrInfo) {
	fmt.Printf("🔍 Discovered peer: %s\n", pi.ID.String())
	
	// Add to our known peers
	n.node.peersMux.Lock()
	n.node.peers[pi.ID] = true
	n.node.peersMux.Unlock()
	
	// Connect to the discovered peer
	if err := n.node.host.Connect(n.node.ctx, pi); err != nil {
		fmt.Printf("❌ Failed to connect to peer %s: %v\n", pi.ID.String(), err)
	} else {
		fmt.Printf("✅ Connected to peer: %s\n", pi.ID.String())
	}
}

// NewGoHushNode creates a new P2P messaging node
func NewGoHushNode(port int) (*GoHushNode, error) {
	ctx, cancel := context.WithCancel(context.Background())
	
	// Create a libp2p host with a random key pair
	h, err := libp2p.New(
		libp2p.ListenAddrStrings(fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", port)),
		libp2p.RandomIdentity, // Generates a random key pair
	)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create libp2p host: %w", err)
	}
	
	node := &GoHushNode{
		host:   h,
		ctx:    ctx,
		cancel: cancel,
		peers:  make(map[peer.ID]bool),
	}
	
	// Set up our messaging protocol handler
	h.SetStreamHandler(ProtocolID, node.handleIncomingStream)
	
	// Print host information
	fmt.Printf("🆔 Peer ID: %s\n", h.ID().String())
	fmt.Printf("🌐 Listening on:\n")
	for _, addr := range h.Addrs() {
		fmt.Printf("   %s/p2p/%s\n", addr, h.ID().String())
	}
	
	return node, nil
}

// StartDiscovery starts mDNS peer discovery
func (n *GoHushNode) StartDiscovery() error {
	// Create mDNS discovery service
	disc := mdns.NewMdnsService(n.host, ServiceName, &discoveryNotifee{node: n})
	
	// Start the discovery service
	if err := disc.Start(); err != nil {
		return fmt.Errorf("failed to start mDNS discovery: %w", err)
	}
	
	fmt.Printf("🔍 Started mDNS discovery service\n")
	return nil
}

// handleIncomingStream handles incoming message streams
func (n *GoHushNode) handleIncomingStream(s network.Stream) {
	defer s.Close()
	
	remotePeer := s.Conn().RemotePeer()
	fmt.Printf("📨 Incoming stream from: %s\n", remotePeer.String())
	
	// Read messages from the stream
	reader := bufio.NewReader(s)
	for {
		msgBytes, err := reader.ReadBytes('\n')
		if err != nil {
			if err != io.EOF {
				fmt.Printf("❌ Error reading from stream: %v\n", err)
			}
			break
		}
		
		message := strings.TrimSpace(string(msgBytes))
		if message != "" {
			// Use first 12 characters of peer ID for display
			peerShort := remotePeer.String()
			if len(peerShort) > 12 {
				peerShort = peerShort[:12]
			}
			fmt.Printf("💬 %s: %s\n", peerShort, message)
		}
	}
}

// SendMessage sends a message to a specific peer
func (n *GoHushNode) SendMessage(peerID peer.ID, message string) error {
	// Open a stream to the peer
	s, err := n.host.NewStream(n.ctx, peerID, ProtocolID)
	if err != nil {
		return fmt.Errorf("failed to open stream to peer %s: %w", peerID.String(), err)
	}
	defer s.Close()
	
	// Send the message
	_, err = s.Write([]byte(message + "\n"))
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	
	// Use first 12 characters of peer ID for display
	peerShort := peerID.String()
	if len(peerShort) > 12 {
		peerShort = peerShort[:12]
	}
	fmt.Printf("📤 Sent to %s: %s\n", peerShort, message)
	return nil
}

// BroadcastMessage sends a message to all connected peers
func (n *GoHushNode) BroadcastMessage(message string) {
	n.peersMux.RLock()
	peers := make([]peer.ID, 0, len(n.peers))
	for peerID := range n.peers {
		if peerID != n.host.ID() { // Don't send to ourselves
			peers = append(peers, peerID)
		}
	}
	n.peersMux.RUnlock()
	
	if len(peers) == 0 {
		fmt.Printf("📭 No peers connected to broadcast message\n")
		return
	}
	
	fmt.Printf("📢 Broadcasting to %d peers: %s\n", len(peers), message)
	for _, peerID := range peers {
		go func(pid peer.ID) {
			if err := n.SendMessage(pid, message); err != nil {
				// Use first 12 characters of peer ID for display
				peerShort := pid.String()
				if len(peerShort) > 12 {
					peerShort = peerShort[:12]
				}
				fmt.Printf("❌ Failed to send to %s: %v\n", peerShort, err)
			}
		}(peerID)
	}
}

// ListPeers shows all connected peers
func (n *GoHushNode) ListPeers() {
	n.peersMux.RLock()
	defer n.peersMux.RUnlock()
	
	if len(n.peers) == 0 {
		fmt.Printf("👥 No peers connected\n")
		return
	}
	
	fmt.Printf("👥 Connected peers (%d):\n", len(n.peers))
	for peerID := range n.peers {
		if peerID != n.host.ID() {
			fmt.Printf("   %s\n", peerID.String())
		}
	}
}

// ConnectToPeer connects to a peer using their multiaddress
func (n *GoHushNode) ConnectToPeer(addrStr string) error {
	addr, err := multiaddr.NewMultiaddr(addrStr)
	if err != nil {
		return fmt.Errorf("invalid multiaddress: %w", err)
	}
	
	peerInfo, err := peer.AddrInfoFromP2pAddr(addr)
	if err != nil {
		return fmt.Errorf("failed to get peer info: %w", err)
	}
	
	if err := n.host.Connect(n.ctx, *peerInfo); err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	
	n.peersMux.Lock()
	n.peers[peerInfo.ID] = true
	n.peersMux.Unlock()
	
	fmt.Printf("✅ Manually connected to peer: %s\n", peerInfo.ID.String())
	return nil
}

// Close shuts down the node
func (n *GoHushNode) Close() error {
	n.cancel()
	return n.host.Close()
}

// CLI interface for interacting with the node
func (n *GoHushNode) StartCLI() {
	scanner := bufio.NewScanner(os.Stdin)
	
	fmt.Printf("\n🚀 GoHush P2P Chat Started!\n")
	fmt.Printf("Commands:\n")
	fmt.Printf("  /peers          - List connected peers\n")
	fmt.Printf("  /connect <addr> - Connect to peer manually\n")
	fmt.Printf("  /quit           - Exit the application\n")
	fmt.Printf("  <message>       - Broadcast message to all peers\n")
	fmt.Printf("\nType your messages:\n")
	
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}
		
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		
		switch {
		case input == "/quit":
			fmt.Printf("👋 Goodbye!\n")
			return
			
		case input == "/peers":
			n.ListPeers()
			
		case strings.HasPrefix(input, "/connect "):
			addr := strings.TrimSpace(input[9:])
			if err := n.ConnectToPeer(addr); err != nil {
				fmt.Printf("❌ Connection failed: %v\n", err)
			}
			
		default:
			n.BroadcastMessage(input)
		}
	}
}

func main() {
	// Parse command line arguments for port
	port := 0
	if len(os.Args) > 1 {
		fmt.Sscanf(os.Args[1], "%d", &port)
	}
	if port == 0 {
		// Generate a random port between 10000-20000
		portBytes := make([]byte, 2)
		rand.Read(portBytes)
		port = 10000 + int(portBytes[0])<<8 + int(portBytes[1])%10000
	}
	
	// Create and start the node
	node, err := NewGoHushNode(port)
	if err != nil {
		log.Fatalf("Failed to create node: %v", err)
	}
	defer node.Close()
	
	// Start peer discovery
	if err := node.StartDiscovery(); err != nil {
		log.Fatalf("Failed to start discovery: %v", err)
	}
	
	// Give some time for discovery to work
	time.Sleep(2 * time.Second)
	
	// Start the CLI interface
	node.StartCLI()
}