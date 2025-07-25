package libp2p

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
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	discovery "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/libp2p/go-libp2p/p2p/discovery/util"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	"github.com/libp2p/go-libp2p/p2p/host/autorelay"
	"github.com/multiformats/go-multiaddr"
)

const (
	// ProtocolID for our messaging service
	ProtocolID = "/gohush/chat/1.0.0"
	// ServiceName for mDNS discovery
	ServiceName = "gohush-chat"
	// Rendezvous string for DHT discovery
	Rendezvous = "gohush-dht-rendezvous"
)

// GoHushNode represents our P2P messaging node
type GoHushNode struct {
	host     host.Host
	ctx      context.Context
	cancel   context.CancelFunc
	peers    map[peer.ID]bool
	peersMux sync.RWMutex
	config   *NodeConfig
	dht      *dht.IpfsDHT
}

// NodeConfig holds configuration for NAT traversal
type NodeConfig struct {
	EnableAutoRelay bool
	EnableHolePunch bool
	EnableUPnP      bool
	RelayNodes      []string // Known relay nodes for bootstrapping
}

// DefaultRelayNodes - public relay nodes for bootstrapping
var DefaultRelayNodes = []string{
	"/dnsaddr/relay.libp2p.io/p2p/12D3KooWDpJ7At3VoHAPQCjKJC8RB1hGJJt6eFCrTdWhjLU7mGYd",
	"/dnsaddr/relay.libp2p.io/p2p/12D3KooWDpJ7At3VoHAPQCjKJC8RB1hGJJt6eFCrTdWhjLU7mGYd",
}

// discoveryNotifee handles peer discovery events
type discoveryNotifee struct {
	node *GoHushNode
}

func (n *discoveryNotifee) HandlePeerFound(pi peer.AddrInfo) {
	fmt.Printf("🔍 Discovered peer (mDNS): %s\n", pi.ID.String())
	
	// Add to our known peers
	n.node.peersMux.Lock()
	if _, ok := n.node.peers[pi.ID]; ok {
		n.node.peersMux.Unlock()
		return
	}
	n.node.peers[pi.ID] = true
	n.node.peersMux.Unlock()
	
	// Connect to the discovered peer
	if err := n.node.host.Connect(n.node.ctx, pi); err != nil {
		fmt.Printf("❌ Failed to connect to peer %s: %v\n", pi.ID.String(), err)
	} else {
		fmt.Printf("✅ Connected to peer: %s\n", pi.ID.String())
	}
}

// NewGoHushNode creates a new P2P messaging node.
func NewGoHushNode(port int, config *NodeConfig) (*GoHushNode, error) {
	ctx, cancel := context.WithCancel(context.Background())
	
	var idht *dht.IpfsDHT
	opts := []libp2p.Option{
		libp2p.ListenAddrStrings(fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", port)),
		libp2p.RandomIdentity,
	}

	if config != nil {
		cm, err := connmgr.NewConnManager(100, 400, connmgr.WithGracePeriod(time.Minute))
		if err != nil {
			return nil, err
		}
		opts = append(opts, libp2p.ConnectionManager(cm))
		if config.EnableUPnP {
			opts = append(opts, libp2p.NATPortMap())
			fmt.Printf("🔧 UPnP port mapping enabled\n")
		}
		if config.EnableAutoRelay {
			opts = append(opts, libp2p.EnableAutoRelay(autorelay.WithStaticRelays(parseRelayNodes(config.RelayNodes))))
			fmt.Printf("🔧 AutoRelay enabled with %d relay nodes\n", len(config.RelayNodes))
		}
		if config.EnableHolePunch {
			opts = append(opts, libp2p.EnableHolePunching())
			fmt.Printf("🔧 Hole punching enabled\n")
		}
		opts = append(opts, libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
			var err error
			idht, err = dht.New(ctx, h, dht.Mode(dht.ModeServer))
			return idht, err
		}))
	}
	
	h, err := libp2p.New(opts...)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create libp2p host: %w", err)
	}
	
	node := &GoHushNode{
		host:   h,
		ctx:    ctx,
		cancel: cancel,
		peers:  make(map[peer.ID]bool),
		config: config,
		dht:    idht,
	}
	
	h.SetStreamHandler(ProtocolID, node.handleIncomingStream)
	
	if config != nil && config.EnableAutoRelay {
		_, err := relay.New(h)
		if err != nil {
			fmt.Printf("⚠️  Failed to enable circuit relay: %v\n", err)
		} else {
			fmt.Printf("🔧 Circuit relay v2 enabled\n")
		}
	}
	
	fmt.Printf("🆔 Peer ID: %s\n", h.ID().String())
	fmt.Printf("🌐 Listening on:\n")
	for _, addr := range h.Addrs() {
		fmt.Printf("   %s/p2p/%s\n", addr, h.ID().String())
	}
	
	return node, nil
}

