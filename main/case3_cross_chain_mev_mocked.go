package main

import (
	"bytes"
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
	defaultFee     = "1000stake"
	gasFlags       = "--gas=auto --gas-adjustment=1.2"

	userA_KeyName     = "userA"     // Victim account on Chain A
	userB_KeyName     = "userB"     // Recipient account on Chain B
	attackerB_KeyName = "attackerB" // Attacker account on Chain B

	// For mocked DeFi interaction on Chain B
	// Ensure these keys/addresses exist and are funded if they need to send tokens.
	// If they are just receiving, they only need to be valid addresses.
	// You can create these with `simd keys add <keyname> --keyring-backend test --home <chainBHome>`
	mockDexAccountB_KeyName   = "mockDexB" // Dummy DEX account on Chain B
	attackerStakeDenom = "stake"     // Denom attacker uses to "buy" token
	attackerTokenDenom = "token"     // Denom attacker "buys" and "sells" (same as ibcTokenDenom)

	// Amounts for mocked DeFi
	attackerPreemptiveBuyAmountStake = "50" + attackerStakeDenom // Attacker "buys" token with this much stake
	// Conceptual amount of 'token' attacker "receives" for the stake. For logging.
	// This isn't actually transferred to attacker unless mockDexAccountB sends it.
	// For this script, attacker just sends stake TO mockDexAccountB.

	// Victim's IBC transfer
	ibcTokenDenom     = "token" // Denom of the token being transferred via IBC (should match attackerTokenDenom)
	largeIBCTransferAmount = "500" + ibcTokenDenom // Large IBC transfer by victim

	// Attacker's "post-IBC sell" - attacker sends token to mockDexAccountB (simulating selling for stake)
	// This assumes attackerB has 'token' to sell (e.g., from a previous balance or the conceptual buy)
	// For simplicity, let's assume attackerB already has some 'token' to "sell".
	attackerPostIBCTokenSellAmount = "20" + attackerTokenDenom


	ibcPathName = "a-b-transfer" // Relayer path name
	// IMPORTANT: Verify these channel IDs from 'rly paths show a-b-transfer'
	channelA_ID_on_A = "channel-0" // Src channel on chain A for path a-b-transfer
	channelB_ID_on_B = "channel-0" // Src channel on chain B for path a-b-transfer
)
// --- End Configuration ---

// --- Structs (reused) ---
type Attribute struct { Key, Value string }
type Event struct { Type string; Attributes []Attribute }
type LogEntry struct { MsgIndex int; Events []Event }
type TxResponse struct {
	Height string; TxHash string; Code int; RawLog string; Logs []LogEntry
	GasWanted string; GasUsed string; Timestamp string
}
type SearchTxsResult struct { Txs []TxResponse }
type Coin struct { Denom, Amount string }
type BalanceResponse struct { Balances []Coin }
// --- End Structs ---

var (
	chainA_Home string
	chainB_Home string
	rlyConfigPath  string
	userBAddrOnB       string
	attackerBAddrOnB   string
	mockDexAddrOnB     string
)

