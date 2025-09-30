# go-regtest

A lightweight Go library for managing Bitcoin Core regtest environments with minimal dependencies. Perfect for integration testing, development, and prototyping Bitcoin applications.

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

## Features

- **Simple API**: Start and stop Bitcoin Core regtest nodes with a single function call
- **Pure Go**: 100% Go implementation with a minimal bash script for process management

## Prerequisites

- **Bitcoin Core**: Install via your system's package manager
  - macOS: `brew install bitcoin`
  - Ubuntu/Debian: `sudo apt-get install bitcoind`
  - Arch: `sudo pacman -S bitcoin-core`
- **Go**: Version 1.16 or higher

## Installation

```bash
go get github.com/neverDefined/go-regtest
```

## Quick Start

```go
package main

import (
    "log"
    
    "github.com/btcsuite/btcd/rpcclient"
    "github.com/neverDefined/go-regtest"
)

func main() {
    // Start the Bitcoin regtest node
    if err := regtest.StartBitcoinRegtest(); err != nil {
        log.Fatalf("Failed to start bitcoind: %v", err)
    }
    defer regtest.StopBitcoinRegtest()
    
    // Connect to the node
    client, err := rpcclient.New(regtest.DefaultRegtestConfig(), nil)
    if err != nil {
        log.Fatalf("Failed to create RPC client: %v", err)
    }
    defer client.Shutdown()
    
    // Interact with the node
    blockCount, err := client.GetBlockCount()
    if err != nil {
        log.Fatalf("Failed to get block count: %v", err)
    }
    
    log.Printf("Current block height: %d", blockCount)
}
```

## Usage

### Starting a Regtest Node

```go
err := regtest.StartBitcoinRegtest()
if err != nil {
    log.Fatal(err)
}
```

The node starts with:
- Network: regtest
- RPC Port: 18443
- Authentication: user/pass
- Data directory: `./bitcoind_regtest/`

### Stopping a Regtest Node

```go
err := regtest.StopBitcoinRegtest()
if err != nil {
    log.Fatal(err)
}
```

This performs a graceful shutdown and cleans up resources.

### Checking Node Status

```go
running, err := regtest.IsBitcoindRunning()
if err != nil {
    log.Fatal(err)
}

if running {
    log.Println("Bitcoin node is running")
}
```

### Using Custom RPC Configuration

While `DefaultRegtestConfig()` covers most use cases, you can customize the connection:

```go
config := &rpcclient.ConnConfig{
    Host:         "127.0.0.1:18443",
    User:         "your-user",
    Pass:         "your-pass",
    HTTPPostMode: true,
    DisableTLS:   true,
}

client, err := rpcclient.New(config, nil)
```

## API Reference

### Functions

#### `StartBitcoinRegtest() error`
Starts a Bitcoin Core regtest node. Thread-safe and idempotent.

**Returns:**
- `error`: Error if the node fails to start

#### `StopBitcoinRegtest() error`
Stops the Bitcoin Core regtest node and cleans up resources.

**Returns:**
- `error`: Error if the node fails to stop

#### `IsBitcoindRunning() (bool, error)`
Checks if the Bitcoin regtest node is currently running.

**Returns:**
- `bool`: true if running, false otherwise
- `error`: Error if the status check fails

#### `DefaultRegtestConfig() *rpcclient.ConnConfig`
Returns a pre-configured RPC connection config for the regtest network.

**Returns:**
- `*rpcclient.ConnConfig`: Configuration for connecting to the regtest node

## Testing

Run the included test suite:

```bash
go test -v
```

The test suite automatically:
1. Starts a regtest node
2. Connects via RPC
3. Performs a health check
4. Stops the node
5. Verifies cleanup

## Project Structure

```
go-regtest/
├── regtest.go              # Main library implementation
├── regtest_test.go         # Test suite
├── scripts/
│   └── bitcoind_manager.sh # Bitcoin Core process manager
├── bitcoind_regtest/       # Runtime data directory (created automatically)
├── go.mod
├── LICENSE
└── README.md
```
## Use Cases

- **Integration Testing**: Test Bitcoin applications against a real local node
- **Development**: Rapid prototyping without testnet/mainnet setup
- **CI/CD**: Automated testing in continuous integration pipelines
- **Education**: Learn Bitcoin development in a safe environment

## Troubleshooting

### Node Fails to Start

- Verify Bitcoin Core is installed: `which bitcoind`
- Check if port 18443 is available: `lsof -i :18443`
- Ensure the `scripts/bitcoind_manager.sh` script has execute permissions

### RPC Connection Fails

- Verify the node is running: `regtest.IsBitcoindRunning()`
- Check credentials match `DefaultRegtestConfig()`
- Ensure no firewall is blocking port 18443

### Script Not Found

The library uses automatic path discovery. If you encounter script path issues:
- Ensure `go.mod` exists in your project root
- Verify the `scripts/` directory is in the same root as `go.mod`

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Acknowledgments

- Built with [btcsuite/btcd](https://github.com/btcsuite/btcd) for RPC client functionality
- Inspired by the need for simple Bitcoin testing infrastructure

## Support

If you encounter any issues or have questions, please [open an issue](https://github.com/neverDefined/go-regtest/issues) on GitHub.

---

**Note**: This library is intended for development and testing purposes only. Do not use regtest nodes for production applications.
