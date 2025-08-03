# GoHush

GoHush is an encrypted peer-to-peer messaging system that provides a decentralized and secure communication platform. It leverages libp2p for its networking layer, ensuring that messages are sent directly between peers without a central server.

## Features

*   **Decentralized:** No central server, communication is peer-to-peer.
*   **End-to-End Encryption:** Ensures that only the sender and receiver can read the messages.
*   **Peer Discovery:** Automatically discovers other peers on the network.
*   **Contact Management:** Allows users to manage their contacts.
*   **Temporary Message Storage:** Messages are stored temporarily, enhancing privacy.

## Installation

To get started with GoHush, you need to have Go (version 1.21 or later) installed.

1.  **Clone the repository:**
    ```sh
    git clone https://github.com/baderanaas/GoHush.git
    cd GoHush
    ```

2.  **Install dependencies:**
    ```sh
    go mod tidy
    ```

## Usage

GoHush requires a `config.toml` file in the root directory. You can create one based on the example below:

```toml
# Port for the libp2p node to listen on
port = 4001

# Optional bootstrap node address
# relay = "/ip4/127.0.0.1/tcp/4001/p2p/..."
```

To run the application, use the following command:

```sh
go run cmd/main.go
```

## Building and Testing

To build the application, run:

```sh
go build -v ./...
```

To run the tests, use:
```sh
go test -v ./...
```

## Contributing

Contributions are welcome! Please feel free to submit a pull request.

## License

This project is under a proprietary license. See the [LICENSE](LICENSE) file for details.