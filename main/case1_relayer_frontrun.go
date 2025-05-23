package main

import (
	"bytes"
	"crypto/sha256"
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

// --- Configuration Constants (values not typically changed by Makefile for this specific case) ---
    const (
        keyringBackend = "test"
        defaultFee     = "1000stake"
        gasFlags       = "--gas=auto --gas-adjustment=1.2"

        userA_KeyName     = "userA" // Victim account on Chain A
        userB_KeyName     = "userB" // Recipient account on Chain B (used to derive address)
        attackerB_KeyName = "attackerB" // Attacker account on Chain B

        ibcTokenDenom     = "token"      // Denom of the token being transferred via IBC
        ibcTransferAmount = "100"        // Amount of ibcTokenDenom to transfer
        attackerTxAmount  = "1" + ibcTokenDenom // Amount for attacker's transaction

        ibcPathName      = "a-b-transfer" // Default path for this case from Makefile's RLY_PATH_AB_TRANSFER
        // IMPORTANT: Verify these channel IDs from 'rly paths show a-b-transfer'
        // These are the SOURCE channel IDs on their respective chains for the given path.
        channelA_ID_on_A = "channel-0" // Example: src channel on chain A for path a-b-transfer
        channelB_ID_on_B = "channel-0" // Example: src channel on chain B for path a-b-transfer (used for querying recv_packet on B)
    )
// --- End Configuration Constants ---

// --- Structs for JSON Parsing ---
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

type Coin struct {
	Denom  string `json:"denom"`
	Amount string `json:"amount"`
}

type BalanceResponse struct {
	Balances []Coin `json:"balances"`
}
// --- End Structs ---

func main() {
	log.Println("Starting Case 1: Relayer Front-Running Scenario")

	if err := setup(); err != nil {
		log.Fatalf("Setup failed: %v", err)
	}

	// 1. Initiate Victim Transfer: userA on Chain A sends an IBC transfer
	log.Printf("Step 1: userA (%s) on %s sending %s%s to userB (%s) on %s",
		userA_KeyName, chainA_ID, ibcTransferAmount, ibcTokenDenom, userB_KeyName, chainB_ID)

	transferTxHash, err := ibcTransfer(chainA_ID, chainA_RPC, chainA_Home, userA_KeyName, userBAddrOnB,
		channelA_ID_on_A, ibcTransferAmount+ibcTokenDenom, defaultFee)
	if err != nil {
		log.Fatalf("Failed to initiate IBC transfer: %v", err)
	}
	log.Printf("Victim's IBC transfer submitted. Tx hash on %s: %s", chainA_ID, transferTxHash)
	log.Println("Waiting a few seconds for the transaction to be indexed...")
	time.Sleep(6 * time.Second) // Give time for indexing

	// 2. Observe Packet: Query Chain A to find the sequence number
	log.Println("Step 2: Observing packet sequence number on", chainA_ID)
	packetSequence, err := findPacketSequenceFromTx(chainA_ID, chainA_RPC, transferTxHash, channelA_ID_on_A)
	if err != nil {
		log.Fatalf("Failed to find packet sequence: %v", err)
	}
	log.Printf("Observed SendPacket sequence: %s for channel %s", packetSequence, channelA_ID_on_A)

	// 3. Attacker Action on Chain B: attackerB performs a transaction
	log.Printf("Step 3: attackerB (%s) on %s performing a transaction BEFORE victim's packet is relayed", attackerB_KeyName, chainB_ID)
	attackerTxHashOnB, err := bankSend(chainB_ID, chainB_RPC, chainB_Home, attackerB_KeyName,
		attackerReceiverAddrOnB, attackerTxAmount, defaultFee)
	if err != nil {
		log.Fatalf("Attacker's transaction failed: %v", err)
	}
	log.Printf("Attacker's transaction submitted. Tx hash on %s: %s", chainB_ID, attackerTxHashOnB)
	log.Println("Waiting a few seconds for attacker's transaction to be confirmed...")
	time.Sleep(6 * time.Second)


	// 4. Controlled Relaying: Manually trigger relayer for the specific packet
	log.Printf("Step 4: Manually relaying specific packet (seq: %s) using path %s, src channel %s",
		packetSequence, ibcPathName, channelA_ID_on_A)
	err = relaySpecificPacket(rlyConfigPath, ibcPathName, channelA_ID_on_A, packetSequence)
	if err != nil {
		// It's possible the relayer might have already picked it up if running in background.
		// We'll proceed to verification, but log this as a warning.
		log.Printf("Warning: Relaying specific packet command failed: %v. The packet might have been relayed by another process.", err)
	} else {
		log.Println("Specific packet relay command executed.")
	}
	log.Println("Waiting a few seconds for packet to be relayed and processed...")
	time.Sleep(10 * time.Second)


	// 5. Verification
	log.Println("Step 5: Verification")

	// Get block height of attacker's transaction on Chain B
	attackerTxInfo, err := queryTx(chainB_ID, chainB_RPC, attackerTxHashOnB)
	if err != nil {
		log.Printf("Warning: Could not query attacker's tx info on %s: %v", chainB_ID, err)
	} else {
		log.Printf("Attacker's transaction (%s) on %s was included in block: %s", attackerTxHashOnB, chainB_ID, attackerTxInfo.Height)
	}

	// Find RecvPacket transaction on Chain B and its block height
	recvPacketTxInfo, err := findRecvPacketTx(chainB_ID, chainB_RPC, channelB_ID_on_B, packetSequence)
	if err != nil {
		log.Printf("Warning: Could not find RecvPacket tx for sequence %s on %s: %v. The packet may not have been processed.", packetSequence, chainB_ID, err)
	} else {
		log.Printf("Victim's IBC packet (seq: %s) was processed in tx %s on %s in block: %s",
			packetSequence, recvPacketTxInfo.TxHash, chainB_ID, recvPacketTxInfo.Height)

		if attackerTxInfo != nil {
			attackerHeight, _ := strconv.ParseInt(attackerTxInfo.Height, 10, 64)
			recvPacketHeight, _ := strconv.ParseInt(recvPacketTxInfo.Height, 10, 64)

			if attackerHeight < recvPacketHeight {
				log.Printf("SUCCESS: Attacker's tx (block %d) processed before victim's RecvPacket (block %d).", attackerHeight, recvPacketHeight)
			} else if attackerHeight == recvPacketHeight {
				log.Printf("INFO: Attacker's tx and victim's RecvPacket are in the SAME block (%d). Manual check of intra-block order might be needed if tx index is not available or not parsed.", attackerHeight)
				// Further check intra-block order if possible (requires parsing block.data.txs)
				// For this script, we'll consider same block as potential front-run if attacker's tx is confirmed.
			} else {
				log.Printf("FAILURE: Attacker's tx (block %d) processed after victim's RecvPacket (block %d).", attackerHeight, recvPacketHeight)
			}
		}
	}

	// Query final balances
	userBBalance, err := queryBalance(chainB_ID, chainB_RPC, userBAddrOnB, ibcTokenDenom)
	if err != nil {
		log.Printf("Warning: Could not query userB balance on %s: %v", chainB_ID, err)
	} else {
		log.Printf("Final balance of userB (%s) on %s: %s %s", userB_KeyName, chainB_ID, userBBalance, ibcTokenDenom)
	}

	attackerBBalance, err := queryBalance(chainB_ID, chainB_RPC, attackerBAddrOnB, ibcTokenDenom)
	if err != nil {
		log.Printf("Warning: Could not query attackerB balance on %s: %v", chainB_ID, err)
	} else {
		log.Printf("Final balance of attackerB (%s) on %s: %s %s", attackerB_KeyName, chainB_ID, attackerBBalance, ibcTokenDenom)
	}

	log.Println("Case 1 finished.")
}

// --- Helper Functions ---
func setup() error {
	var err error

	userBAddrOnB, err = getKeyAddress(chainB_ID, chainB_RPC, userB_KeyName, chainB_Home, keyringBackend)
    if err != nil { return fmt.Errorf("getting userB address: %w", err) }
    attackerBAddrOnB, err = getKeyAddress(chainB_ID, chainB_RPC, attackerB_KeyName, chainB_Home, keyringBackend)
    if err != nil { return fmt.Errorf("getting attackerB address: %w", err) }

    attackerReceiverAddrOnB = attackerBAddrOnB

    log.Printf("Setup using Chain A Home: %s, Chain B Home: %s, Relayer Config: %s", chainA_Home, chainB_Home, rlyConfigPath)
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
	// simd tx ibc-transfer transfer <src-port> <src-channel> <receiver> <amount> --from <key> --chain-id <chain-id> --node <node> --home <home> -y --fees <fees>
	// Assuming 'transfer' is the port.
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
		"--fees", fee,
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
	if resp.TxHash == "" { // Basic check if unmarshalling was incomplete or tx not found
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
					if attr.Key == "packet_src_channel" {
						packetSrcChannel = attr.Value
					}
					if attr.Key == "packet_sequence" {
						packetSequence = attr.Value
					}
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
	// rly tx relay-packets [path-name] --sequence <sequence> --src-channel <channel-id>
	args := []string{"tx", "relay-packets", pathName,
		"--src-channel", srcChanID,
		"--sequence", sequence,
		"--config", rlyCfg,
	}
	_, _, err := executeCommand(true, rlyBinary, args...)
	return err
}

func findRecvPacketTx(chainID, node, dstChannelID, sequence string) (*TxResponse, error) {
	// Query for txs with recv_packet event, specific channel and sequence
	// simd query txs --events 'recv_packet.packet_dst_channel=channel-0&recv_packet.packet_sequence=1'
	eventQueries := fmt.Sprintf("recv_packet.packet_dst_channel='%s',recv_packet.packet_sequence='%s'", dstChannelID, sequence)
	args := []string{"query", "txs", "--events", eventQueries,
		"--node", node,
		"--chain-id", chainID,
		"-o", "json",
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
	if len(searchResult.Txs) > 1 {
		log.Printf("Warning: Found multiple RecvPacket transactions for channel %s, sequence %s. Using the first one.", dstChannelID, sequence)
	}

	// Verify the event attributes more carefully from the first Tx found
	for _, logEntry := range searchResult.Txs[0].Logs {
		for _, event := range logEntry.Events {
			if event.Type == "recv_packet" {
				var foundDstChannel, foundSeq string
				for _, attr := range event.Attributes {
					if attr.Key == "packet_dst_channel" {
						foundDstChannel = attr.Value
					}
					if attr.Key == "packet_sequence" {
						foundSeq = attr.Value
					}
				}
				if foundDstChannel == dstChannelID && foundSeq == sequence {
					return &searchResult.Txs[0], nil // Return the full TxResponse
				}
			}
		}
	}
	return nil, fmt.Errorf("no RecvPacket transaction found with matching event attributes for channel %s, sequence %s, despite initial query success", dstChannelID, sequence)
}

func queryBalance(chainID, node, address, denom string) (string, error) {
	args := []string{"query", "bank", "balances", address,
		"--denom", denom,
		"--node", node,
		"--chain-id", chainID,
		"-o", "json",
	}
	stdout, _, err := executeCommand(false, simdBinary, args...)
	if err != nil {
		// If the specific denom balance is not found, it might return an error or empty.
		// The command `query bank balances` returns all balances if --denom is not specified.
		// If --denom is specified and the coin is not there, it might return an empty coin object or error.
		// Let's try to parse it anyway.
		// A typical response for a specific denom is: {"denom":"token","amount":"1000"}
		// Or if not found: could be error or empty balances array if using general balances query.
		// For `balances <addr> --denom <denom>` it should be specific.
		// If it errors because denom not found, that's fine, means 0.
		log.Printf("Query balance for %s on %s returned: %s", address, denom, stdout)
	}

	// The output for a specific denom is just a Coin object, not BalanceResponse
	// e.g. {"denom":"stake","amount":"999998980"}
	var coin Coin
	if errUnmarshal := json.Unmarshal([]byte(stdout), &coin); errUnmarshal != nil {
		// It might be that the account has NO balance of this denom, which can result in an error from the CLI or empty output.
		// Let's check if the error from executeCommand was about "not found" or similar.
		if err != nil && (strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no balance")) {
			return "0", nil // Treat as zero balance
		}
		// If there was no execution error but unmarshal failed, or other execution error
		return "Error", fmt.Errorf("failed to unmarshal balance query response for %s: %w. Output: %s. Original exec error: %v", address, errUnmarshal, stdout, err)
	}

	if coin.Denom == denom {
		return coin.Amount, nil
	}
	// If denom doesn't match or amount is empty, assume 0 if no error from executeCommand
	if err == nil { // No execution error, but denom didn't match (should not happen with --denom) or amount empty
	    return "0", nil
	}
	return "Error", fmt.Errorf("could not find balance for denom %s for address %s. Raw output: %s", denom, address, stdout)
}

// sha256HashBytes computes the SHA256 hash of a byte slice and returns its hex representation.
func sha256HashBytes(data []byte) string {
	h := sha256.New()
	h.Write(data)
	return strings.ToUpper(hex.EncodeToString(h.Sum(nil)))
}
