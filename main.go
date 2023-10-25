// Package main is the entry point, containing the main function
package main

import (
	"context"
	"encoding/json"
	"fmt"

	rpchttp "github.com/tendermint/tendermint/rpc/client/http"
)

// TxInfoLog represents Log data in a transaction.
type TxInfoLog struct {
	MsgIndex uint64        `json:"msg_index"`
	Log      string        `json:"log"`
	Events   []TxInfoEvent `json:"events"`
}

// TxInfoAttribute represents an attribute within a Log item.
type TxInfoAttribute struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// TxInfoEvent represents an Event within a Log item.
type TxInfoEvent struct {
	Type       string            `json:"type"`
	Attributes []TxInfoAttribute `json:"attributes"`
}

func main() {
	c, err := rpchttp.New("https://rpc.margined.io:443")
	if err != nil {
		panic(err)
	}

	page := 1
	perPage := 10

	for {
		result, err := c.TxSearch(context.Background(),
			"wasm-apply_funding._contract_address = 'osmo1cnj84q49sp4sd3tsacdw9p4zvyd8y46f2248ndq2edve3fqa8krs9jds9g'", true, &page, &perPage, "asc")
		if err != nil {
			panic(err)
		}

		for _, tx := range result.Txs {
			block, err := c.Block(context.Background(), &tx.Height)
			if err != nil {
				panic(err)
			}

			rawLogParsed := new([]TxInfoLog)
			if unmarshalErr := json.Unmarshal([]byte(tx.TxResult.Log), rawLogParsed); unmarshalErr != nil {
				panic(err)
			}

			for _, log := range *rawLogParsed {
				for _, event := range log.Events {
					if event.Type == "wasm-apply_funding" {
						for _, attr := range event.Attributes {
							if attr.Key == "funding_rate" {
								// Process data to PostgreSQL here, doing an upsert
								fmt.Printf("Height: %+v\n", tx.Height)
								fmt.Printf("Time: %+v\n", block.Block.Time)
								fmt.Printf("Funding Rate: %s\n", attr.Value)
							}
						}
					}
				}
			}

		}

		// If there are no more pages, exit the loop
		if len(result.Txs) < perPage {
			break
		}

		// Increment the page number for the next request
		page++
	}
}
