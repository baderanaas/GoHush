package libp2p

const (
	// Protocol IDs for different services
	DiscoveryProtocol   = "/gohush/discovery/1.0.0"
	ExchangeProtocol    = "/gohush/exchange/1.0.0"
	PrivateChatProtocol = "/gohush/private-chat/1.0.0"

	// DHT namespaces for different discovery types
	GlobalNamespace   = "gohush-global"
	TopicNamespace    = "gohush-topic"
	InterestNamespace = "gohush-interest"
)
