package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// --- Configuration ---
// YOU MUST REVIEW AND UPDATE THESE VALUES FOR YOUR SETUP
const (
	keyringBackend = "test"
	defaultFee     = "1000stake" // Standard fee
	attackerHighFee = "5000stake" // Higher fee for attacker's transaction
	gasFlags       = "--gas=auto --gas-adjustment=1.3" // Slightly higher adjustment for attacker

	userA_KeyName     = "userA"     // Victim account on Chain A
	userB_KeyName     = "userB"     // Recipient account on Chain B (used to derive address)
	attackerB_KeyName = "attackerB" // Attacker account on Chain B

	ibcTokenDenom     = "token"      // Denom of the token being transferred via IBC
	ibcTransferAmount = "100"        // Amount of ibcTokenDenom to transfer
	attackerTxAmount  = "1" + ibcTokenDenom // Amount for attacker's transaction

	ibcPathName = "a-b-transfer" // Relayer path name
	// IMPORTANT: Verify these channel IDs from 'rly paths show a-b-transfer'
	channelA_ID_on_A = "channel-0" // Src channel on chain A for path a-b-transfer
	channelB_ID_on_B = "channel-0" // Src channel on chain B for path a-b-transfer (used for querying recv_packet on B)
)
// --- End Configuration ---

// --- Structs for JSON Parsing (some reused from Case 1) ---
type Attribute struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type Event struct {
	Type       string      `json:"type"`
	Attributes []Attribute `json:"attributes"`
}

type LogEntry struct {
	MsgIndex int     `json:"msg_index"`
	Log      string  `json:"log"`
	Events   []Event `json:"events"`
}

type TxResponse struct {
	Height    string     `json:"height"`
	TxHash    string     `json:"txhash"`
	Code      int        `json:"code"`
	RawLog    string     `json:"raw_log"`
	Logs      []LogEntry `json:"logs"`
	GasWanted string     `json:"gas_wanted"`
	GasUsed   string     `json:"gas_used"`
	Timestamp string     `json:"timestamp"`
}

type SearchTxsResult struct {
	Txs []TxResponse `json:"txs"`
}

type BlockData struct {
	Txs []string `json:"txs"` // List of base64-encoded transactions
}

type Block struct {
	Header struct {
		Height string `json:"height"`
	} `json:"header"`
	Data BlockData `json:"data"`
}

type BlockResponse struct {
	BlockID interface{} `json:"block_id"` // Can be complex, not strictly needed for this script
	Block   Block       `json:"block"`
}
// --- End Structs ---

