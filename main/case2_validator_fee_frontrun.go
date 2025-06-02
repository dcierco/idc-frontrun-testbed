package main

import (
	"fmt"
	"log"
	"strings"
	"time"
)

// --- Case-Specific Configuration ---
const (
	// Fee and Gas for attacker's transaction in this specific case
	case2_attackerHighFee  = "250000uatom"                     // Higher fee for attacker's transaction
	case2_attackerGasFlags = "--gas=auto --gas-adjustment=1.3" // Slightly higher gas adjustment for attacker

	// IBC transfer details
	case2_ibcTransferAmount = "100" // Amount of ibcTokenDenom (global) to transfer
	case2_attackerTxAmount  = "1"   // Amount of ibcTokenDenom (global) for attacker's transaction (denom added in code)

	// Note: Channel IDs are now auto-discovered and loaded from environment variables
	// set by the Makefile after 'rly tx link' operations complete.
	// The global variables transferChannelA and transferChannelB from config.go are used.
)

// --- End Case-Specific Configuration ---

func main() {
	log.Println("Starting Case 2: Validator Front-Running (Fee Manipulation)")

	if err := setupCase2(); err != nil {
		log.Fatalf("Setup failed: %v", err)
	}

	// 1. Initiate Victim Transfer
	victimTransferAmountStr := case2_ibcTransferAmount + ibcTokenDenom // Use global ibcTokenDenom
	log.Printf("Step 1: userA (%s) on %s sending %s to userB (%s) on %s via channel %s (fee: %s, gas: %s)",
		userA_KeyName, chainA_ID, victimTransferAmountStr, userB_KeyName, chainB_ID, transferChannelA, defaultFee, defaultGasFlags)

	transferTxHashA, err := ibcTransfer(chainA_ID, chainA_RPC, chainA_Home, userA_KeyName, userBAddrOnB,
		defaultSrcPort, transferChannelA, victimTransferAmountStr, defaultFee, defaultGasFlags)
	if err != nil {
		log.Fatalf("Failed to initiate IBC transfer: %v", err)
	}
	log.Printf("Victim's IBC transfer submitted on %s. Tx hash: %s", chainA_ID, transferTxHashA)
	log.Println("Waiting for transaction to be indexed on Chain A...")
	time.Sleep(6 * time.Second)

	packetSequence, err := findPacketSequenceFromTx(chainA_ID, chainA_RPC, transferTxHashA, defaultSrcPort, transferChannelA)
	if err != nil {
		log.Fatalf("Failed to find packet sequence from tx %s: %v", transferTxHashA, err)
	}
	log.Printf("Observed SendPacket sequence: %s for channel %s", packetSequence, transferChannelA)

	// 2. Relay to Mempool (by submitting relay command) & 3. Simultaneous Attacker Submission
	log.Println("Step 2 & 3: Relaying victim's packet and simultaneously submitting attacker's high-fee transaction to Chain B")

	relayDone := make(chan error, 1)
	go func() {
		log.Printf("Submitting relay command for packet sequence %s (path: %s, src chan: %s)", packetSequence, ibcPathTransfer, transferChannelA)
		// This command will try to submit MsgUpdateClient, MsgRecvPacket, etc.
		// The relayer itself will use its configured fees, not defaultFee from our script for this command.
		errRelay := relaySpecificPacket(rlyConfigPath, ibcPathTransfer, transferChannelA, packetSequence)
		if errRelay != nil {
			log.Printf("Warning: relaySpecificPacket command returned an error: %v. This might be okay if packet is already relayed or in flight.", errRelay)
		}
		relayDone <- errRelay // Send error or nil
	}()

	// Attacker's transaction
	attackerSendAmountStr := case2_attackerTxAmount + ibcTokenDenom // Use global ibcTokenDenom
	log.Printf("AttackerB (%s) submitting transaction (%s) with high fee (%s) and gas flags (%s) on %s",
		attackerB_KeyName, attackerSendAmountStr, case2_attackerHighFee, case2_attackerGasFlags, chainB_ID)
	attackerTxHashB, err := bankSend(chainB_ID, chainB_RPC, chainB_Home, attackerB_KeyName,
		attackerReceiverAddrOnB, attackerSendAmountStr, case2_attackerHighFee, case2_attackerGasFlags)
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
	case <-time.After(25 * time.Second): // Timeout for relay command
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
	recvPacketTxInfoB, err := findRecvPacketTx(chainB_ID, chainB_RPC, defaultDstPort, transferChannelB, packetSequence)
	if err != nil {
		log.Fatalf("Failed to find RecvPacket transaction for sequence %s on %s (port %s, channel %s): %v. The packet might not have been processed or the relay failed.",
			packetSequence, chainB_ID, defaultDstPort, transferChannelB, err)
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

	attackerTxHashUpper := strings.ToUpper(attackerTxHashB)
	recvPacketTxHashUpper := strings.ToUpper(recvPacketTxInfoB.TxHash)

	for i, b64Tx := range blockInfo.Block.Data.Txs {
		txHashInBlock, errHash := getTxHashFromBytes(b64Tx)
		if errHash != nil {
			log.Printf("Warning: Failed to get hash for tx from block data at index %d: %v", i, errHash)
			continue
		}

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

// setupCase2 populates the global address variables needed for this test case.
func setupCase2() error {
	var err error

	// Get userA's address on Chain A
	userAAddrOnA, err = getKeyAddress(chainA_ID, chainA_RPC, userA_KeyName, chainA_Home, keyringBackend)
	if err != nil {
		return fmt.Errorf("getting userA address on chain A: %w", err)
	}

	// Get userB's address on Chain B (victim's recipient)
	userBAddrOnB, err = getKeyAddress(chainB_ID, chainB_RPC, userB_KeyName, chainB_Home, keyringBackend)
	if err != nil {
		return fmt.Errorf("getting userB address on chain B: %w", err)
	}

	// Get attackerB's address on Chain B
	attackerBAddrOnB, err = getKeyAddress(chainB_ID, chainB_RPC, attackerB_KeyName, chainB_Home, keyringBackend)
	if err != nil {
		return fmt.Errorf("getting attackerB address on chain B: %w", err)
	}
	// For this case, attacker sends to themselves or a controlled address. Let's use their own.
	attackerReceiverAddrOnB = attackerBAddrOnB

	log.Println("--- Case 2 Setup ---")
	log.Printf("User A Address (Chain A): %s", userAAddrOnA)
	log.Printf("User B Address (Chain B - Victim's Recipient): %s", userBAddrOnB)
	log.Printf("Attacker B Address (Chain B): %s", attackerBAddrOnB)
	log.Printf("Attacker B Receiver Address (Chain B): %s", attackerReceiverAddrOnB)
	log.Printf("Using IBC Path from ENV (RLY_PATH_TRANSFER_ENV): %s", ibcPathTransfer)
	log.Printf("Using Auto-discovered Channel ID on Chain A (for send_packet): %s", transferChannelA)
	log.Printf("Using Auto-discovered Channel ID on Chain B (for recv_packet): %s", transferChannelB)
	log.Printf("Attacker's High Fee: %s, Attacker's Gas Flags: %s", case2_attackerHighFee, case2_attackerGasFlags)
	log.Println("--------------------")

	if ibcPathTransfer == "" {
		return fmt.Errorf("ibcPathTransfer (from RLY_PATH_TRANSFER_ENV) is not set in config")
	}
	if transferChannelA == "" || transferChannelB == "" {
		return fmt.Errorf("auto-discovered channel IDs (transferChannelA, transferChannelB) are not set. Ensure 'make setup-relayer' was run successfully")
	}
	return nil
}
