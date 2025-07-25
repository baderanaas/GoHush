package main

import (
	"flag"
	"fmt"
	"github.com/baderanaas/GoHush/pkg/libp2p"
	"os"
)

func main() {
	var (
		port            int
		enableUPnP      bool
		enableAutoRelay bool
		enableHolePunch bool
	)

	flag.IntVar(&port, "port", 0, "Listen port (random if not specified)")
	flag.BoolVar(&enableUPnP, "upnp", false, "Enable UPnP port mapping")
	flag.BoolVar(&enableAutoRelay, "relay", false, "Enable AutoRelay for NAT traversal")
	flag.BoolVar(&enableHolePunch, "hole-punch", false, "Enable hole punching")
	flag.Parse()

	// If no flags are provided, start the basic node
	if len(os.Args) == 1 {
		fmt.Println("🚀 Starting basic GoHush node...")
		libp2p.StartBasic()
		return
	}

	config := &libp2p.NodeConfig{
		EnableAutoRelay: enableAutoRelay,
		EnableHolePunch: enableHolePunch,
		EnableUPnP:      enableUPnP,
		RelayNodes:      libp2p.DefaultRelayNodes,
	}

	if enableAutoRelay || enableHolePunch || enableUPnP {
		fmt.Printf("🔧 NAT Traversal enabled: UPnP=%v, AutoRelay=%v, HolePunch=%v\n",
			enableUPnP, enableAutoRelay, enableHolePunch)
	}

	node, err := libp2p.NewGoHushNode(port, config)
	if err != nil {
		fmt.Printf("❌ Failed to create node: %v\n", err)
		return
	}
	defer node.Close()

	if err := node.StartDiscovery(); err != nil {
		fmt.Printf("❌ Failed to start discovery: %v\n", err)
		return
	}

	if config.EnableAutoRelay {
		node.ConnectToRelayNodes()
	}

	go node.GetNATStatus()

	node.StartCLI()
}