func parseRelayNodes(relayAddrs []string) []peer.AddrInfo {
	var relays []peer.AddrInfo
	for _, addr := range relayAddrs {
		maddr, err := multiaddr.NewMultiaddr(addr)
		if err != nil {
			continue
		}
		addrInfo, err := peer.AddrInfoFromP2pAddr(maddr)
		if err != nil {
			continue
		}
		relays = append(relays, *addrInfo)
	}
	return relays
}

func (n *GoHushNode) StartDiscovery() error {
	// Start mDNS discovery
	disc := mdns.NewMdnsService(n.host, ServiceName, &discoveryNotifee{node: n})
	if err := disc.Start(); err != nil {
		return fmt.Errorf("failed to start mDNS discovery: %w", err)
	}
	fmt.Printf("🔍 Started mDNS discovery service\n")

	// If DHT is enabled, start DHT-based discovery
	if n.dht != nil {
		fmt.Println("🌐 Bootstrapping from public DHT...")
		if err := n.dht.Bootstrap(n.ctx); err != nil {
			return fmt.Errorf("failed to bootstrap DHT: %w", err)
		}

		go n.DiscoverPeers()
	}

	return nil
}

func (n *GoHushNode) DiscoverPeers() {
	routingDiscovery := discovery.NewRoutingDiscovery(n.dht)
	util.Advertise(n.ctx, routingDiscovery, Rendezvous)
	fmt.Printf("🔍 Started DHT discovery service, searching for peers...\n")

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-n.ctx.Done():
			return
		case <-ticker.C:
			peerChan, err := routingDiscovery.FindPeers(n.ctx, Rendezvous)
			if err != nil {
				fmt.Printf("❌ Failed to find peers: %v\n", err)
				continue
			}

			for p := range peerChan {
				if p.ID == n.host.ID() || len(p.Addrs) == 0 {
					continue
				}

				n.peersMux.Lock()
				if _, ok := n.peers[p.ID]; ok {
					n.peersMux.Unlock()
					continue
				}
				n.peers[p.ID] = true
				n.peersMux.Unlock()

				fmt.Printf("🔍 Discovered peer (DHT): %s\n", p.ID.String())
				if err := n.host.Connect(n.ctx, p); err != nil {
					fmt.Printf("❌ Failed to connect to peer %s: %v\n", p.ID.String(), err)
				} else {
					fmt.Printf("✅ Connected to peer: %s\n", p.ID.String())
				}
			}
		}
	}
}

func (n *GoHushNode) ConnectToRelayNodes() {
	if n.config == nil || !n.config.EnableAutoRelay {
		return
	}
	
	fmt.Printf("🔗 Connecting to relay nodes...\n")
	for _, relayAddr := range n.config.RelayNodes {
		go func(addr string) {
			maddr, err := multiaddr.NewMultiaddr(addr)
			if err != nil {
				fmt.Printf("❌ Invalid relay address %s: %v\n", addr, err)
				return
			}
			
			addrInfo, err := peer.AddrInfoFromP2pAddr(maddr)
			if err != nil {
				fmt.Printf("❌ Failed to parse relay address %s: %v\n", addr, err)
				return
			}
			
			ctx, cancel := context.WithTimeout(n.ctx, 30*time.Second)
			defer cancel()
			
			if err := n.host.Connect(ctx, *addrInfo); err != nil {
				fmt.Printf("❌ Failed to connect to relay %s: %v\n", addrInfo.ID.String()[:12], err)
			} else {
				fmt.Printf("✅ Connected to relay: %s\n", addrInfo.ID.String()[:12])
			}
		}(relayAddr)
	}
}

