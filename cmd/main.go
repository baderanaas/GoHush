package main

import (
	"flag"
	"fmt"

	"github.com/baderanaas/GoHush/pkg/libp2p"
)

func main() {
	var useNat bool
	flag.BoolVar(&useNat, "nat", false, "Enable NAT traversal features")
	flag.Parse()

	if useNat {
		fmt.Println("🚀 Starting GoHush with NAT traversal...")
		libp2p.StartWithNat()
	} else {
		fmt.Println("🚀 Starting basic GoHush node...")
		libp2p.StartBasic()
	}
}