func main() {
	log.Println("Starting Case 2: Validator Front-Running (Fee Manipulation)")

	if err := setup(); err != nil {
		log.Fatalf("Setup failed: %v", err)
	}

	// 1. Initiate Victim Transfer
	log.Printf("Step 1: userA (%s) on %s sending %s%s to userB (%s) on %s",
		userA_KeyName, chainA_ID, ibcTransferAmount, ibcTokenDenom, userB_KeyName, chainB_ID)
	transferTxHashA, err := ibcTransfer(chainA_ID, chainA_RPC, chainA_Home, userA_KeyName, userBAddrOnB,
		channelA_ID_on_A, ibcTransferAmount+ibcTokenDenom, defaultFee)
	if err != nil {
		log.Fatalf("Failed to initiate IBC transfer: %v", err)
	}
	log.Printf("Victim's IBC transfer submitted on %s. Tx hash: %s", chainA_ID, transferTxHashA)
	log.Println("Waiting for transaction to be indexed on Chain A...")
	time.Sleep(6 * time.Second)

	packetSequence, err := findPacketSequenceFromTx(chainA_ID, chainA_RPC, transferTxHashA, channelA_ID_on_A)
	if err != nil {
		log.Fatalf("Failed to find packet sequence from tx %s: %v", transferTxHashA, err)
	}
	log.Printf("Observed SendPacket sequence: %s for channel %s", packetSequence, channelA_ID_on_A)

	// 2. Relay to Mempool (by submitting relay command) & 3. Simultaneous Attacker Submission
	log.Println("Step 2 & 3: Relaying victim's packet and simultaneously submitting attacker's high-fee transaction to Chain B")

	// We'll execute the relayer command in a goroutine so it doesn't block the attacker's tx submission.
	// However, the relayer command itself might be quick. The goal is for both txs to hit the mempool around the same time.

	relayDone := make(chan error, 1)
	go func() {
		log.Printf("Submitting relay command for packet sequence %s (path: %s, src chan: %s)", packetSequence, ibcPathName, channelA_ID_on_A)
		// This command will try to submit MsgUpdateClient, MsgRecvPacket, etc.
		// We assume the relayer uses a standard/lower fee for these.
		errRelay := relaySpecificPacket(rlyConfigPath, ibcPathName, channelA_ID_on_A, packetSequence)
		if errRelay != nil {
			log.Printf("Warning: relaySpecificPacket command returned an error: %v. This might be okay if packet is already relayed or in flight.", errRelay)
		}
		relayDone <- errRelay // Send error or nil
	}()

	// Give a very brief moment for the relay command to start, or submit attacker tx immediately.
	// For "simultaneous", immediate is better. The rly command involves network calls.
	// time.Sleep(500 * time.Millisecond)

	log.Printf("AttackerB (%s) submitting transaction with high fee (%s) on %s", attackerB_KeyName, attackerHighFee, chainB_ID)
	attackerTxHashB, err := bankSend(chainB_ID, chainB_RPC, chainB_Home, attackerB_KeyName,
		attackerReceiverAddrOnB, attackerTxAmount, attackerHighFee) // Using attackerHighFee
	if err != nil {
		log.Fatalf("Attacker's high-fee transaction failed to submit: %v", err)
	}
	log.Printf("Attacker's high-fee transaction submitted on %s. Tx hash: %s", chainB_ID, attackerTxHashB)

	// Wait for relay goroutine to finish or timeout
	select {
	case errRelay := <-relayDone:
		if errRelay != nil {
			log.Printf("Relay command finished with error: %v", errRelay)
		} else {
			log.Println("Relay command finished successfully.")
		}
	case <-time.After(20 * time.Second): // Timeout for relay command
		log.Println("Warning: Timeout waiting for relay command to finish.")
	}

	log.Println("Waiting for transactions to be included in a block on Chain B...")
	time.Sleep(10 * time.Second) // Allow time for block inclusion

	// 4. Observation & 5. Verification
	log.Println("Step 4 & 5: Observation and Verification on Chain B")

	attackerTxInfoB, err := queryTx(chainB_ID, chainB_RPC, attackerTxHashB)
	if err != nil {
		log.Fatalf("Failed to query attacker's transaction %s on %s: %v", attackerTxHashB, chainB_ID, err)
	}
	log.Printf("Attacker's transaction %s on %s included in block: %s", attackerTxHashB, chainB_ID, attackerTxInfoB.Height)

	// Find the RecvPacket transaction related to the victim's transfer
	recvPacketTxInfoB, err := findRecvPacketTx(chainB_ID, chainB_RPC, channelB_ID_on_B, packetSequence)
	if err != nil {
		log.Fatalf("Failed to find RecvPacket transaction for sequence %s on %s: %v. The packet might not have been processed or the relay failed.", packetSequence, chainB_ID, err)
	}
	log.Printf("Victim's IBC packet (seq: %s) processed in tx %s on %s, block: %s",
		packetSequence, recvPacketTxInfoB.TxHash, chainB_ID, recvPacketTxInfoB.Height)

	if attackerTxInfoB.Height != recvPacketTxInfoB.Height {
		log.Printf("FAILURE: Attacker's tx (block %s) and RecvPacket tx (block %s) are in DIFFERENT blocks. Fee-based front-running for same-block priority not achieved as expected.",
			attackerTxInfoB.Height, recvPacketTxInfoB.Height)
		log.Println("Case 2 finished.")
		return
	}

	log.Printf("Both attacker's tx and RecvPacket tx are in the SAME block: %s. Checking intra-block order...", attackerTxInfoB.Height)

	blockInfo, err := queryBlock(chainB_ID, chainB_RPC, attackerTxInfoB.Height)
	if err != nil {
		log.Fatalf("Failed to query block %s on %s: %v", attackerTxInfoB.Height, chainB_ID, err)
	}

	attackerTxIndex := -1
	recvPacketTxIndex := -1

	// The hashes from queryTx are uppercase hex.
	attackerTxHashUpper := strings.ToUpper(attackerTxHashB)
	recvPacketTxHashUpper := strings.ToUpper(recvPacketTxInfoB.TxHash)

	for i, b64Tx := range blockInfo.Block.Data.Txs {
		txBytes, err := base64.StdEncoding.DecodeString(b64Tx)
		if err != nil {
			log.Printf("Warning: Failed to decode tx from block data at index %d: %v", i, err)
			continue
		}
		hash := sha256.Sum256(txBytes)
		txHashInBlock := strings.ToUpper(hex.EncodeToString(hash[:]))

		if txHashInBlock == attackerTxHashUpper {
			attackerTxIndex = i
		}
		if txHashInBlock == recvPacketTxHashUpper {
			recvPacketTxIndex = i
		}
	}

	log.Printf("Attacker's tx index in block: %d", attackerTxIndex)
	log.Printf("RecvPacket tx index in block: %d", recvPacketTxIndex)

	if attackerTxIndex != -1 && recvPacketTxIndex != -1 {
		if attackerTxIndex < recvPacketTxIndex {
			log.Printf("SUCCESS: Attacker's transaction (index %d) appeared BEFORE RecvPacket transaction (index %d) in block %s due to higher fee.",
				attackerTxIndex, recvPacketTxIndex, attackerTxInfoB.Height)
		} else {
			log.Printf("FAILURE: Attacker's transaction (index %d) did NOT appear before RecvPacket transaction (index %d) in block %s.",
				attackerTxIndex, recvPacketTxIndex, attackerTxInfoB.Height)
		}
	} else {
		log.Printf("ERROR: Could not find one or both transactions in the block's tx list by hash. Attacker found: %t, RecvPacket found: %t",
			attackerTxIndex != -1, recvPacketTxIndex != -1)
	}

	log.Println("Case 2 finished.")
}


