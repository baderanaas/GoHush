package main

import (
	"flag"
	"github.com/baderanaas/GoHush/pkg/libp2p"
)

func main() {
	var port int
	var relayAddr string
	flag.IntVar(&port, "port", 0, "Listen port (random if not specified)")
	flag.StringVar(&relayAddr, "relay", "", "Custom relay server multiaddress")
	flag.Parse()

	// Start the fully decentralized node.
	// All configuration is now handled internally by the node.
	libp2p.StartDecentralized(port, relayAddr)
}
