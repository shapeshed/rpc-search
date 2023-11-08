// Package main is the entry point, containing the main function
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	rpchttp "github.com/tendermint/tendermint/rpc/client/http"
)

const (
	baseDenom    = "uosmo"
	targetPoolID = "1133"
	rpcURL       = "https://rpc.margined.io:443"
	query        = "token_swapped.module = 'gamm' AND tx.height > 12227140 AND token_swapped.pool_id = 1133"
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

func extractNumericValue(token string) (float64, error) {
	re := regexp.MustCompile(`\d+`)
	matches := re.FindAllString(token, -1)

	if len(matches) == 0 {
		return 0, fmt.Errorf("no numeric value found in token")
	}

	value, err := strconv.ParseFloat(matches[0], 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse numeric value: %v", err)
	}

	return value, nil
}

func calculateSpotPrice(baseValue, quoteValue float64) (string, error) {
	if baseValue == 0 {
		return "", fmt.Errorf("base value is zero, cannot calculate spot price")
	}

	spotPrice := quoteValue / baseValue

	// Format to 6 decimal places
	return fmt.Sprintf("%.6f", spotPrice), nil
}

func main() {
	c, err := rpchttp.New(rpcURL)
	if err != nil {
		panic(err)
	}

	page := 1
	perPage := 10

	for {
		result, err := c.TxSearch(context.Background(), query, true, &page, &perPage, "asc")
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
					if event.Type == "token_swapped" {
						var tokensIn, tokensOut, poolId, sender string
						var quote, base float64
						captureNextTokens := false
						var isBuy bool = true

						for _, attr := range event.Attributes {
							if attr.Key == "sender" {
								sender = attr.Value
							} else if captureNextTokens {
								if attr.Key == "tokens_in" {
									if strings.Contains(attr.Value, "uosmo") {
										isBuy = false
									}
									tokensIn = attr.Value
								} else if attr.Key == "tokens_out" {
									tokensOut = attr.Value
									break
								}
							} else if attr.Key == "pool_id" && attr.Value == targetPoolID {
								poolId = attr.Value
								captureNextTokens = true
							}
						}

						tokensInNumeric, err := extractNumericValue(tokensIn)
						if err != nil {
							panic(err)
						}

						tokensOutNumeric, err := extractNumericValue(tokensOut)
						if err != nil {
							panic(err)
						}

						if isBuy {
							quote = tokensInNumeric
							base = tokensOutNumeric
						} else {
							quote = tokensOutNumeric
							base = tokensInNumeric
						}

						spotPrice, err := calculateSpotPrice(base, quote)
						if err != nil {
							panic(err)
						}

						fmt.Printf("Height: %d\n", block.Block.Height)
						fmt.Printf("Unix Time: %d\n", block.Block.Time.Unix())
						fmt.Printf("Sender: %s\n", sender)
						fmt.Printf("Tokens In: %f\n", tokensInNumeric)
						fmt.Printf("Tokens Out: %f\n", tokensOutNumeric)
						fmt.Printf("Spot Price: %s\n", spotPrice)
						fmt.Printf("Pool id: %s\n", poolId)
						fmt.Printf("\n")
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