func main() {
	log.Println("Starting Case 3: Cross-Chain MEV (Mocked DeFi Interaction)")

	if err := setup(); err != nil {
		log.Fatalf("Setup failed: %v", err)
	}

	// Initial Balances (optional, for clarity)
	logInitialBalances()

	// 1. Mock DeFi State (Conceptual) & 2. Attacker's "Pre-emptive Buy"
	log.Printf("Step 1 & 2: Attacker (%s) performs 'pre-emptive buy' on Chain B (sends %s to %s)",
		attackerB_KeyName, attackerPreemptiveBuyAmountStake, mockDexAccountB_KeyName)

	preemptiveBuyTxHash, err := bankSend(chainB_ID, chainB_RPC, chainB_Home, attackerB_KeyName,
		mockDexAddrOnB, attackerPreemptiveBuyAmountStake, defaultFee)
	if err != nil {
		log.Fatalf("Attacker's pre-emptive buy transaction failed: %v", err)
	}
	log.Printf("Attacker's 'pre-emptive buy' tx hash on %s: %s", chainB_ID, preemptiveBuyTxHash)
	log.Println("Waiting for attacker's pre-emptive buy to confirm...")
	time.Sleep(6 * time.Second)


	// 3. Initiate Victim IBC Transfer
	log.Printf("Step 3: userA (%s) on %s sending large IBC transfer (%s) to userB (%s) on %s",
		userA_KeyName, chainA_ID, largeIBCTransferAmount, userB_KeyName, chainB_ID)

	victimTransferTxHashA, err := ibcTransfer(chainA_ID, chainA_RPC, chainA_Home, userA_KeyName, userBAddrOnB,
		channelA_ID_on_A, largeIBCTransferAmount, defaultFee)
	if err != nil {
		log.Fatalf("Victim's large IBC transfer failed: %v", err)
	}
	log.Printf("Victim's large IBC transfer submitted on %s. Tx hash: %s", chainA_ID, victimTransferTxHashA)
	log.Println("Waiting for victim's transfer to be indexed on Chain A...")
	time.Sleep(6 * time.Second)


	// 4. Observe and Relay Packet
	log.Println("Step 4: Observing and relaying victim's IBC packet")
	packetSequence, err := findPacketSequenceFromTx(chainA_ID, chainA_RPC, victimTransferTxHashA, channelA_ID_on_A)
	if err != nil {
		log.Fatalf("Failed to find packet sequence from victim's transfer tx %s: %v", victimTransferTxHashA, err)
	}
	log.Printf("Observed SendPacket sequence: %s for channel %s", packetSequence, channelA_ID_on_A)

	err = relaySpecificPacket(rlyConfigPath, ibcPathName, channelA_ID_on_A, packetSequence)
	if err != nil {
		log.Printf("Warning: Relaying specific packet command failed: %v. Packet might have been relayed by another process.", err)
	} else {
		log.Println("Specific packet relay command executed for victim's transfer.")
	}
	log.Println("Waiting for victim's IBC transfer to be processed on Chain B...")
	time.Sleep(10 * time.Second)

	// Verify victim's packet was received on Chain B
	recvPacketTxInfoB, err := findRecvPacketTx(chainB_ID, chainB_RPC, channelB_ID_on_B, packetSequence)
	if err != nil {
		log.Fatalf("Victim's IBC packet (seq %s) not found on Chain B: %v. Aborting.", packetSequence, err)
	}
	log.Printf("Victim's IBC packet (seq %s) processed in tx %s on %s (block %s)",
		packetSequence, recvPacketTxInfoB.TxHash, chainB_ID, recvPacketTxInfoB.Height)


	// 5. Attacker's "Post-IBC Sell"
	log.Printf("Step 5: Attacker (%s) performs 'post-IBC sell' on Chain B (sends %s to %s)",
		attackerB_KeyName, attackerPostIBCTokenSellAmount, mockDexAccountB_KeyName)
	// This assumes attackerB has 'attackerTokenDenom' (e.g. 'token') to "sell".
	// Ensure attackerB is funded with 'token' prior to running the script if this is to succeed.
	postIBCTxHash, err := bankSend(chainB_ID, chainB_RPC, chainB_Home, attackerB_KeyName,
		mockDexAddrOnB, attackerPostIBCTokenSellAmount, defaultFee)
	if err != nil {
		log.Fatalf("Attacker's post-IBC sell transaction failed: %v", err)
	}
	log.Printf("Attacker's 'post-IBC sell' tx hash on %s: %s", chainB_ID, postIBCTxHash)
	log.Println("Waiting for attacker's post-IBC sell to confirm...")
	time.Sleep(6 * time.Second)


	// 6. Verification
	log.Println("Step 6: Verification - Logging transaction details and final balances")

	preemptiveBuyTxInfo, _ := queryTx(chainB_ID, chainB_RPC, preemptiveBuyTxHash)
	victimRecvTxInfo, _ := queryTx(chainB_ID, chainB_RPC, recvPacketTxInfoB.TxHash) // Already have this as recvPacketTxInfoB
	postIBCTxInfo, _ := queryTx(chainB_ID, chainB_RPC, postIBCTxHash)

	log.Println("--- Transaction Summary on Chain B ---")
	if preemptiveBuyTxInfo != nil {
		log.Printf("1. Attacker 'Pre-emptive Buy' Tx: %s, Block: %s, Timestamp: %s",
			preemptiveBuyTxInfo.TxHash, preemptiveBuyTxInfo.Height, preemptiveBuyTxInfo.Timestamp)
	}
	if victimRecvTxInfo != nil {
		log.Printf("2. Victim's IBC RecvPacket Tx:   %s, Block: %s, Timestamp: %s",
			victimRecvTxInfo.TxHash, victimRecvTxInfo.Height, victimRecvTxInfo.Timestamp)
	}
	if postIBCTxInfo != nil {
		log.Printf("3. Attacker 'Post-IBC Sell' Tx:  %s, Block: %s, Timestamp: %s",
			postIBCTxInfo.TxHash, postIBCTxInfo.Height, postIBCTxInfo.Timestamp)
	}

	// Check sequence of operations based on block heights (and ideally timestamps if available and reliable)
	validSequence := true
	if preemptiveBuyTxInfo == nil || victimRecvTxInfo == nil || postIBCTxInfo == nil {
		log.Println("Could not retrieve all transaction details for full sequence verification.")
		validSequence = false
	} else {
		buyHeight, _ := strconv.ParseInt(preemptiveBuyTxInfo.Height, 10, 64)
		recvHeight, _ := strconv.ParseInt(victimRecvTxInfo.Height, 10, 64)
		sellHeight, _ := strconv.ParseInt(postIBCTxInfo.Height, 10, 64)

		if !(buyHeight <= recvHeight && recvHeight <= sellHeight) {
			log.Printf("ERROR: Transaction sequence is NOT as expected based on block heights: Buy (H:%d), Recv (H:%d), Sell (H:%d)",
				buyHeight, recvHeight, sellHeight)
			validSequence = false
		} else {
			log.Printf("SUCCESS: Transaction sequence appears correct based on block heights: Buy (H:%d) -> Recv (H:%d) -> Sell (H:%d)",
				buyHeight, recvHeight, sellHeight)
			// For more precise check, if all in same block, would need intra-block tx index.
			// This script focuses on block height order for simplicity of "sandwich".
		}
	}
	if validSequence {
		log.Println("Mocked MEV scenario sequence executed.")
	} else {
		log.Println("Mocked MEV scenario sequence FAILED or was incomplete.")
	}


	log.Println("--- Final Balances on Chain B ---")
	logBalance(chainB_ID, chainB_RPC, userBAddrOnB, userB_KeyName, ibcTokenDenom)
	logBalance(chainB_ID, chainB_RPC, attackerBAddrOnB, attackerB_KeyName, attackerStakeDenom)
	logBalance(chainB_ID, chainB_RPC, attackerBAddrOnB, attackerB_KeyName, attackerTokenDenom)
	logBalance(chainB_ID, chainB_RPC, mockDexAddrOnB, mockDexAccountB_KeyName, attackerStakeDenom)
	logBalance(chainB_ID, chainB_RPC, mockDexAddrOnB, mockDexAccountB_KeyName, attackerTokenDenom)

	log.Println("Case 3 finished.")
}

