
package libp2p

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	discovery "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/libp2p/go-libp2p/p2p/discovery/util"
	"github.com/multiformats/go-multiaddr"
)

// maintainNetwork runs background tasks to keep the network healthy.
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

// cleanupStaleConnections periodically removes peers that haven't been seen in a while.
func (n *DecentralizedNode) cleanupStaleConnections() {
	n.peersMux.Lock()
	defer n.peersMux.Unlock()
	staleThreshold := time.Now().Add(-60 * time.Minute)
	for id, peerInfo := range n.peers {
		if peerInfo.LastSeen.Before(staleThreshold) && n.host.Network().Connectedness(id) == network.NotConnected {
			delete(n.peers, id)
		}
	}
}

// ensureConnectivity checks if the node has enough connections and triggers more discovery if isolated.
func (n *DecentralizedNode) ensureConnectivity() {
	connected := len(n.host.Network().Peers())
	if connected < 3 {
		fmt.Printf("⚠️ Low connectivity (%d peers), boosting discovery...\n", connected)
		n.announcePresence()
	}
}

// announcePresence advertises our presence in the global and topic-specific namespaces.
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

// connectToPeer connects to a peer given its multiaddress string.
func (n *DecentralizedNode) connectToPeer(addrStr string) error {
	addr, err := multiaddr.NewMultiaddr(addrStr)
	if err != nil {
		return err
	}
	peerInfo, err := peer.AddrInfoFromP2pAddr(addr)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(n.ctx, 30*time.Second)
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

// ListPeers prints the list of known peers and their status.
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
