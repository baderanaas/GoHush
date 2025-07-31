package main

import (
	"fmt"
	"github.com/baderanaas/GoHush/pkg/libp2p"
	"github.com/spf13/viper"
	"log"
	"os"
)

func run() error {
	// Set up configuration management with Viper
	viper.SetConfigName("config") // name of config file (without extension)
	viper.SetConfigType("toml")   // REQUIRED if the config file does not have the extension in the name
	viper.AddConfigPath(".")      // optionally look for config in the working directory
	if err := viper.ReadInConfig(); err != nil { // Handle errors reading the config file
		return fmt.Errorf("fatal error config file: %w", err)
	}

	// Get configuration values
	port := viper.GetInt("port")
	relayAddr := viper.GetString("relay")

	return libp2p.StartDecentralized(port, relayAddr)
}

func main() {
	if err := run(); err != nil {
		log.Printf("❌ Error: %v", err)
		os.Exit(1)
	}
}
