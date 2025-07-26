
package libp2p

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-pubsub"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
)

// DecentralizedNode - Fully autonomous P2P node
type DecentralizedNode struct {
	host    host.Host
	ctx    context.Context
	cancel context.CancelFunc
	dht    *dht.IpfsDHT
	pubsub *pubsub.PubSub
	hushDir string

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

// NewDecentralizedNode creates a fully decentralized node
func NewDecentralizedNode(port int, baseDir string) (*DecentralizedNode, error) {
	ctx, cancel := context.WithCancel(context.Background())

	hushDir, err := getHushDir(baseDir)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to get hush directory: %w", err)
	}

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

	privKey, err := LoadIdentity(hushDir)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to load or generate identity: %w", err)
	}

	opts := []libp2p.Option{
		libp2p.ListenAddrStrings(
			fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", port),
			fmt.Sprintf("/ip4/0.0.0.0/udp/%d/quic", port+1),
		),
		libp2p.Identity(privKey),
		libp2p.ConnectionManager(cm),
		libp2p.EnableAutoRelayWithStaticRelays(staticRelays),
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
		hushDir:        hushDir,
		peers:          make(map[peer.ID]*PeerInfo),
		joinedTopics:   make(map[string]*pubsub.Topic),
		messageHistory: make(map[string]time.Time),
		bootstrapPeers: make([]peer.AddrInfo, 0),
	}

	// Set up protocol handlers
	h.SetStreamHandler(DiscoveryProtocol, node.handleDiscoveryStream)
	h.SetStreamHandler(ExchangeProtocol, node.handleExchangeStream)
	h.SetStreamHandler(PrivateChatProtocol, node.handlePrivateChatStream)

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

// Close shuts down the node.
func (n *DecentralizedNode) Close() error {
	n.cancel()
	return n.host.Close()
}
