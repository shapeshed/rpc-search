// package main provides a tx search for osmosis
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/lib/pq"
	rpchttp "github.com/tendermint/tendermint/rpc/client/http"
	"github.com/tendermint/tendermint/rpc/coretypes"
)

// ... [Unchanged types and constants] ...
const (
	baseDenom    = "uosmo"
	targetPoolID = "1133"
	rpcURL       = "https://rpc.margined.io:443"
	query        = "token_swapped.module = 'gamm' AND tx.height > 12227140 AND token_swapped.pool_id = 1267"
)

var db *sql.DB

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

func initDB() error {
	var err error
	// Replace with your database connection information
	connStr := "user=postgres dbname=margined_osmosis_1 sslmode=disable password=mypassword"
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		return err
	}

	return db.Ping() // Ensure the connection is alive
}

func calculateSpotPrice(baseValue, quoteValue float64) (string, error) {
	if baseValue == 0 {
		return "", fmt.Errorf("base value is zero, cannot calculate spot price")
	}

	spotPrice := quoteValue / baseValue

	// Format to 6 decimal places
	return fmt.Sprintf("%.6f", spotPrice), nil
}

// exponentialBackoff retries a function with an exponential backoff strategy.
func exponentialBackoff(attemptFunc func() error) error {
	const maxRetries = 5
	const baseDelay = time.Second

	var err error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if err = attemptFunc(); err == nil {
			return nil
		}
		wait := time.Duration(math.Pow(2, float64(attempt))) * baseDelay
		fmt.Printf("Attempt %d failed; retrying in %v: %v\n", attempt+1, wait, err)
		time.Sleep(wait)
	}
	return fmt.Errorf("after %d attempts, last error: %s", maxRetries, err)
}

func processTransaction(tx *coretypes.ResultTx, c *rpchttp.HTTP) {
	block, err := c.Block(context.Background(), &tx.Height)
	if err != nil {
		log.Printf("Error fetching block data for height %d: %v", tx.Height, err)
		return
	}

	rawLogParsed := new([]TxInfoLog)
	if unmarshalErr := json.Unmarshal([]byte(tx.TxResult.Log), rawLogParsed); unmarshalErr != nil {
		log.Printf("Error unmarshalling log data: %v", unmarshalErr)
		return
	}

	for _, txLog := range *rawLogParsed {
		for _, event := range txLog.Events {
			if event.Type == "token_swapped" {
				var tokensIn, tokensOut, poolID, sender string
				var quote, base float64
				var isBuy bool

				captureNextTokens := false

				for _, attr := range event.Attributes {
					if attr.Key == "sender" {
						sender = attr.Value
					} else if attr.Key == "pool_id" {
						if attr.Value == targetPoolID {
							// Flag that the next tokens_in and tokens_out should be captured.
							poolID = attr.Value
							captureNextTokens = true
						} else {
							// Reset the flag if we encounter a pool_id that is not the target.
							captureNextTokens = false
						}
					} else if captureNextTokens {
						if attr.Key == "tokens_in" {
							tokensIn = attr.Value
							if strings.Contains(attr.Value, baseDenom) {
								isBuy = false
							}
						} else if attr.Key == "tokens_out" {
							tokensOut = attr.Value
							// We've captured both values, so we can reset the flag.
							captureNextTokens = false
						}
					}
				}

				// Continue only if both tokens_in and tokens_out are found for the target poolId
				if poolID == targetPoolID && tokensIn != "" && tokensOut != "" {
					tokensInNumeric, err := extractNumericValue(tokensIn)
					if err != nil {
						log.Printf("Error parsing tokens_in numeric value: %v", err)
						continue
					}

					tokensOutNumeric, err := extractNumericValue(tokensOut)
					if err != nil {
						log.Printf("Error parsing tokens_out numeric value: %v", err)
						continue
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
						log.Printf("Error calculating spot price: %v", err)
						continue
					}

					_, err = db.Exec(`INSERT INTO base_gamm_pool (pool_id, quote, base, spot_price, timestamp, block_height) VALUES ($1, $2, $3, $4, $5, $6)`,
						poolID, quote, base, spotPrice, block.Block.Time.Unix(), block.Block.Height)
					if err != nil {
						log.Printf("Error inserting data into database: %v", err)
					} else {
						log.Printf("Data inserted for pool ID: %s\n", poolID)
					}

					log.Printf("Height: %d\n", block.Block.Height)
					log.Printf("Unix Time: %d\n", block.Block.Time.Unix())
					log.Printf("Sender: %s\n", sender)
					log.Printf("Tokens In: %f\n", tokensInNumeric)
					log.Printf("Tokens Out: %f\n", tokensOutNumeric)
					log.Printf("Spot Price: %s\n", spotPrice)
					log.Printf("Pool ID: %s\n", poolID)
				}
			}
		}
	}
}

func main() {
	c, err := rpchttp.New(rpcURL)
	if err != nil {
		log.Fatalf("Failed to create RPC client: %v", err)
	}

	err = initDB()
	if err != nil {
		log.Fatalf("Failed to initialize the database: %v", err)
	}
	defer db.Close()

	// Use a WaitGroup to wait for all goroutines to complete.
	var wg sync.WaitGroup
	page := 1
	perPage := 1
	var totalPages int // Used to capture the total pages after the first fetch

	// Use a channel to limit the number of concurrent goroutines.
	concurrency := 10
	semaphore := make(chan struct{}, concurrency)

	// Initially, set totalPages high to enter the loop
	totalPages = math.MaxInt32

	for page <= totalPages {
		semaphore <- struct{}{} // Acquire a token.
		wg.Add(1)

		go func(page int) {
			defer wg.Done()                // Signal the WaitGroup that the goroutine is done.
			defer func() { <-semaphore }() // Release the token.

			var localTotalPages int // To capture total pages from this goroutine

			err := exponentialBackoff(func() error {
				result, err := c.TxSearch(context.Background(), query, true, &page, &perPage, "asc")
				if err != nil {
					return err
				}

				// Process the transactions
				for _, tx := range result.Txs {
					processTransaction(tx, c)
				}

				// Capture the total number of pages on the first fetch
				if page == 1 {
					localTotalPages = (result.TotalCount + perPage - 1) / perPage
				}

				return nil
			})

			if err != nil {
				log.Printf("Failed to fetch or process transactions for page %d: %v", page, err)
			}

			// If this was the first page, set the total pages for the main loop
			if page == 1 {
				totalPages = localTotalPages
			}
		}(page)

		page++ // Go to the next page
	}

	wg.Wait() // Wait for all goroutines to complete.
}
