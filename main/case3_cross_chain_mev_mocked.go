package main

import (
	"fmt"
	"log"
	"strconv"
	"time"
)

// --- Case-Specific Configuration ---
const (
	// Amounts for mocked DeFi interaction
	// Denoms (ibcTokenDenom, attackerStakeDenom) are from config.go
	case3_attackerPreemptiveBuyAmountStake = "50"  // Amount of attackerStakeDenom
	case3_largeIBCTransferAmount           = "500" // Amount of ibcTokenDenom
	case3_attackerPostIBCTokenSellAmount   = "20"  // Amount of ibcTokenDenom

	// Note: Channel IDs are now auto-discovered and loaded from environment variables
	// set by the Makefile after 'rly tx link' operations complete.
	// The global variables transferChannelA and transferChannelB from config.go are used.
)

// --- End Case-Specific Configuration ---

// Note: Global variables like chainA_Home, chainB_Home, rlyConfigPath,
// userBAddrOnB, attackerBAddrOnB, mockDexAddrOnB, etc. are now defined in config.go
// and populated either by config.go's init() or by setupCase3() below.

func main() {
	log.Println("Starting Case 3: Cross-Chain MEV (Mocked DeFi Interaction)")

	if err := setupCase3(); err != nil {
		log.Fatalf("Setup failed: %v", err)
	}

	// Initial Balances (optional, for clarity)
	logInitialBalances()

	// 1. Mock DeFi State (Conceptual) & 2. Attacker's "Pre-emptive Buy"
	preemptiveBuyAmountStr := case3_attackerPreemptiveBuyAmountStake + attackerStakeDenom // Use global attackerStakeDenom
	log.Printf("Step 1 & 2: Attacker (%s) performs 'pre-emptive buy' on Chain B (sends %s to %s (%s))",
		attackerB_KeyName, preemptiveBuyAmountStr, mockDexB_KeyName, mockDexAddrOnB)

	preemptiveBuyTxHash, err := bankSend(chainB_ID, chainB_RPC, chainB_Home, attackerB_KeyName,
		mockDexAddrOnB, preemptiveBuyAmountStr, defaultFee, defaultGasFlags)
	if err != nil {
		log.Fatalf("Attacker's pre-emptive buy transaction failed: %v", err)
	}
	log.Printf("Attacker's 'pre-emptive buy' tx hash on %s: %s", chainB_ID, preemptiveBuyTxHash)
	log.Println("Waiting for attacker's pre-emptive buy to confirm...")
	time.Sleep(6 * time.Second)

	// 3. Initiate Victim Transfer
	victimLargeTransferAmountStr := case3_largeIBCTransferAmount + ibcTokenDenom // Use global ibcTokenDenom
	log.Printf("Step 3: userA (%s) on %s sending large IBC transfer (%s) to userB (%s) on %s via channel %s",
		userA_KeyName, chainA_ID, victimLargeTransferAmountStr, userB_KeyName, chainB_ID, transferChannelA)

	victimTransferTxHashA, err := ibcTransfer(chainA_ID, chainA_RPC, chainA_Home, userA_KeyName, userBAddrOnB,
		defaultSrcPort, transferChannelA, victimLargeTransferAmountStr, defaultFee, defaultGasFlags)
	if err != nil {
		log.Fatalf("Victim's large IBC transfer failed: %v", err)
	}
	log.Printf("Victim's large IBC transfer submitted on %s. Tx hash: %s", chainA_ID, victimTransferTxHashA)
	log.Println("Waiting for victim's transfer to be indexed on Chain A...")
	time.Sleep(6 * time.Second)

	// 4. Observe and Relay Packet
	log.Println("Step 4: Observing and relaying victim's IBC packet")
	packetSequence, err := findPacketSequenceFromTx(chainA_ID, chainA_RPC, victimTransferTxHashA, defaultSrcPort, transferChannelA)
	if err != nil {
		log.Fatalf("Failed to find packet sequence from victim's transfer tx %s: %v", victimTransferTxHashA, err)
	}
	log.Printf("Observed SendPacket sequence: %s for port %s, channel %s", packetSequence, defaultSrcPort, transferChannelA)

	err = relaySpecificPacket(rlyConfigPath, ibcPathTransfer, transferChannelA, packetSequence)
	if err != nil {
		log.Printf("Warning: Relaying specific packet command failed: %v. Packet might have been relayed by another process.", err)
	} else {
		log.Println("Specific packet relay command executed for victim's transfer.")
	}
	log.Println("Waiting for victim's IBC transfer to be processed on Chain B...")
	time.Sleep(10 * time.Second)

	// Verify victim's packet was received on Chain B
	recvPacketTxInfoB, err := findRecvPacketTx(chainB_ID, chainB_RPC, defaultDstPort, transferChannelB, packetSequence)
	if err != nil {
		log.Fatalf("Victim's IBC packet (seq %s) not found on Chain B (port %s, channel %s): %v. Aborting.", packetSequence, defaultDstPort, transferChannelB, err)
	}
	log.Printf("Victim's IBC packet (seq %s) processed in tx %s on %s (block %s)",
		packetSequence, recvPacketTxInfoB.TxHash, chainB_ID, recvPacketTxInfoB.Height)

	// 5. Attacker's "Post-IBC Sell"
	postIBCTokenSellAmountStr := case3_attackerPostIBCTokenSellAmount + ibcTokenDenom // Use global ibcTokenDenom
	log.Printf("Step 5: Attacker (%s) performs 'post-IBC sell' on Chain B (sends %s to %s (%s))",
		attackerB_KeyName, postIBCTokenSellAmountStr, mockDexB_KeyName, mockDexAddrOnB)
	// This assumes attackerB has 'ibcTokenDenom' (e.g. 'token') to "sell".
	// Ensure attackerB is funded with 'token' prior to running the script if this is to succeed.
	postIBCTxHash, err := bankSend(chainB_ID, chainB_RPC, chainB_Home, attackerB_KeyName,
		mockDexAddrOnB, postIBCTokenSellAmountStr, defaultFee, defaultGasFlags)
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
	logBalance(chainB_ID, chainB_RPC, attackerBAddrOnB, attackerB_KeyName, attackerStakeDenom) // Global attackerStakeDenom
	logBalance(chainB_ID, chainB_RPC, attackerBAddrOnB, attackerB_KeyName, ibcTokenDenom)      // Global ibcTokenDenom (was attackerTokenDenom)
	logBalance(chainB_ID, chainB_RPC, mockDexAddrOnB, mockDexB_KeyName, attackerStakeDenom)    // Global attackerStakeDenom
	logBalance(chainB_ID, chainB_RPC, mockDexAddrOnB, mockDexB_KeyName, ibcTokenDenom)         // Global ibcTokenDenom

	log.Println("Case 3 finished.")
}

