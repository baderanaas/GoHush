package libp2p

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/baderanaas/GoHush/pkg/crypto"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
)

// Intercepting a message for testing purposes
type testMessageHandler struct {
	done chan string
}

func (tmh *testMessageHandler) handle(chatMsg ChatMessage, topicDisplayName string, key []byte) {
	plaintext, err := crypto.Decrypt(chatMsg.Content, key)
	if err != nil {
		return // Ignore decryption errors in test
	}
	tmh.done <- string(plaintext)
}

func TestJoinTopicAndSendMessage(t *testing.T) {
	dir1 := newTestDir(t)
	node1, err := NewDecentralizedNode(0, dir1, "")
	require.NoError(t, err)
	defer func() { require.NoError(t, node1.Close()) }()

	dir2 := newTestDir(t)
	node2, err := NewDecentralizedNode(0, dir2, "")
	require.NoError(t, err)
	defer func() { require.NoError(t, node2.Close()) }()

	// Connect nodes
	addr, err := peer.AddrInfoToP2pAddrs(&peer.AddrInfo{ID: node2.host.ID(), Addrs: node2.host.Addrs()})
	require.NoError(t, err)
	err = node1.connectToPeer(addr[0].String())
	require.NoError(t, err)
	time.Sleep(2 * time.Second) // allow for connection and peer discovery

	// Create a new topic on node1
	topicName := "test-topic"
	topicInfo, err := node1.topicManager.CreateTopic(topicName)
	require.NoError(t, err)

	// Simulate invite by adding the same topic info to node2
	err = node2.topicManager.AddTopic(topicInfo)
	require.NoError(t, err)

	// Join topic
	err = node1.JoinTopic(topicInfo)
	require.NoError(t, err)
	err = node2.JoinTopic(topicInfo)
	require.NoError(t, err)
	time.Sleep(2 * time.Second) // allow for pubsub to sync

	// Send and receive message
	message := "hello world"
	handler := &testMessageHandler{done: make(chan string, 1)}

	// Temporarily override node2's message handler to intercept the message
	originalHandler := node2.messageHandler
	networkTopic := deriveNetworkTopic(topicInfo)
	key := crypto.KeyFromTopic(networkTopic)
	node2.messageHandler = func(chatMsg ChatMessage, topic string, _ []byte) {
		handler.handle(chatMsg, topic, key)
	}
	defer func() { node2.messageHandler = originalHandler }() // Restore handler

	time.Sleep(1 * time.Second) // wait for subscriber to be ready

	err = node1.SendMessage(message, topicName)
	require.NoError(t, err)

	select {
	case received := <-handler.done:
		require.Equal(t, message, received)
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for message")
	}
}

func TestSendPrivateMessage(t *testing.T) {
	dir1 := newTestDir(t)
	node1, err := NewDecentralizedNode(0, dir1, "")
	require.NoError(t, err)
	defer func() { require.NoError(t, node1.Close()) }()

	dir2 := newTestDir(t)
	node2, err := NewDecentralizedNode(0, dir2, "")
	require.NoError(t, err)
	defer func() { require.NoError(t, node2.Close()) }()

	// Connect nodes
	addr, err := peer.AddrInfoToP2pAddrs(&peer.AddrInfo{ID: node2.host.ID(), Addrs: node2.host.Addrs()})
	require.NoError(t, err)
	err = node1.connectToPeer(addr[0].String())
	require.NoError(t, err)
	time.Sleep(1 * time.Second)

	message := "private message"
	done := make(chan string, 1)

	// Set a stream handler on node2 to receive the message
	node2.host.SetStreamHandler(PrivateChatProtocol, func(s network.Stream) {
		// This now simulates the real handler more closely
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
