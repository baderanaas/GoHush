package libp2p

import (
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
)

func TestNewDecentralizedNode(t *testing.T) {
	dir := newTestDir(t)
	node, err := NewDecentralizedNode(0, dir, "")
	require.NoError(t, err)
	require.NotNil(t, node)
	defer func() { require.NoError(t, node.Close()) }()

	require.NotNil(t, node.host)
	require.NotNil(t, node.dht)
	require.NotNil(t, node.pubsub)
	require.NotNil(t, node.ctx)
}

func TestNodeToNodeConnection(t *testing.T) {
	dir1 := newTestDir(t)
	node1, err := NewDecentralizedNode(0, dir1, "")
	require.NoError(t, err)
	defer func() { require.NoError(t, node1.Close()) }()

	dir2 := newTestDir(t)
	node2, err := NewDecentralizedNode(0, dir2, "")
	require.NoError(t, err)
	defer func() { require.NoError(t, node2.Close()) }()

	node2AddrInfo := peer.AddrInfo{
		ID:    node2.host.ID(),
		Addrs: node2.host.Addrs(),
	}

	addr, err := peer.AddrInfoToP2pAddrs(&node2AddrInfo)
	require.NoError(t, err)

	err = node1.connectToPeer(addr[0].String())
	require.NoError(t, err)

	// Give some time for connection to establish
	time.Sleep(1 * time.Second)

	peers1 := node1.host.Network().Peers()
	found := false
	for _, p := range peers1 {
		if p == node2.host.ID() {
			found = true
			break
		}
	}
	require.True(t, found, "node1 should be connected to node2")
}

