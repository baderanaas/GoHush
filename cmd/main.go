package main

import (
	"flag"
	"fmt"

	"github.com/baderanaas/GoHush/pkg/libp2p"
)

func main() {
	useNat := flag.Bool("nat", false, "Enable NAT traversal features")
	port := flag.Int("port", 0, "Listen port (random if not specified)")
	useUPnP := flag.Bool("upnp", false, "Enable UPnP port mapping")
	useRelay := flag.Bool("relay", false, "Enable AutoRelay for NAT traversal")
	useHolePunch := flag.Bool("hole-punch", false, "Enable hole punching")

	flag.Parse()

	if *useNat {
		fmt.Println("🚀 Starting GoHush with NAT traversal...")
		config := &libp2p.NodeConfig{
			EnableAutoRelay: *useRelay,
			EnableHolePunch: *useHolePunch,
			EnableUPnP:      *useUPnP,
			RelayNodes:      libp2p.DefaultRelayNodes,
		}
		libp2p.StartWithNat(*port, config)
	} else {
		fmt.Println("🚀 Starting basic GoHush node...")
		libp2p.StartBasic(*port)
	}
}
