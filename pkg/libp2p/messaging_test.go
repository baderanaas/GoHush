package libp2p

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/baderanaas/GoHush/pkg/crypto"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
)

func TestJoinTopicAndSendMessage(t *testing.T) {
	dir1 := newTestDir(t)
	node1, err := NewDecentralizedNode(0, dir1)
	require.NoError(t, err)
	defer func() { require.NoError(t, node1.Close()) }()

	dir2 := newTestDir(t)
	node2, err := NewDecentralizedNode(0, dir2)
	require.NoError(t, err)
	defer func() { require.NoError(t, node2.Close()) }()

	// Connect nodes
	addr, err := peer.AddrInfoToP2pAddrs(&peer.AddrInfo{ID: node2.host.ID(), Addrs: node2.host.Addrs()})
	require.NoError(t, err)
	err = node1.connectToPeer(addr[0].String())
	require.NoError(t, err)
	time.Sleep(2 * time.Second) // allow for connection and peer discovery

	// Join topic
	topic := "test-topic"
	err = node1.JoinTopic(topic)
	require.NoError(t, err)
	err = node2.JoinTopic(topic)
	require.NoError(t, err)
	time.Sleep(2 * time.Second) // allow for pubsub to sync

	// Send and receive message
	message := "hello world"
	done := make(chan string)

	// We need to read the message from node2
	go func() {
		n2topic, ok := node2.joinedTopics[topic]
		require.True(t, ok)
		sub, err := n2topic.Subscribe()
		require.NoError(t, err)
		msg, err := sub.Next(context.Background())
		require.NoError(t, err)
		var chatMsg ChatMessage
		err = json.Unmarshal(msg.Data, &chatMsg)
		require.NoError(t, err)
		key := crypto.KeyFromTopic(topic)
		plaintext, err := crypto.Decrypt(chatMsg.Content, key)
		require.NoError(t, err)
		done <- string(plaintext)
	}()

	time.Sleep(1 * time.Second) // wait for subscriber to be ready

	err = node1.SendMessage(message, topic)
	require.NoError(t, err)

	select {
	case received := <-done:
		require.Equal(t, message, received)
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for message")
	}
}

func TestSendPrivateMessage(t *testing.T) {
	dir1 := newTestDir(t)
	node1, err := NewDecentralizedNode(0, dir1)
	require.NoError(t, err)
	defer func() { require.NoError(t, node1.Close()) }()

	dir2 := newTestDir(t)
	node2, err := NewDecentralizedNode(0, dir2)
	require.NoError(t, err)
	defer func() { require.NoError(t, node2.Close()) }()

	// Connect nodes
	addr, err := peer.AddrInfoToP2pAddrs(&peer.AddrInfo{ID: node2.host.ID(), Addrs: node2.host.Addrs()})
	require.NoError(t, err)
	err = node1.connectToPeer(addr[0].String())
	require.NoError(t, err)
	time.Sleep(1 * time.Second)

	message := "private message"
	done := make(chan string)

	// Set a stream handler on node2 to receive the message
	node2.host.SetStreamHandler(PrivateChatProtocol, func(s network.Stream) {
		decoder := json.NewDecoder(s)
		var msg ChatMessage
		err := decoder.Decode(&msg)
		require.NoError(t, err)
		key := crypto.KeyFromPeers(node2.host.ID(), s.Conn().RemotePeer())
		plaintext, err := crypto.Decrypt(msg.Content, key)
		require.NoError(t, err)
		done <- string(plaintext)
	})

	err = node1.SendPrivateMessage(message, node2.host.ID())
	require.NoError(t, err)

	select {
	case received := <-done:
		require.Equal(t, message, received)
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for private message")
	}
}

