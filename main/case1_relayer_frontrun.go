package main

import (
	"fmt"
	"log"
	"strconv"
	"time"
)

// --- Case-Specific Configuration Constants ---
// These values might be specific to this test case or require manual updates
// after chain/relayer setup.
const (
	// IBC transfer details for this case
	case1_ibcTransferAmount = "100" // Amount of ibcTokenDenom (defined in config.go) to transfer
	case1_attackerTxAmount  = "1"   // Amount of ibcTokenDenom for attacker's transaction (denom added in code)

	// Note: Channel IDs are now auto-discovered and loaded from environment variables
	// set by the Makefile after 'rly tx link' operations complete.
	// The global variables transferChannelA and transferChannelB from config.go are used.
)

// --- End Case-Specific Configuration Constants ---

func main() {
	log.Println("Starting Case 1: Relayer Front-Running Scenario")

	// Populate global address variables
	if err := setupCase1(); err != nil {
		log.Fatalf("Setup failed: %v", err)
	}

	// 1. Initiate Victim Transfer: userA on Chain A sends an IBC transfer
	victimTransferAmountStr := case1_ibcTransferAmount + ibcTokenDenom // Use global ibcTokenDenom
	log.Printf("Step 1: userA (%s) on %s sending %s to userB (%s) on %s via channel %s",
		userA_KeyName, chainA_ID, victimTransferAmountStr, userB_KeyName, chainB_ID, transferChannelA)

	transferTxHash, err := ibcTransfer(chainA_ID, chainA_RPC, chainA_Home, userA_KeyName, userBAddrOnB,
		defaultSrcPort, transferChannelA, victimTransferAmountStr, defaultFee, defaultGasFlags)
	if err != nil {
		log.Fatalf("Failed to initiate IBC transfer: %v", err)
	}
	log.Printf("Victim's IBC transfer submitted. Tx hash on %s: %s", chainA_ID, transferTxHash)
	log.Println("Waiting a few seconds for the transaction to be indexed...")
	time.Sleep(6 * time.Second) // Give time for indexing

	// 2. Observe Packet: Query Chain A to find the sequence number
	log.Println("Step 2: Observing packet sequence number on", chainA_ID)
	packetSequence, err := findPacketSequenceFromTx(chainA_ID, chainA_RPC, transferTxHash, defaultSrcPort, transferChannelA)
	if err != nil {
		log.Fatalf("Failed to find packet sequence: %v", err)
	}
	log.Printf("Observed SendPacket sequence: %s for channel %s", packetSequence, transferChannelA)

	// 3. Attacker Action on Chain B: attackerB performs a transaction
	attackerSendAmountStr := case1_attackerTxAmount + ibcTokenDenom // Use global ibcTokenDenom
	log.Printf("Step 3: attackerB (%s) on %s performing a transaction (%s) BEFORE victim's packet is relayed",
		attackerB_KeyName, chainB_ID, attackerSendAmountStr)
	attackerTxHashOnB, err := bankSend(chainB_ID, chainB_RPC, chainB_Home, attackerB_KeyName,
		attackerReceiverAddrOnB, attackerSendAmountStr, defaultFee, defaultGasFlags)
	if err != nil {
		log.Fatalf("Attacker's transaction failed: %v", err)
	}
	log.Printf("Attacker's transaction submitted. Tx hash on %s: %s", chainB_ID, attackerTxHashOnB)
	log.Println("Waiting a few seconds for attacker's transaction to be confirmed...")
	time.Sleep(6 * time.Second)

	// 4. Controlled Relaying: Manually trigger relayer for the specific packet
	// Using ibcPathTransfer from config.go (populated by RLY_PATH_TRANSFER_ENV)
	log.Printf("Step 4: Manually relaying specific packet (seq: %s) using path %s, src channel %s",
		packetSequence, ibcPathTransfer, transferChannelA)
	err = relaySpecificPacket(rlyConfigPath, ibcPathTransfer, transferChannelA, packetSequence)
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
	// Using defaultDstPort and transferChannelB
	recvPacketTxInfo, err := findRecvPacketTx(chainB_ID, chainB_RPC, defaultDstPort, transferChannelB, packetSequence)
	if err != nil {
		log.Printf("Warning: Could not find RecvPacket tx for sequence %s on %s (port %s, channel %s): %v. The packet may not have been processed.",
			packetSequence, chainB_ID, defaultDstPort, transferChannelB, err)
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

// setupCase1 populates the global address variables needed for this test case.
// It uses global configuration variables like chainA_ID, chainA_RPC, userA_KeyName, etc.
func setupCase1() error {
	var err error
	// Get userA's address on Chain A (though not strictly needed for sending if using key name, good for logging)
	userAAddrOnA, err = getKeyAddress(chainA_ID, chainA_RPC, userA_KeyName, chainA_Home, keyringBackend)
	if err != nil {
		return fmt.Errorf("getting userA address on chain A: %w", err)
	}

	// Get userB's address on Chain B (recipient)
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

	log.Println("--- Case 1 Setup ---")
	log.Printf("User A Address (Chain A): %s", userAAddrOnA)
	log.Printf("User B Address (Chain B - Victim's Recipient): %s", userBAddrOnB)
	log.Printf("Attacker B Address (Chain B): %s", attackerBAddrOnB)
	log.Printf("Attacker B Receiver Address (Chain B): %s", attackerReceiverAddrOnB)
	log.Printf("Using IBC Path from ENV (RLY_PATH_TRANSFER_ENV): %s", ibcPathTransfer)
	log.Printf("Using Auto-discovered Channel ID on Chain A (for send_packet): %s", transferChannelA)
	log.Printf("Using Auto-discovered Channel ID on Chain B (for recv_packet): %s", transferChannelB)
	log.Println("--------------------")

	// Check if the path from ENV is empty, which would be an issue.
	if ibcPathTransfer == "" {
		return fmt.Errorf("ibcPathTransfer (from RLY_PATH_TRANSFER_ENV) is not set. Ensure Makefile passes this env var")
	}
	if transferChannelA == "" || transferChannelB == "" {
		return fmt.Errorf("auto-discovered channel IDs (transferChannelA, transferChannelB) are not set. Ensure 'make setup-relayer' was run successfully")
	}

	return nil
}
