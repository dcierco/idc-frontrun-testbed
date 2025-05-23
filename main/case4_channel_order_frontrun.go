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

	ibcTokenDenom     = "token"
	ibcTransferAmount = "10" // Small amount for each test packet
	attackerTxAmount  = "1" + ibcTokenDenom

	// --- ORDERED CHANNEL CONFIG ---
	// Ensure this path 'a-b-ordered' is configured in rly with 'order: ordered'
	ibcPathOrdered = "a-b-ordered"
	// Channel IDs for the ORDERED path (src channel on Chain A, dst channel on Chain B for recv_packet query)
	// Get these from `rly paths show a-b-ordered`
	// e.g. if chain A is src, channelA_ID_Ordered is src_channel_id on chain A
	channelA_ID_Ordered = "channel-0" // Example: src channel on chain A for ORDERED path
	channelB_ID_Ordered = "channel-0" // Example: dst channel on chain B for ORDERED path (used for recv_packet query)

	// --- UNORDERED CHANNEL CONFIG ---
	// Ensure this path 'a-b-unordered' is configured in rly with 'order: unordered'
	ibcPathUnordered = "a-b-unordered"
	// Channel IDs for the UNORDERED path
	// Get these from `rly paths show a-b-unordered`
	channelA_ID_Unordered = "channel-1" // Example: src channel on chain A for UNORDERED path
	channelB_ID_Unordered = "channel-1" // Example: dst channel on chain B for UNORDERED path
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
// --- End Structs ---

func main() {
	log.Println("Starting Case 4: Ordered vs. Unordered Channel Front-Running")

	if err := setup(); err != nil {
		log.Fatalf("Setup failed: %v", err)
	}

	// --- Run A: Test with ORDERED Channel ---
	log.Println("\n\n--- RUN A: Testing with ORDERED Channel ---")
	log.Printf("Path: %s, Chain A src channel: %s, Chain B dst channel: %s", ibcPathOrdered, channelA_ID_Ordered, channelB_ID_Ordered)
	runFrontRunTest("ORDERED", ibcPathOrdered, channelA_ID_Ordered, channelB_ID_Ordered)

	log.Println("\nWaiting a bit before starting the unordered channel test...")
	time.Sleep(15 * time.Second) // Give some time for things to settle, clear mempools etc.

	// --- Run B: Test with UNORDERED Channel ---
	log.Println("\n\n--- RUN B: Testing with UNORDERED Channel ---")
	log.Printf("Path: %s, Chain A src channel: %s, Chain B dst channel: %s", ibcPathUnordered, channelA_ID_Unordered, channelB_ID_Unordered)
	runFrontRunTest("UNORDERED", ibcPathUnordered, channelA_ID_Unordered, channelB_ID_Unordered)

	log.Println("\n\nCase 4 finished.")
}