func (n *GoHushNode) GetNATStatus() {
	time.Sleep(5 * time.Second)
	
	hasRelay := false
	for _, addr := range n.host.Addrs() {
		if strings.Contains(addr.String(), "/p2p-circuit") {
			hasRelay = true
			fmt.Printf("🔄 Relay address: %s/p2p/%s\n", addr, n.host.ID().String())
		}
	}
	
	if hasRelay {
		fmt.Printf("🏠 NAT Status: Behind NAT (using relay)\n")
	} else {
		fmt.Printf("🌐 NAT Status: Direct connection available\n")
	}
}

func (n *GoHushNode) handleIncomingStream(s network.Stream) {
	defer s.Close()
	
	remotePeer := s.Conn().RemotePeer()
	fmt.Printf("📨 Incoming stream from: %s\n", remotePeer.String())
	
	if n.config != nil && strings.Contains(s.Conn().RemoteMultiaddr().String(), "/p2p-circuit") {
		fmt.Printf("🔄 Connection via relay\n")
	}
	
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
			peerShort := remotePeer.String()
			if len(peerShort) > 12 {
				peerShort = peerShort[:12]
			}
			fmt.Printf("💬 %s: %s\n", peerShort, message)
		}
	}
}

func (n *GoHushNode) SendMessage(peerID peer.ID, message string) error {
	s, err := n.host.NewStream(n.ctx, peerID, ProtocolID)
	if err != nil {
		return fmt.Errorf("failed to open stream to peer %s: %w", peerID.String(), err)
	}
	defer s.Close()
	
	_, err = s.Write([]byte(message + "\n"))
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	
	peerShort := peerID.String()
	if len(peerShort) > 12 {
		peerShort = peerShort[:12]
	}
	
	if n.config != nil {
		if len(n.host.Network().ConnsToPeer(peerID)) > 0 && strings.Contains(n.host.Network().ConnsToPeer(peerID)[0].RemoteMultiaddr().String(), "/p2p-circuit") {
			fmt.Printf("📤 Sent via relay to %s: %s\n", peerShort, message)
		} else {
			fmt.Printf("📤 Sent to %s: %s\n", peerShort, message)
		}
	} else {
		fmt.Printf("📤 Sent to %s: %s\n", peerShort, message)
	}
	
	return nil
}

func (n *GoHushNode) BroadcastMessage(message string) {
	n.peersMux.RLock()
	peers := make([]peer.ID, 0, len(n.peers))
	for peerID := range n.peers {
		if peerID != n.host.ID() {
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
				peerShort := pid.String()
				if len(peerShort) > 12 {
					peerShort = peerShort[:12]
				}
				fmt.Printf("❌ Failed to send to %s: %v\n", peerShort, err)
			}
		}(peerID)
	}
}

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
			if n.config != nil {
				// Check connection type
				conn := n.host.Network().ConnsToPeer(peerID)
				connType := "direct"
				if len(conn) > 0 && strings.Contains(conn[0].RemoteMultiaddr().String(), "/p2p-circuit") {
					connType = "relay"
				}
				fmt.Printf("   %s (%s)\n", peerID.String(), connType)
			} else {
				fmt.Printf("   %s\n", peerID.String())
			}
		}
	}
}

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

func (n *GoHushNode) Close() error {
	n.cancel()
	return n.host.Close()
}

