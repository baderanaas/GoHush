
package libp2p

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	discovery "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/libp2p/go-libp2p/p2p/discovery/util"
)

// startGlobalDiscovery periodically finds new peers in the global namespace.
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

// processPeerDiscovery handles peers found via discovery.
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

// startPeerExchange periodically gossips with peers to exchange discovery info.
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

// exchangePeersWithNetwork selects a few connected peers to exchange info with.
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

// exchangeDiscoveryInfo sends our discovery info to a specific peer.
func (n *DecentralizedNode) exchangeDiscoveryInfo(peerID peer.ID) {
	s, err := n.host.NewStream(n.ctx, peerID, DiscoveryProtocol)
	if err != nil {
		log.Printf("Failed to open discovery stream to %s: %v\n", peerID, err)
		return
	}
	defer func() {
		if err := s.Close(); err != nil {
			log.Printf("Error closing stream: %v", err)
		}
	}()


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

// handleDiscoveryStream handles incoming discovery messages.
func (n *DecentralizedNode) handleDiscoveryStream(s network.Stream) {
	defer func() {
		if err := s.Close(); err != nil {
			log.Printf("Error closing stream: %v", err)
		}
	}()
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
		go func(addr string) {
			if err := n.connectToPeer(addr); err != nil {
				log.Printf("Failed to connect to peer from discovery: %v", err)
			}
		}(peerAddr)
	}
}

// handleExchangeStream is functionally similar to discovery in this context.
func (n *DecentralizedNode) handleExchangeStream(s network.Stream) {
	n.handleDiscoveryStream(s)
}
