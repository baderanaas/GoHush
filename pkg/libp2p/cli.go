package libp2p

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// StartDecentralizedCLI starts the command-line interface for the node.
func (n *DecentralizedNode) StartDecentralizedCLI() {
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Printf("\n✅ Fully Decentralized & Encrypted P2P Chat Started!\n")
	fmt.Printf("Commands:\n")
	fmt.Printf("  /join <topic>              - Join an encrypted chat topic\n")
	fmt.Printf("  /leave <topic>             - Leave a chat topic\n")
	fmt.Printf("  /history <topic|peer> [n]  - Show the last [n] messages (default 50)\n")
	fmt.Printf("  /topics                    - Show joined topics\n")
	fmt.Printf("  /peers                     - List network peers and their full IDs\n")
	fmt.Printf("  /connect <addr>            - Connect to a specific peer\n")
	fmt.Printf("  /disconnect <peerID>       - Disconnect from a specific peer\n")
	fmt.Printf("  /disconnect-all            - Disconnect from all peers\n")
	fmt.Printf("  /msg <topic> <msg>         - Send a message to a specific topic\n")
	fmt.Printf("  /private <peerID|name> <msg> - Send a private message to a peer or contact\n")
	fmt.Printf("  /contacts                  - List all contacts\n")
	fmt.Printf("  /add-contact <name> <peerID> - Add a new contact\n")
	fmt.Printf("  /switch <topic>            - Switch the current chat topic\n")
	fmt.Printf("  /quit                      - Exit\n")
	fmt.Printf("  <message>                  - Send to the current topic\n")
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
			} else {
				n.currentTopic = topic
				// Load and display recent messages
				messages, err := LoadRecentMessages(topic, 20, n.hushDir)
				if err != nil {
					log.Printf("⚠️ Could not load message history: %v", err)
				}
				fmt.Printf("--- History for %s ---\n", topic)
				for _, msg := range messages {
					fromShort := msg.From
					if len(fromShort) > 12 {
						fromShort = fromShort[:12]
					}
					fmt.Printf("[%s] %s: %s\n", msg.Timestamp.Format("15:04"), fromShort, msg.Content)
				}
				fmt.Println("--- End of history ---")
			}
		case strings.HasPrefix(input, "/leave "):
			topic := strings.TrimSpace(input[7:])
			if err := n.LeaveTopic(topic); err != nil {
				log.Printf("❌ Failed to leave topic: %v\n", err)
			} else {
				fmt.Printf("✅ Left topic: %s\n", topic)
				if n.currentTopic == topic {
					n.currentTopic = ""
				}
			}
		case strings.HasPrefix(input, "/history "):
			parts := strings.Fields(input)
			if len(parts) < 2 {
				fmt.Println("Usage: /history <topic_or_peer_id> [count]")
				continue
			}
			logID := parts[1]
			count := 50 // Default message count
			if len(parts) > 2 {
				var err error
				count, err = strconv.Atoi(parts[2])
				if err != nil {
					fmt.Println("Invalid count, must be a number.")
					continue
				}
			}

			messages, err := LoadRecentMessages(logID, count, n.hushDir)
			if err != nil {
				log.Printf("⚠️ Could not load message history for %s: %v", logID, err)
				continue
			}

			fmt.Printf("--- History for %s (last %d messages) ---\n", logID, len(messages))
			for _, msg := range messages {
				fromShort := msg.From
				if len(fromShort) > 12 {
					fromShort = fromShort[:12]
				}
				fmt.Printf("[%s] %s: %s\n", msg.Timestamp.Format("15:04"), fromShort, msg.Content)
			}
			fmt.Println("--- End of history ---")

		case input == "/topics":
			n.joinedTopicsMux.RLock()
			if len(n.joinedTopics) == 0 {
				fmt.Println("No active topics. Use /join <topic> to start.")
			} else {
				fmt.Println("Joined topics:")
				for topic := range n.joinedTopics {
					if topic == n.currentTopic {
						fmt.Printf("  - %s (current)\n", topic)
					} else {
						fmt.Printf("  - %s\n", topic)
					}
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
		case strings.HasPrefix(input, "/disconnect "):
			peerIDStr := strings.TrimSpace(input[12:])
			peerID, err := peer.Decode(peerIDStr)
			if err != nil {
				fmt.Printf("❌ Invalid peer ID: %v\n", err)
				continue
			}
			if err := n.DisconnectFromPeer(peerID); err != nil {
				log.Printf("❌ Failed to disconnect from peer: %v\n", err)
			} else {
				fmt.Printf("✅ Disconnected from peer: %s\n", peerIDStr)
			}
		case input == "/disconnect-all":
			n.DisconnectFromAllPeers()
			fmt.Println("✅ Disconnected from all peers.")

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
				fmt.Println("Usage: /private <peerID|name> <message>")
				continue
			}
			
			peerIDStr := parts[0]
			var peerID peer.ID
			var err error
			
			// Try to find peerID from contacts first
			contact, found := n.contactManager.GetContact(peerIDStr)
			if found {
				peerID, err = peer.Decode(contact.PeerID)
				if err != nil {
					fmt.Printf("❌ Invalid peer ID for contact %s: %v\n", peerIDStr, err)
					continue
				}
			} else {
				// If not a contact, assume it's a peerID
				peerID, err = peer.Decode(peerIDStr)
				if err != nil {
					fmt.Printf("❌ Invalid peer ID or contact name: %v\n", err)
					continue
				}
			}

			if err := n.SendPrivateMessage(parts[1], peerID); err != nil {
				log.Printf("❌ Failed to send private message: %v\n", err)
			}
		
		case input == "/contacts":
			contacts := n.contactManager.ListContacts()
			if len(contacts) == 0 {
				fmt.Println("No contacts found. Use /add-contact <name> <peerID> to add one.")
			} else {
				fmt.Println("Contacts:")
				for _, contact := range contacts {
					fmt.Printf("  - %s: %s\n", contact.Name, contact.PeerID)
				}
			}
		
		case strings.HasPrefix(input, "/add-contact "):
			parts := strings.SplitN(input[13:], " ", 2)
			if len(parts) < 2 {
				fmt.Println("Usage: /add-contact <name> <peerID>")
				continue
			}
			name := parts[0]
			peerID := parts[1]
			
			// Validate peerID
			_, err := peer.Decode(peerID)
			if err != nil {
				fmt.Printf("❌ Invalid peer ID: %v\n", err)
				continue
			}

			n.contactManager.AddContact(name, peerID)
			if err := n.contactManager.SaveContacts(); err != nil {
				log.Printf("❌ Failed to save contacts: %v\n", err)
			} else {
				fmt.Printf("✅ Contact '%s' added.\n", name)
			}
		
		case strings.HasPrefix(input, "/switch "):
			topic := strings.TrimSpace(input[8:])
			n.joinedTopicsMux.RLock()
			_, exists := n.joinedTopics[topic]
			n.joinedTopicsMux.RUnlock()
			if !exists {
				fmt.Printf("You are not in topic '%s'. Use /join %s to join it.\n", topic, topic)
			} else {
				n.currentTopic = topic
				fmt.Printf("Switched to topic '%s'\n", topic)
			}

		default:
			if n.currentTopic == "" {
				fmt.Println("No active topic. Use /join <topic> or /switch <topic>.")
				continue
			}
			if err := n.SendMessage(input, n.currentTopic); err != nil {
				log.Printf("❌ Failed to send message to topic %s: %v\n", n.currentTopic, err)
			}
		}
		fmt.Print("> ")
	}
}

// StartDecentralized is the main entry point for the decentralized mode.
func StartDecentralized(port int, relayAddr string) error {
	if port == 0 {
		// Use a random port to allow multiple nodes on the same machine
		port = 8000 + int(time.Now().Unix()%1000)
	}

	// Pass an empty string to use the default user directory
	node, err := NewDecentralizedNode(port, "", relayAddr)
	if err != nil {
		return fmt.Errorf("failed to create decentralized node: %w", err)
	}
	defer func() {
		if err := node.Close(); err != nil {
			log.Printf("Error closing node: %v", err)
		}
	}()

	if err := node.Bootstrap(); err != nil {
		return fmt.Errorf("failed to bootstrap: %w", err)
	}

	time.Sleep(3 * time.Second)

	node.StartDecentralizedCLI()
	return nil
}