func (n *GoHushNode) StartCLI() {
	scanner := bufio.NewScanner(os.Stdin)
	
	if n.config != nil {
		fmt.Printf("\n🚀 GoHush P2P Chat with NAT Traversal Started!\n")
		fmt.Printf("Commands:\n")
		fmt.Printf("  /peers          - List connected peers\n")
		fmt.Printf("  /connect <addr> - Connect to peer manually\n")
		fmt.Printf("  /msg <peerID> <message> - Send a direct message\n")
		fmt.Printf("  /status         - Show NAT and connection status\n")
		fmt.Printf("  /quit           - Exit the application\n")
		fmt.Printf("  <message>       - Broadcast message to all peers\n")
	} else {
		fmt.Printf("\n🚀 GoHush P2P Chat Started!\n")
		fmt.Printf("Commands:\n")
		fmt.Printf("  /peers          - List connected peers\n")
		fmt.Printf("  /connect <addr> - Connect to peer manually\n")
		fmt.Printf("  /msg <peerID> <message> - Send a direct message\n")
		fmt.Printf("  /quit           - Exit the application\n")
		fmt.Printf("  <message>       - Broadcast message to all peers\n")
	}
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
			
		case input == "/status" && n.config != nil:
			n.GetNATStatus()
			
		case strings.HasPrefix(input, "/connect "):
			addr := strings.TrimSpace(input[9:])
			if err := n.ConnectToPeer(addr); err != nil {
				fmt.Printf("❌ Connection failed: %v\n", err)
			}
		
		case strings.HasPrefix(input, "/msg "):
			parts := strings.SplitN(input, " ", 3)
			if len(parts) < 3 {
				fmt.Println("Usage: /msg <peerID> <message>")
				continue
			}
			peerID, err := peer.Decode(parts[1])
			if err != nil {
				fmt.Printf("❌ Invalid peer ID: %v\n", err)
				continue
			}
			if err := n.SendMessage(peerID, parts[2]); err != nil {
				fmt.Printf("❌ Failed to send message: %v\n", err)
			}

		default:
			n.BroadcastMessage(input)
		}
	}
}

func StartBasic() {
	port := 0
	if len(os.Args) > 1 {
		fmt.Sscanf(os.Args[1], "%d", &port)
	}
	if port == 0 {
		portBytes := make([]byte, 2)
		rand.Read(portBytes)
		port = 10000 + int(portBytes[0])<<8 + int(portBytes[1])%10000
	}
	
	node, err := NewGoHushNode(port, nil)
	if err != nil {
		log.Fatalf("Failed to create node: %v", err)
	}
	defer node.Close()
	
	if err := node.StartDiscovery(); err != nil {
		log.Fatalf("Failed to start discovery: %v", err)
	}
	
	time.Sleep(2 * time.Second)
	
	node.StartCLI()
}

func StartWithNat() {
	port := 0
	enableUPnP := false
	enableAutoRelay := false
	enableHolePunch := false
	
	args := os.Args[1:]
	for i, arg := range args {
		switch arg {
		case "--port", "-p":
			if i+1 < len(args) {
				fmt.Sscanf(args[i+1], "%d", &port)
			}
		case "--upnp":
			enableUPnP = true
		case "--relay":
			enableAutoRelay = true
		case "--hole-punch":
			enableHolePunch = true
		case "--help", "-h":
			fmt.Printf("Usage: %s [options]\n", os.Args[0])
			fmt.Printf("Options:\n")
			fmt.Printf("  --port, -p <port>  Listen port (random if not specified)\n")
			fmt.Printf("  --upnp             Enable UPnP port mapping\n")
			fmt.Printf("  --relay            Enable AutoRelay for NAT traversal\n")
			fmt.Printf("  --hole-punch       Enable hole punching\n")
			fmt.Printf("  --help, -h         Show this help\n")
			return
		}
	}
	
	if port == 0 {
		portBytes := make([]byte, 2)
		rand.Read(portBytes)
		port = 10000 + int(portBytes[0])<<8 + int(portBytes[1])%10000
	}
	
	config := &NodeConfig{
		EnableAutoRelay: enableAutoRelay,
		EnableHolePunch: enableHolePunch,
		EnableUPnP:      enableUPnP,
		RelayNodes:      DefaultRelayNodes,
	}
	
	if enableAutoRelay || enableHolePunch || enableUPnP {
		fmt.Printf("🔧 NAT Traversal enabled: UPnP=%v, AutoRelay=%v, HolePunch=%v\n", 
			enableUPnP, enableAutoRelay, enableHolePunch)
	}
	
	node, err := NewGoHushNode(port, config)
	if err != nil {
		log.Fatalf("Failed to create node: %v", err)
	}
	defer node.Close()
	
	if err := node.StartDiscovery(); err != nil {
		log.Fatalf("Failed to start discovery: %v", err)
	}
	
	if config.EnableAutoRelay {
		node.ConnectToRelayNodes()
	}
	
	go node.GetNATStatus()
	
	time.Sleep(3 * time.Second)
	
	node.StartCLI()
}