// setupCase3 populates the global address variables needed for this test case.
func setupCase3() error {
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
	attackerReceiverAddrOnB = attackerBAddrOnB // Attacker sends to self in simpler scenarios

	// Get mockDexB's address on Chain B
	mockDexAddrOnB, err = getKeyAddress(chainB_ID, chainB_RPC, mockDexB_KeyName, chainB_Home, keyringBackend)
	if err != nil {
		return fmt.Errorf("getting mockDexB address on chain B: %w", err)
	}

	log.Println("--- Case 3 Setup ---")
	log.Printf("User A Address (Chain A): %s", userAAddrOnA)
	log.Printf("User B Address (Chain B - Victim's Recipient): %s", userBAddrOnB)
	log.Printf("Attacker B Address (Chain B): %s", attackerBAddrOnB)
	log.Printf("Mock DEX B Address (Chain B): %s", mockDexAddrOnB)
	log.Printf("Using IBC Path from ENV (RLY_PATH_TRANSFER_ENV): %s", ibcPathTransfer)
	log.Printf("Using Auto-discovered Channel ID on Chain A (for send_packet): %s", transferChannelA)
	log.Printf("Using Auto-discovered Channel ID on Chain B (for recv_packet): %s", transferChannelB)
	log.Printf("Attacker Stake Denom (global): %s, IBC Token Denom (global): %s", attackerStakeDenom, ibcTokenDenom)
	log.Println("--------------------")

	if ibcPathTransfer == "" {
		return fmt.Errorf("ibcPathTransfer (from RLY_PATH_TRANSFER_ENV) is not set in config")
	}
	if transferChannelA == "" || transferChannelB == "" {
		return fmt.Errorf("auto-discovered channel IDs (transferChannelA, transferChannelB) are not set. Ensure 'make setup-relayer' was run successfully")
	}
	return nil
}

func logInitialBalances() {
	log.Println("--- Initial Balances on Chain B (for reference) ---")
	logBalance(chainB_ID, chainB_RPC, userBAddrOnB, userB_KeyName, ibcTokenDenom)
	logBalance(chainB_ID, chainB_RPC, attackerBAddrOnB, attackerB_KeyName, attackerStakeDenom)
	logBalance(chainB_ID, chainB_RPC, attackerBAddrOnB, attackerB_KeyName, ibcTokenDenom)
	logBalance(chainB_ID, chainB_RPC, mockDexAddrOnB, mockDexB_KeyName, attackerStakeDenom)
	logBalance(chainB_ID, chainB_RPC, mockDexAddrOnB, mockDexB_KeyName, ibcTokenDenom)
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