// --- Helper Functions (many reused) ---
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

func logInitialBalances() {
	log.Println("--- Initial Balances on Chain B (for reference) ---")
	logBalance(chainB_ID, chainB_RPC, userBAddrOnB, userB_KeyName, ibcTokenDenom)
	logBalance(chainB_ID, chainB_RPC, attackerBAddrOnB, attackerB_KeyName, attackerStakeDenom)
	logBalance(chainB_ID, chainB_RPC, attackerBAddrOnB, attackerB_KeyName, attackerTokenDenom)
	logBalance(chainB_ID, chainB_RPC, mockDexAddrOnB, mockDexAccountB_KeyName, attackerStakeDenom)
	logBalance(chainB_ID, chainB_RPC, mockDexAddrOnB, mockDexAccountB_KeyName, attackerTokenDenom)
	log.Println("----------------------------------------------------")
}

func logBalance(chainID, node, address, keyName, denom string) {
	bal, err := queryBalance(chainID, node, address, denom)
	if err != nil {
		log.Printf("Balance query error for %s (%s) denom %s: %v", keyName, address, denom, err)
	} else {
		log.Printf("Balance of %s (%s): %s %s", keyName, address, bal, denom)
	}
}


func executeCommand(printCmd bool, name string, args ...string) (string, string, error) {
	cmd := exec.Command(name, args...)
	if printCmd { log.Printf("Executing: %s %s", name, strings.Join(args, " ")) }
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout; cmd.Stderr = &stderr
	err := cmd.Run()
	outStr, errStr := strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String())
	if err != nil {
		return outStr, errStr, fmt.Errorf("cmd '%s %s' failed: %w\nStdout: %s\nStderr: %s", name, strings.Join(args, " "), err, outStr, errStr)
	}
	return outStr, errStr, nil
}