func runFrontRunTest(channelType, ibcPath, srcChannelA, dstChannelB string) {
	log.Printf("[%s] Test: Simulating front-running scenario.", channelType)

	// 1. Initiate Multiple Victim Transfers (Packet1, Packet2)
	log.Printf("[%s] Step 1: userA sending two IBC transfers (Packet1, Packet2) via %s channel %s",
		channelType, channelType, srcChannelA)

	// Packet 1
	transfer1TxHashA, err := ibcTransfer(chainA_ID, chainA_RPC, chainA_Home, userA_KeyName, userBAddrOnB,
		srcChannelA, ibcTransferAmount+ibcTokenDenom, defaultFee)
	if err != nil {
		log.Printf("[%s] ERROR: Failed to send Packet1: %v", channelType, err)
		return
	}
	log.Printf("[%s] Packet1 sent. Tx hash on %s: %s", channelType, chainA_ID, transfer1TxHashA)
	time.Sleep(1 * time.Second) // Small delay between sends

	// Packet 2
	transfer2TxHashA, err := ibcTransfer(chainA_ID, chainA_RPC, chainA_Home, userA_KeyName, userBAddrOnB,
		srcChannelA, ibcTransferAmount+ibcTokenDenom, defaultFee)
	if err != nil {
		log.Printf("[%s] ERROR: Failed to send Packet2: %v", channelType, err)
		return
	}
	log.Printf("[%s] Packet2 sent. Tx hash on %s: %s", channelType, chainA_ID, transfer2TxHashA)
	log.Printf("[%s] Waiting for packets to be indexed on Chain A...", channelType)
	time.Sleep(6 * time.Second)

	// 2. Observe Packets
	log.Printf("[%s] Step 2: Observing packet sequence numbers on %s", channelType, chainA_ID)
	packet1Seq, err := findPacketSequenceFromTx(chainA_ID, chainA_RPC, transfer1TxHashA, srcChannelA)
	if err != nil {
		log.Printf("[%s] ERROR: Failed to find Packet1 sequence: %v", channelType, err)
		return
	}
	log.Printf("[%s] Observed Packet1 sequence: %s", channelType, packet1Seq)

	packet2Seq, err := findPacketSequenceFromTx(chainA_ID, chainA_RPC, transfer2TxHashA, srcChannelA)
	if err != nil {
		log.Printf("[%s] ERROR: Failed to find Packet2 sequence: %v", channelType, err)
		return
	}
	log.Printf("[%s] Observed Packet2 sequence: %s", channelType, packet2Seq)


	// 3. Attacker Action (Targeting Packet2) - before Packet2 is relayed/processed
	// We will first relay Packet1, then attacker acts, then relay Packet2.
	log.Printf("[%s] Step 3: Controlled Relaying of Packet1 (seq %s)", channelType, packet1Seq)
	err = relaySpecificPacket(rlyConfigPath, ibcPath, srcChannelA, packet1Seq)
	if err != nil {
		log.Printf("[%s] WARNING: Relaying Packet1 (seq %s) command failed: %v. It might have been relayed by another process or an issue occurred.", channelType, packet1Seq, err)
		// Continue, as the goal is to see if attacker can front-run Packet2
	} else {
		log.Printf("[%s] Packet1 (seq %s) relay command executed.", channelType, packet1Seq)
	}
	log.Printf("[%s] Waiting a moment for Packet1 to potentially process on Chain B...", channelType)
	time.Sleep(8 * time.Second) // Give Packet1 time to land

	// Attacker's action on Chain B
	log.Printf("[%s] Step 3b: AttackerB (%s) performing transaction on %s BEFORE Packet2 (seq %s) is relayed",
		channelType, attackerB_KeyName, chainB_ID, packet2Seq)
	attackerTxHashB, err := bankSend(chainB_ID, chainB_RPC, chainB_Home, attackerB_KeyName,
		attackerReceiverAddrOnB, attackerTxAmount, defaultFee)
	if err != nil {
		log.Printf("[%s] ERROR: Attacker's transaction failed: %v", channelType, err)
		return
	}
	log.Printf("[%s] Attacker's transaction submitted. Tx hash on %s: %s", channelType, chainB_ID, attackerTxHashB)
	log.Printf("[%s] Waiting for attacker's transaction to confirm...", channelType)
	time.Sleep(6 * time.Second)


	// 4. Controlled Relaying of Packet2
	log.Printf("[%s] Step 4: Controlled Relaying of Packet2 (seq %s) AFTER attacker's action", channelType, packet2Seq)
	err = relaySpecificPacket(rlyConfigPath, ibcPath, srcChannelA, packet2Seq)
	if err != nil {
		log.Printf("[%s] WARNING: Relaying Packet2 (seq %s) command failed: %v.", channelType, packet2Seq, err)
	} else {
		log.Printf("[%s] Packet2 (seq %s) relay command executed.", channelType, packet2Seq)
	}
	log.Printf("[%s] Waiting for Packet2 to process on Chain B...", channelType)
	time.Sleep(8 * time.Second)


	// 5. Verification
	log.Printf("[%s] Step 5: Verification on Chain B", channelType)
	attackerTxInfoB, err := queryTx(chainB_ID, chainB_RPC, attackerTxHashB)
	if err != nil {
		log.Printf("[%s] WARNING: Could not query attacker's tx info on %s: %v", channelType, chainB_ID, err)
	} else {
		log.Printf("[%s] Attacker's tx (%s) on %s in block: %s", channelType, attackerTxHashB, chainB_ID, attackerTxInfoB.Height)
	}

	// Verify Packet1 processing
	recvPacket1TxInfoB, errP1 := findRecvPacketTx(chainB_ID, chainB_RPC, dstChannelB, packet1Seq)
	if errP1 != nil {
		log.Printf("[%s] WARNING: Could not find RecvPacket for Packet1 (seq %s) on %s: %v", channelType, packet1Seq, chainB_ID, errP1)
	} else {
		log.Printf("[%s] Packet1 (seq %s) processed in tx %s on %s, block: %s",
			channelType, packet1Seq, recvPacket1TxInfoB.TxHash, chainB_ID, recvPacket1TxInfoB.Height)
	}

	// Verify Packet2 processing
	recvPacket2TxInfoB, errP2 := findRecvPacketTx(chainB_ID, chainB_RPC, dstChannelB, packet2Seq)
	if errP2 != nil {
		log.Printf("[%s] WARNING: Could not find RecvPacket for Packet2 (seq %s) on %s: %v", channelType, packet2Seq, chainB_ID, errP2)
	} else {
		log.Printf("[%s] Packet2 (seq %s) processed in tx %s on %s, block: %s",
			channelType, packet2Seq, recvPacket2TxInfoB.TxHash, chainB_ID, recvPacket2TxInfoB.Height)
	}

	// Compare block heights for front-running Packet2
	if attackerTxInfoB != nil && recvPacket2TxInfoB != nil {
		attackerHeight, _ := strconv.ParseInt(attackerTxInfoB.Height, 10, 64)
		recvPacket2Height, _ := strconv.ParseInt(recvPacket2TxInfoB.Height, 10, 64)

		log.Printf("[%s] Attacker Tx Block: %d, Packet2 Recv Block: %d", channelType, attackerHeight, recvPacket2Height)
		if attackerHeight < recvPacket2Height {
			log.Printf("[%s] RESULT: SUCCESSFUL Front-run. Attacker's tx (block %d) processed BEFORE Packet2 (block %d).",
				channelType, attackerHeight, recvPacket2Height)
		} else if attackerHeight == recvPacket2Height {
			log.Printf("[%s] RESULT: POTENTIAL Front-run. Attacker's tx and Packet2 are in the SAME block (%d). Intra-block order check needed for definitive result (not implemented in this script).",
				channelType, attackerHeight)
		} else {
			log.Printf("[%s] RESULT: FAILED Front-run. Attacker's tx (block %d) processed AFTER Packet2 (block %d).",
				channelType, attackerHeight, recvPacket2Height)
		}
	} else {
		log.Printf("[%s] RESULT: Inconclusive. Could not retrieve all necessary transaction details for Packet2 front-run analysis.", channelType)
	}

	// Check order of Packet1 and Packet2 processing for ORDERED channels
	if channelType == "ORDERED" && recvPacket1TxInfoB != nil && recvPacket2TxInfoB != nil {
		p1RecvHeight, _ := strconv.ParseInt(recvPacket1TxInfoB.Height, 10, 64)
		p2RecvHeight, _ := strconv.ParseInt(recvPacket2TxInfoB.Height, 10, 64)
		log.Printf("[%s] Packet1 Recv Block: %d, Packet2 Recv Block: %d", channelType, p1RecvHeight, p2RecvHeight)

		if p1RecvHeight > p2RecvHeight {
			log.Printf("[%s] CRITICAL ORDERING VIOLATION: Packet1 (seq %s) processed in block %d AFTER Packet2 (seq %s) in block %d on an ORDERED channel!",
				channelType, packet1Seq, p1RecvHeight, packet2Seq, p2RecvHeight)
		} else if p1RecvHeight == p2RecvHeight {
			// If in same block, need to check tx index. For now, just log.
			log.Printf("[%s] INFO: Packet1 and Packet2 processed in the SAME block (%d) on ORDERED channel. Intra-block order should ensure Packet1 is first.", channelType, p1RecvHeight)
			// A more robust check would involve querying the block and comparing tx indices of recvPacket1 and recvPacket2.
		} else {
			log.Printf("[%s] INFO: Packet1 (block %d) processed before Packet2 (block %d) as expected for ORDERED channel.", channelType, p1RecvHeight, p2RecvHeight)
		}
	} else if channelType == "UNORDERED" && recvPacket1TxInfoB != nil && recvPacket2TxInfoB != nil {
		p1RecvHeight, _ := strconv.ParseInt(recvPacket1TxInfoB.Height, 10, 64)
		p2RecvHeight, _ := strconv.ParseInt(recvPacket2TxInfoB.Height, 10, 64)
		log.Printf("[%s] Packet1 Recv Block: %d, Packet2 Recv Block: %d", channelType, p1RecvHeight, p2RecvHeight)
		log.Printf("[%s] INFO: For UNORDERED channels, Packet1 and Packet2 can be processed in any order.", channelType)
	}
	log.Printf("[%s] Test finished.", channelType)
}


