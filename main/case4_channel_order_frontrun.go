package main

import (
	"fmt"
	"log"
	"strconv"
	"time"
)

// --- Case-Specific Configuration ---
const (
	// Amounts for this case
	// ibcTokenDenom is from config.go
	case4_ibcTransferAmount = "10"
	case4_attackerTxAmount  = "1"

	// Note: Channel IDs are now auto-discovered and loaded from environment variables
	// set by the Makefile after 'rly tx link' operations complete.
	// For ordered channels: orderedChannelA and orderedChannelB from config.go are used.
	// For unordered channels: unorderedChannelA and unorderedChannelB from config.go are used.
)

// --- End Case-Specific Configuration ---

// Note: Global variables like chainA_Home, chainB_Home, rlyConfigPath,
// userBAddrOnB, attackerBAddrOnB, etc. are defined in config.go
// and populated either by config.go's init() or by setupCase4() below.
// ibcPathOrdered and ibcPathUnordered are also from config.go (loaded from ENV).

func main() {
	log.Println("Starting Case 4: Ordered vs. Unordered Channel Front-Running")

	if err := setupCase4(); err != nil {
		log.Fatalf("Setup failed: %v", err)
	}

	// --- Run A: Test with ORDERED Channel ---
	log.Println("\n\n--- RUN A: Testing with ORDERED Channel ---")
	log.Printf("Path: %s, Chain A src channel: %s, Chain B dst channel (for recv_packet): %s",
		ibcPathOrdered, orderedChannelA, orderedChannelB)
	runFrontRunTest("ORDERED", ibcPathOrdered, orderedChannelA, orderedChannelB)

	log.Println("\nWaiting a bit before starting the unordered channel test...")
	time.Sleep(15 * time.Second) // Give some time for things to settle, clear mempools etc.

	// --- Run B: Test with UNORDERED Channel ---
	log.Println("\n\n--- RUN B: Testing with UNORDERED Channel ---")
	log.Printf("Path: %s, Chain A src channel: %s, Chain B dst channel (for recv_packet): %s",
		ibcPathUnordered, unorderedChannelA, unorderedChannelB)
	runFrontRunTest("UNORDERED", ibcPathUnordered, unorderedChannelA, unorderedChannelB)

	log.Println("\n\nCase 4 finished.")
}