// --- Helper Functions ---
func setup() error {
	var err error

	userBAddrOnB, err = getKeyAddress(chainB_ID, chainB_RPC, userB_KeyName, chainB_Home, keyringBackend)
	if err != nil { return fmt.Errorf("getting userB address: %w", err) }
	attackerBAddrOnB, err = getKeyAddress(chainB_ID, chainB_RPC, attackerB_KeyName, chainB_Home, keyringBackend)
	if err != nil { return fmt.Errorf("getting attackerB address: %w", err) }

	attackerReceiverAddrOnB = attackerBAddrOnB

	log.Printf("Setup complete. Chain A Home: %s, Chain B Home: %s, Relayer Config: %s", chainA_Home, chainB_Home, rlyConfigPath)
	log.Printf("User B address on Chain B: %s", userBAddrOnB)
	log.Printf("Attacker B address on Chain B: %s", attackerBAddrOnB)
	return nil
}

func executeCommand(printCmd bool, name string, args ...string) (string, string, error) {
	cmd := exec.Command(name, args...)
	if printCmd {
		log.Printf("Executing: %s %s", name, strings.Join(args, " "))
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	outStr := strings.TrimSpace(stdout.String())
	errStr := strings.TrimSpace(stderr.String())

	if err != nil {
		return outStr, errStr, fmt.Errorf("command '%s %s' failed: %w\nStdout: %s\nStderr: %s", name, strings.Join(args, " "), err, outStr, errStr)
	}
	return outStr, errStr, nil
}

func getKeyAddress(chainID, node, keyName, home, keyring string) (string, error) {
	args := []string{"keys", "show", keyName, "-a",
		"--keyring-backend", keyring,
		"--home", home,
		"--chain-id", chainID,
		"--node", node,
	}
	stdout, _, err := executeCommand(false, simdBinary, args...)
	if err != nil {
		return "", fmt.Errorf("getting address for key '%s': %w", keyName, err)
	}
	return strings.TrimSpace(stdout), nil
}

func ibcTransfer(chainID, node, home, fromKey, toAddr, srcChannel, amountToken, fee string) (string, error) {
	args := []string{"tx", "ibc-transfer", "transfer", "transfer", srcChannel, toAddr, amountToken,
		"--from", fromKey,
		"--chain-id", chainID,
		"--node", node,
		"--home", home,
		"--keyring-backend", keyringBackend,
		"--fees", fee,
		"-y", "-o", "json",
	}
	args = append(args, strings.Split(gasFlags, " ")...)

	stdout, _, err := executeCommand(true, simdBinary, args...)
	if err != nil {
		return "", fmt.Errorf("ibc transfer failed: %w", err)
	}

	var resp TxResponse
	if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
		return "", fmt.Errorf("failed to unmarshal ibc transfer tx response: %w\nResponse: %s", err, stdout)
	}
	if resp.Code != 0 {
		return "", fmt.Errorf("ibc transfer tx failed with code %d: %s", resp.Code, resp.RawLog)
	}
	return resp.TxHash, nil
}

