package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

const (
	BlockInterval            uint64 = 2 // time interval between two consecutive blocks.
	InitialMaxBlockProposers uint64 = 5
	CheckpointInterval              = 180 // blocks between two bft checkpoints.
	NodeURL                         = "http://localhost:8689/"
	AddressLength                   = 20
)

type JSONBlockSummary struct {
	Number      uint32 `json:"number"`
	IsFinalized bool   `json:"isFinalized"`
}

type BlockResult struct {
	Best           uint32
	Justified      uint32
	Finalized      uint32
	AfterFinalized JSONBlockSummary
	Error          []string
}

func (br BlockResult) String() string {
	return fmt.Sprintf("Best: %d, Justified: %d, Finalized: %d, Error: %v", br.Best, br.Justified, br.Finalized, br.Error)
}

func producer(ch chan<- BlockResult, client *http.Client) {
	blockResult := &BlockResult{Error: make([]string, 0)}

	for range time.Tick(time.Duration(BlockInterval) * time.Second) {
		best, err := getBestBlock(client)
		if err != nil {
			fmt.Println("Error getting best block: ", err)
			blockResult.Error = append(blockResult.Error, fmt.Sprint("Error getting best block: ", err))
		}
		blockResult.Best = best

		justified, err := getJustifiedBlock(client)
		if err != nil {
			blockResult.Error = append(blockResult.Error, fmt.Sprint("Error getting justified block: ", err))
		}
		blockResult.Justified = justified

		finalized, err := getFinalizedBlock(client)
		if err != nil {
			blockResult.Error = append(blockResult.Error, fmt.Sprint("Error getting finalized block: ", err))
		}
		blockResult.Finalized = finalized

		afterFinalized, err := getBlockAfterFinalized(client, finalized)
		if err != nil {
			blockResult.Error = append(blockResult.Error, fmt.Sprint("Error getting before finalized block: ", err))
		}
		blockResult.AfterFinalized = afterFinalized

		ch <- *blockResult
	}
}

func getBestBlock(client *http.Client) (uint32, error) {
	res, err := client.Get(NodeURL + "blocks/best")
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("status code not 200: %s", res.Status)
	}

	responseBody, err := io.ReadAll(res.Body)
	if err != nil {
		return 0, fmt.Errorf("error reading response body: %w", err)
	}

	var block JSONBlockSummary
	if err = json.Unmarshal(responseBody, &block); err != nil {
		return 0, fmt.Errorf("unable to unmarshall events - %w", err)
	}

	return block.Number, nil
}

func getJustifiedBlock(client *http.Client) (uint32, error) {
	res, err := client.Get(NodeURL + "blocks/justified")
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("status code not 200: %s", res.Status)
	}

	responseBody, err := io.ReadAll(res.Body)
	if err != nil {
		return 0, fmt.Errorf("error reading response body: %w", err)
	}

	var block JSONBlockSummary
	if err = json.Unmarshal(responseBody, &block); err != nil {
		return 0, fmt.Errorf("unable to unmarshall events - %w", err)
	}

	return block.Number, nil
}

func getFinalizedBlock(client *http.Client) (uint32, error) {
	res, err := client.Get(NodeURL + "blocks/finalized")
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("status code not 200: %s", res.Status)
	}

	responseBody, err := io.ReadAll(res.Body)
	if err != nil {
		return 0, fmt.Errorf("error reading response body: %w", err)
	}

	var block JSONBlockSummary
	if err = json.Unmarshal(responseBody, &block); err != nil {
		return 0, fmt.Errorf("unable to unmarshall events - %w", err)
	}

	return block.Number, nil
}

func performChecks(r BlockResult) error {
	if r.Error != nil {
		return formatError(r.Error)
	}

	if r.Best < CheckpointInterval*2-1 {
		if r.Justified != 0 || r.Finalized != 0 {
			return fmt.Errorf("best block height less than 2 epochs - 1, justified and finalized block should be 0")
		}
	} else {
		if r.Justified-r.Finalized != CheckpointInterval {
			return fmt.Errorf("justified block number - finalized block number != CheckpointInterval")
		}
		if CheckpointInterval-1 > r.Best-r.Justified || r.Best-r.Justified >= CheckpointInterval*2-1 {
			return fmt.Errorf("179 <= head number - justified block number < 359")
		}
		if r.Best-r.Finalized < CheckpointInterval*2-1 || r.Best-r.Finalized >= CheckpointInterval*3-1 {
			return fmt.Errorf("finalized block number out of bound")
		}
		if r.AfterFinalized.IsFinalized {
			return fmt.Errorf("after finalized block number should not be finalized")
		}
	}

	return nil
}

func getBlockAfterFinalized(client *http.Client, finalized uint32) (JSONBlockSummary, error) {
	fmo := finalized + 1
	res, err := client.Get(NodeURL + "blocks/" + strconv.Itoa(int(fmo)))

	if err != nil {
		return JSONBlockSummary{}, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return JSONBlockSummary{}, fmt.Errorf("status code not 200: %s", res.Status)
	}

	responseBody, err := io.ReadAll(res.Body)
	if err != nil {
		return JSONBlockSummary{}, fmt.Errorf("error reading response body: %w", err)
	}

	var block JSONBlockSummary
	if err = json.Unmarshal(responseBody, &block); err != nil {
		return JSONBlockSummary{}, fmt.Errorf("unable to unmarshall events - %w", err)
	}

	return block, nil
}

func formatError(errs []string) error {
	s := ""
	for _, err := range errs {
		s += err + "\n"
	}
	return errors.New(s)
}

func main() {
	client := &http.Client{Timeout: 10 * time.Second}

	ch := make(chan BlockResult)

	go producer(ch, client)

	for blockResult := range ch {
		if err := performChecks(blockResult); err != nil {
			panic("Error while performing check: " + err.Error())
		}
	}
	// go consumer()
	// Poll each node every second for current block height at /blocks/best endpoint, if any error do nothing.
	// Poll each node every second for new justified block at /blocks/justified endpoint, if any error do nothing.
	// Poll each node every second for new finalized block at /blocks/finalized endpoint, if any error do nothing.
	// If the client cannot receive a response for more than 10 seconds, then terminate the program.
	// Check justifed and finalized consistency.
	/*
		1. As long as the current block height is less than 180:
			- justified block == finalized block == genesis block.
		2. Once the current block height is greater than or equal to 180:
			- justified block number - finalized block number == 180.
			- 180 <= head number - justified block number < 360.
			- finalized block number >= 360.
			- check the quality.
	*/
}

// func getCheckPoint(blockNum uint32) uint32 {
// 	return blockNum / CheckpointInterval * CheckpointInterval
// }

// func isCheckPoint(blockNum uint32) bool {
// 	return getCheckPoint(blockNum) == blockNum
// }

// // save quality at the end of round
// func getStorePoint(blockNum uint32) uint32 {
// 	return getCheckPoint(blockNum) + CheckpointInterval - 1
// }