func runFrontRunTest(channelType, currentIbcPath, srcChannelA, dstChannelB_for_recv_query string) {
	log.Printf("[%s] Test: Simulating front-running scenario using path %s.", channelType, currentIbcPath)

	// Amounts for this test run
	transferAmountStr := case4_ibcTransferAmount + ibcTokenDenom
	attackerSendAmountStr := case4_attackerTxAmount + ibcTokenDenom

	// 1. Initiate Multiple Victim Transfers (Packet1, Packet2)
	log.Printf("[%s] Step 1: userA (%s) sending two IBC transfers (%s each) via %s channel %s (port %s)",
		channelType, userA_KeyName, transferAmountStr, channelType, srcChannelA, defaultSrcPort)

	// Packet 1
	transfer1TxHashA, err := ibcTransfer(chainA_ID, chainA_RPC, chainA_Home, userA_KeyName, userBAddrOnB,
		defaultSrcPort, srcChannelA, transferAmountStr, defaultFee, defaultGasFlags)
	if err != nil {
		log.Printf("[%s] ERROR: Failed to send Packet1: %v", channelType, err)
		return
	}
	log.Printf("[%s] Packet1 sent. Tx hash on %s: %s", channelType, chainA_ID, transfer1TxHashA)
	time.Sleep(5 * time.Second) // Delay between sends to avoid sequence mismatch

	// Packet 2
	transfer2TxHashA, err := ibcTransfer(chainA_ID, chainA_RPC, chainA_Home, userA_KeyName, userBAddrOnB,
		defaultSrcPort, srcChannelA, transferAmountStr, defaultFee, defaultGasFlags)
	if err != nil {
		log.Printf("[%s] ERROR: Failed to send Packet2: %v", channelType, err)
		return
	}
	log.Printf("[%s] Packet2 sent. Tx hash on %s: %s", channelType, chainA_ID, transfer2TxHashA)
	log.Printf("[%s] Waiting for packets to be indexed on Chain A...", channelType)
	time.Sleep(6 * time.Second)

	// 2. Observe Packets
	log.Printf("[%s] Step 2: Observing packet sequence numbers on %s for port %s, channel %s", channelType, chainA_ID, defaultSrcPort, srcChannelA)
	packet1Seq, err := findPacketSequenceFromTx(chainA_ID, chainA_RPC, transfer1TxHashA, defaultSrcPort, srcChannelA)
	if err != nil {
		log.Printf("[%s] ERROR: Failed to find Packet1 sequence: %v", channelType, err)
		return
	}
	log.Printf("[%s] Observed Packet1 sequence: %s", channelType, packet1Seq)

	packet2Seq, err := findPacketSequenceFromTx(chainA_ID, chainA_RPC, transfer2TxHashA, defaultSrcPort, srcChannelA)
	if err != nil {
		log.Printf("[%s] ERROR: Failed to find Packet2 sequence: %v", channelType, err)
		return
	}
	log.Printf("[%s] Observed Packet2 sequence: %s", channelType, packet2Seq)

	// 3. Attacker Action (Targeting Packet2) - before Packet2 is relayed/processed
	// We will first relay Packet1, then attacker acts, then relay Packet2.
	log.Printf("[%s] Step 3: Controlled Relaying of Packet1 (seq %s) via path %s, src chan %s", channelType, packet1Seq, currentIbcPath, srcChannelA)
	err = relaySpecificPacket(rlyConfigPath, currentIbcPath, srcChannelA, packet1Seq)
	if err != nil {
		log.Printf("[%s] WARNING: Relaying Packet1 (seq %s) command failed: %v. It might have been relayed by another process or an issue occurred.", channelType, packet1Seq, err)
		// Continue, as the goal is to see if attacker can front-run Packet2
	} else {
		log.Printf("[%s] Packet1 (seq %s) relay command executed.", channelType, packet1Seq)
	}
	log.Printf("[%s] Waiting a moment for Packet1 to potentially process on Chain B...", channelType)
	time.Sleep(8 * time.Second) // Give Packet1 time to land

	// Attacker's action on Chain B
	log.Printf("[%s] Step 3b: AttackerB (%s) performing transaction (%s) on %s BEFORE Packet2 (seq %s) is relayed",
		channelType, attackerB_KeyName, attackerSendAmountStr, chainB_ID, packet2Seq)
	attackerTxHashB, err := bankSend(chainB_ID, chainB_RPC, chainB_Home, attackerB_KeyName,
		attackerReceiverAddrOnB, attackerSendAmountStr, defaultFee, defaultGasFlags)
	if err != nil {
		log.Printf("[%s] ERROR: Attacker's transaction failed: %v", channelType, err)
		return
	}
	log.Printf("[%s] Attacker's transaction submitted. Tx hash on %s: %s", channelType, chainB_ID, attackerTxHashB)
	log.Printf("[%s] Waiting for attacker's transaction to confirm...", channelType)
	time.Sleep(6 * time.Second)

	// 4. Controlled Relaying of Packet2
	log.Printf("[%s] Step 4: Controlled Relaying of Packet2 (seq %s) via path %s, src chan %s, AFTER attacker's action", channelType, packet2Seq, currentIbcPath, srcChannelA)
	err = relaySpecificPacket(rlyConfigPath, currentIbcPath, srcChannelA, packet2Seq)
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
	recvPacket1TxInfoB, errP1 := findRecvPacketTx(chainB_ID, chainB_RPC, defaultDstPort, dstChannelB_for_recv_query, packet1Seq)
	if errP1 != nil {
		log.Printf("[%s] WARNING: Could not find RecvPacket for Packet1 (seq %s) on %s (port %s, channel %s): %v",
			channelType, packet1Seq, chainB_ID, defaultDstPort, dstChannelB_for_recv_query, errP1)
	} else {
		log.Printf("[%s] Packet1 (seq %s) processed in tx %s on %s, block: %s",
			channelType, packet1Seq, recvPacket1TxInfoB.TxHash, chainB_ID, recvPacket1TxInfoB.Height)
	}

	// Verify Packet2 processing
	recvPacket2TxInfoB, errP2 := findRecvPacketTx(chainB_ID, chainB_RPC, defaultDstPort, dstChannelB_for_recv_query, packet2Seq)
	if errP2 != nil {
		log.Printf("[%s] WARNING: Could not find RecvPacket for Packet2 (seq %s) on %s (port %s, channel %s): %v",
			channelType, packet2Seq, chainB_ID, defaultDstPort, dstChannelB_for_recv_query, errP2)
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

// setupCase4 populates the global address variables needed for this test case.
func setupCase4() error {
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
	attackerReceiverAddrOnB = attackerBAddrOnB // Attacker sends to self

	log.Println("--- Case 4 Setup ---")
	log.Printf("User A Address (Chain A): %s", userAAddrOnA)
	log.Printf("User B Address (Chain B - Recipient): %s", userBAddrOnB)
	log.Printf("Attacker B Address (Chain B): %s", attackerBAddrOnB)
	log.Printf("Attacker B Receiver Address (Chain B): %s", attackerReceiverAddrOnB)

	log.Printf("Using IBC Path (Ordered) from ENV (RLY_PATH_ORDERED_ENV): %s", ibcPathOrdered)
	log.Printf("  Src Channel (Chain A): %s, Dst Channel (Chain B for recv_packet): %s", orderedChannelA, orderedChannelB)
	log.Printf("Using IBC Path (Unordered) from ENV (RLY_PATH_UNORDERED_ENV): %s", ibcPathUnordered)
	log.Printf("  Src Channel (Chain A): %s, Dst Channel (Chain B for recv_packet): %s", unorderedChannelA, unorderedChannelB)
	log.Println("--------------------")

	if ibcPathOrdered == "" || ibcPathUnordered == "" {
		return fmt.Errorf("one or both relayer paths (ibcPathOrdered, ibcPathUnordered) are not set from ENV variables")
	}
	if orderedChannelA == "" || orderedChannelB == "" ||
		unorderedChannelA == "" || unorderedChannelB == "" {
		return fmt.Errorf("auto-discovered channel IDs for ordered/unordered tests are not set. Ensure 'make setup-relayer' was run successfully")
	}
	if ibcPathOrdered == ibcPathUnordered && orderedChannelA == unorderedChannelA {
		log.Println("WARNING: Ordered and Unordered tests are configured to use the same path name AND same source channel ID. This may not be a meaningful distinction unless the path itself has different 'order' settings in the relayer config and different channel IDs were generated on link.")
	}
	return nil
}