func getKeyAddress(chainID, node, keyName, home, keyring string) (string, error) {
	args := []string{"keys", "show", keyName, "-a", "--keyring-backend", keyring, "--home", home, "--chain-id", chainID, "--node", node}
	stdout, _, err := executeCommand(false, simdBinary, args...)
	if err != nil { return "", fmt.Errorf("addr for key '%s': %w", keyName, err) }
	return strings.TrimSpace(stdout), nil
}

func ibcTransfer(chainID, node, home, fromKey, toAddr, srcChannel, amountToken, fee string) (string, error) {
	args := []string{"tx", "ibc-transfer", "transfer", "transfer", srcChannel, toAddr, amountToken,
		"--from", fromKey, "--chain-id", chainID, "--node", node, "--home", home,
		"--keyring-backend", keyringBackend, "--fees", fee, "-y", "-o", "json"}
	args = append(args, strings.Split(gasFlags, " ")...)
	stdout, _, err := executeCommand(true, simdBinary, args...)
	if err != nil { return "", err }
	var resp TxResponse
	if err := json.Unmarshal([]byte(stdout), &resp); err != nil { return "", fmt.Errorf("unmarshal: %w Resp: %s", err, stdout) }
	if resp.Code != 0 { return "", fmt.Errorf("tx code %d: %s", resp.Code, resp.RawLog) }
	return resp.TxHash, nil
}

func bankSend(chainID, node, home, fromKey, toAddr, amountToken, fee string) (string, error) {
	args := []string{"tx", "bank", "send", fromKey, toAddr, amountToken,
		"--chain-id", chainID, "--node", node, "--home", home,
		"--keyring-backend", keyringBackend, "--fees", fee, "-y", "-o", "json"}
	args = append(args, strings.Split(gasFlags, " ")...)
	stdout, _, err := executeCommand(true, simdBinary, args...)
	if err != nil { return "", err }
	var resp TxResponse
	if err := json.Unmarshal([]byte(stdout), &resp); err != nil { return "", fmt.Errorf("unmarshal: %w Resp: %s", err, stdout) }
	if resp.Code != 0 { return "", fmt.Errorf("tx code %d: %s", resp.Code, resp.RawLog) }
	return resp.TxHash, nil
}

func queryTx(chainID, node, txHash string) (*TxResponse, error) {
	args := []string{"query", "tx", txHash, "--chain-id", chainID, "--node", node, "-o", "json"}
	stdout, _, err := executeCommand(false, simdBinary, args...)
	if err != nil { return nil, err }
	var resp TxResponse
	if err := json.Unmarshal([]byte(stdout), &resp); err != nil { return nil, fmt.Errorf("unmarshal: %w Resp: %s", err, stdout) }
	if resp.TxHash == "" { return nil, fmt.Errorf("tx %s not found: %s", txHash, stdout) }
	return &resp, nil
}

func findPacketSequenceFromTx(chainID, node, txHash, srcChannelID string) (string, error) {
	txInfo, err := queryTx(chainID, node, txHash)
	if err != nil { return "", err }
	if txInfo.Code != 0 { return "", fmt.Errorf("tx %s code %d: %s", txHash, txInfo.Code, txInfo.RawLog) }
	for _, l := range txInfo.Logs {
		for _, e := range l.Events {
			if e.Type == "send_packet" {
				var ch, seq string
				for _, a := range e.Attributes {
					if a.Key == "packet_src_channel" { ch = a.Value }
					if a.Key == "packet_sequence" { seq = a.Value }
				}
				if ch == srcChannelID && seq != "" { return seq, nil }
			}
		}
	}
	return "", fmt.Errorf("seq not found in tx %s for chan %s. Log: %s", txHash, srcChannelID, txInfo.RawLog)
}