// --- Helper Functions (many reused) ---
func setup() error {
	var err error

	userBAddrOnB, err = getKeyAddress(chainB_ID, chainB_RPC, userB_KeyName, chainB_Home, keyringBackend)
    if err != nil { return fmt.Errorf("getting userB address: %w", err) }
    attackerBAddrOnB, err = getKeyAddress(chainB_ID, chainB_RPC, attackerB_KeyName, chainB_Home, keyringBackend)
    if err != nil { return fmt.Errorf("getting attackerB address: %w", err) }
    attackerReceiverAddrOnB = attackerBAddrOnB

    log.Printf("Setup using Chain A Home: %s, Chain B Home: %s, Relayer Config: %s", chainA_Home, chainB_Home, rlyConfigPath)
    log.Printf("UserB: %s, AttackerB: %s", userBAddrOnB, attackerBAddrOnB)

    if ibcPathOrdered == "" || channelA_ID_Ordered == "" || channelB_ID_Ordered == "" {
        return fmt.Errorf("ORDERED channel configuration is incomplete in script constants")
    }
    if ibcPathUnordered == "" || channelA_ID_Unordered == "" || channelB_ID_Unordered == "" {
        return fmt.Errorf("UNORDERED channel configuration is incomplete in script constants")
    }
    if channelA_ID_Ordered == channelA_ID_Unordered && ibcPathOrdered == ibcPathUnordered { // Comparing constants
        log.Println("WARNING: Ordered and Unordered tests are configured to use the same path and channel IDs. Ensure they are distinct for a meaningful test.")
    }
    return nil
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
		// Check for common non-fatal stderr messages from rly
		lowerErr := strings.ToLower(stderr)
		if strings.Contains(lowerErr, "no packets to relay found") ||
		   strings.Contains(lowerErr, "already relayed") ||
		   strings.Contains(lowerErr, "light client state is not within trust period") || // Can happen if clients not updated
		   strings.Contains(lowerErr, "failed to send messages: 0/1 messages failed") { // Sometimes this means it worked but had other noise
			log.Printf("Relay command (path %s, seq %s, chan %s) had non-critical stderr: %s (Original error: %v)", pathName, sequence, srcChanID, stderr, err)
			return nil // Treat as non-fatal for the script's flow
		}
		return fmt.Errorf("relay packet (path %s, seq %s, chan %s): %w. Stderr: %s", pathName, sequence, srcChanID, err, stderr)
	}
	log.Printf("Relay command (path %s, seq %s, chan %s) executed. Stderr: %s", pathName, sequence, srcChanID, stderr)
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