func bankSend(chainID, node, home, fromKey, toAddr, amountToken, fee string) (string, error) {
	args := []string{"tx", "bank", "send", fromKey, toAddr, amountToken,
		"--chain-id", chainID,
		"--node", node,
		"--home", home,
		"--keyring-backend", keyringBackend,
		"--fees", fee, // Use the provided fee
		"-y", "-o", "json",
	}
	args = append(args, strings.Split(gasFlags, " ")...)

	stdout, _, err := executeCommand(true, simdBinary, args...)
	if err != nil {
		return "", fmt.Errorf("bank send failed: %w", err)
	}

	var resp TxResponse
	if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
		return "", fmt.Errorf("failed to unmarshal bank send tx response: %w\nResponse: %s", err, stdout)
	}
	if resp.Code != 0 {
		return "", fmt.Errorf("bank send tx failed with code %d: %s", resp.Code, resp.RawLog)
	}
	return resp.TxHash, nil
}

func queryTx(chainID, node, txHash string) (*TxResponse, error) {
	args := []string{"query", "tx", txHash,
		"--chain-id", chainID,
		"--node", node,
		"-o", "json",
	}
	stdout, _, err := executeCommand(false, simdBinary, args...)
	if err != nil {
		return nil, fmt.Errorf("querying tx %s failed: %w", txHash, err)
	}

	var resp TxResponse
	if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal query tx response for %s: %w\nResponse: %s", txHash, err, stdout)
	}
	if resp.TxHash == "" {
		return nil, fmt.Errorf("tx %s not found or invalid response: %s", txHash, stdout)
	}
	return &resp, nil
}

func findPacketSequenceFromTx(chainID, node, txHash, srcChannelID string) (string, error) {
	txInfo, err := queryTx(chainID, node, txHash)
	if err != nil {
		return "", fmt.Errorf("finding packet sequence, queryTx failed for %s: %w", txHash, err)
	}
	if txInfo.Code != 0 {
		return "", fmt.Errorf("tx %s failed with code %d: %s", txHash, txInfo.Code, txInfo.RawLog)
	}
	for _, logEntry := range txInfo.Logs {
		for _, event := range logEntry.Events {
			if event.Type == "send_packet" {
				var packetSrcChannel, packetSequence string
				for _, attr := range event.Attributes {
					if attr.Key == "packet_src_channel" { packetSrcChannel = attr.Value }
					if attr.Key == "packet_sequence" { packetSequence = attr.Value }
				}
				if packetSrcChannel == srcChannelID && packetSequence != "" {
					return packetSequence, nil
				}
			}
		}
	}
	return "", fmt.Errorf("packet sequence not found in tx %s for src_channel %s. Raw log: %s", txHash, srcChannelID, txInfo.RawLog)
}

