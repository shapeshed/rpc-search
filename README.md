# Tendermint RPC Search

POC for an service that uses the `tx_search` endpoint from the [CometBFT
(Tendermint) RPC][1], with pagination.

This can retrieve historical events for use with a bot or indexer.

## Installation

- `go mod download`
- `go run main.go`
- `go build`

## Usage

```sh
./rpc-search
```

The process writes data to the console.

[1]: https://docs.cometbft.com/main/rpc/