func relaySpecificPacket(rlyCfg, pathName, srcChanID, sequence string) error {
	args := []string{"tx", "relay-packets", pathName, "--src-channel", srcChanID, "--sequence", sequence, "--config", rlyCfg}
	_, stderr, err := executeCommand(true, rlyBinary, args...)
	if err != nil {
		if strings.Contains(strings.ToLower(stderr), "no packets to relay found") ||
		   strings.Contains(strings.ToLower(stderr), "already relayed") {
			log.Printf("Relay command (seq %s, chan %s): %s (non-fatal)", sequence, srcChanID, stderr)
			return nil
		}
		return fmt.Errorf("relay packet %s chan %s: %w. Stderr: %s", sequence, srcChanID, err, stderr)
	}
	return nil
}

func findRecvPacketTx(chainID, node, dstChannelID, sequence string) (*TxResponse, error) {
	events := fmt.Sprintf("recv_packet.packet_dst_channel='%s',recv_packet.packet_sequence='%s'", dstChannelID, sequence)
	args := []string{"query", "txs", "--events", events, "--node", node, "--chain-id", chainID, "-o", "json", "--limit=1"}
	stdout, _, err := executeCommand(false, simdBinary, args...)
	if err != nil { return nil, err }
	var res SearchTxsResult
	if err := json.Unmarshal([]byte(stdout), &res); err != nil { return nil, fmt.Errorf("unmarshal: %w Resp: %s", err, stdout) }
	if len(res.Txs) == 0 { return nil, fmt.Errorf("no RecvPacket tx for chan %s, seq %s", dstChannelID, sequence) }
	// Verify attributes
	for _, l := range res.Txs[0].Logs {
		for _, e := range l.Events {
			if e.Type == "recv_packet" {
				var ch, seq string
				for _, a := range e.Attributes {
					if a.Key == "packet_dst_channel" { ch = a.Value }
					if a.Key == "packet_sequence" { seq = a.Value }
				}
				if ch == dstChannelID && seq == sequence { return &res.Txs[0], nil }
			}
		}
	}
	return nil, fmt.Errorf("RecvPacket tx for chan %s, seq %s, attributes mismatch. Log: %s", dstChannelID, sequence, res.Txs[0].RawLog)
}

func queryBalance(chainID, node, address, denom string) (string, error) {
	args := []string{"query", "bank", "balances", address, "--denom", denom, "--node", node, "--chain-id", chainID, "-o", "json"}
	stdout, execErr, err := executeCommand(false, simdBinary, args...) // get execErr too
	if err != nil && !strings.Contains(stdout, `"amount":"0"`) && !strings.Contains(stdout, `amount:"0"`) { // if error and not clearly zero balance
		// If the denom doesn't exist for the account, the query might error or return an empty amount.
		// A specific error message for "denom not found for account" would be ideal to check.
		// For now, if there's an error, we'll check if it's a "not found" type of error.
		if execErr != nil && (strings.Contains(execErr.Error(), "not found") || strings.Contains(execErr.Error(), "no balance")) {
			return "0", nil
		}
		return "Error", fmt.Errorf("balance query for %s denom %s: %w. Output: %s", address, denom, err, stdout)
	}
	var coin Coin
	if errJson := json.Unmarshal([]byte(stdout), &coin); errJson != nil {
		// If stdout is empty or not JSON, and execErr indicated "not found", it's 0.
		if execErr != nil && (strings.Contains(execErr.Error(), "not found") || strings.Contains(execErr.Error(), "no balance")) {
			return "0", nil
		}
		// If it's like `{"balances":[],"pagination":{...}}` from a general query, or just empty from specific.
		if strings.TrimSpace(stdout) == "" || strings.Contains(stdout, `"balances":[]`) {
			return "0", nil
		}
		return "Error", fmt.Errorf("unmarshal balance for %s denom %s: %w. Output: %s", address, denom, errJson, stdout)
	}
	if coin.Denom == denom { return coin.Amount, nil }
	if coin.Amount == "" { return "0", nil } // If denom matches but amount is empty string.
	return "0", nil // Default to 0 if denom doesn't match (should not happen with --denom) or other cases.
}