func relaySpecificPacket(rlyCfg, pathName, srcChanID, sequence string) error {
	args := []string{"tx", "relay-packets", pathName,
		"--src-channel", srcChanID,
		"--sequence", sequence,
		"--config", rlyCfg,
	}
	_, stderr, err := executeCommand(true, rlyBinary, args...)
	// rly relay-packets can sometimes "succeed" but log errors to stderr if packet already relayed or no ack.
	// We check for common success messages or lack of "error" in stderr.
	if err != nil {
		if strings.Contains(strings.ToLower(stderr), "no packets to relay found") ||
		   strings.Contains(strings.ToLower(stderr), "already relayed") ||
		   strings.Contains(strings.ToLower(stderr), "acknowledgement from chain") { // This might indicate success too
			log.Printf("Relay command for packet %s on channel %s, path %s: %s (considered non-fatal)", sequence, srcChanID, pathName, stderr)
			return nil // Treat as non-fatal for this scenario
		}
		return fmt.Errorf("relaying specific packet %s on channel %s, path %s failed: %w. Stderr: %s", sequence, srcChanID, pathName, err, stderr)
	}
	log.Printf("Relay command for packet %s on channel %s, path %s potentially successful. Stderr: %s", sequence, srcChanID, pathName, stderr)
	return nil
}

func findRecvPacketTx(chainID, node, dstChannelID, sequence string) (*TxResponse, error) {
	eventQueries := fmt.Sprintf("recv_packet.packet_dst_channel='%s',recv_packet.packet_sequence='%s'", dstChannelID, sequence)
	args := []string{"query", "txs", "--events", eventQueries,
		"--node", node,
		"--chain-id", chainID,
		"-o", "json",
		"--limit", "1", // We only expect one such event for a unique sequence
		"--order_by", "desc", // Get the latest if multiple (should not happen for unique seq)
	}
	stdout, _, err := executeCommand(false, simdBinary, args...)
	if err != nil {
		return nil, fmt.Errorf("querying txs by events for RecvPacket failed: %w", err)
	}

	var searchResult SearchTxsResult
	if err := json.Unmarshal([]byte(stdout), &searchResult); err != nil {
		return nil, fmt.Errorf("failed to unmarshal SearchTxsResult for RecvPacket: %w\nResponse: %s", err, stdout)
	}

	if len(searchResult.Txs) == 0 {
		return nil, fmt.Errorf("no RecvPacket transaction found for channel %s, sequence %s", dstChannelID, sequence)
	}

	// Double check the event attributes from the first Tx found
	for _, logEntry := range searchResult.Txs[0].Logs {
		for _, event := range logEntry.Events {
			if event.Type == "recv_packet" {
				var foundDstChannel, foundSeq string
				for _, attr := range event.Attributes {
					if attr.Key == "packet_dst_channel" { foundDstChannel = attr.Value }
					if attr.Key == "packet_sequence" { foundSeq = attr.Value }
				}
				if foundDstChannel == dstChannelID && foundSeq == sequence {
					return &searchResult.Txs[0], nil
				}
			}
		}
	}
	return nil, fmt.Errorf("no RecvPacket transaction found with matching event attributes for channel %s, sequence %s, despite initial query success. Raw log: %s", dstChannelID, sequence, searchResult.Txs[0].RawLog)
}

func queryBlock(chainID, node, height string) (*BlockResponse, error) {
	args := []string{"query", "block", height,
		"--node", node,
		"--chain-id", chainID,
		"-o", "json",
	}
	stdout, _, err := executeCommand(false, simdBinary, args...)
	if err != nil {
		return nil, fmt.Errorf("querying block %s failed: %w", height, err)
	}

	var resp BlockResponse
	if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal query block response for height %s: %w\nResponse: %s", height, err, stdout)
	}
	return &resp, nil
}
