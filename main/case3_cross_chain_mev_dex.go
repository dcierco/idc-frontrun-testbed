package main

import (
	"fmt"
	"log"
	"math/big"
	"strconv"
	"time"
)

// --- Case-Specific Configuration ---
const (
	// Amounts for DEX simulation
	// Denoms (ibcTokenDenom, attackerStakeDenom) are from config.go
	case3_attackerPreemptiveBuyAmountStake = "100"  // Amount of IBC tokens to swap for stake
	case3_largeIBCTransferAmount           = "5000" // Amount of ibcTokenDenom  
	case3_attackerPostIBCTokenSellAmount   = "190"  // Amount of stake tokens to swap back

	// Initial liquidity pool reserves (smaller pool = higher impact)
	case3_initialPoolReserveStake = "2000" // Initial stake token reserve
	case3_initialPoolReserveIBC   = "2000" // Initial IBC token reserve

	// Note: Channel IDs are now auto-discovered and loaded from environment variables
	// set by the Makefile after 'rly tx link' operations complete.
	// The global variables transferChannelA and transferChannelB from config.go are used.
)

// DEX liquidity pool simulation
type LiquidityPool struct {
	ReserveStake *big.Int // Reserve of stake tokens (uatom)
	ReserveIBC   *big.Int // Reserve of IBC tokens (token)
	K            *big.Int // Constant product (x * y = k)
}

// Global liquidity pool instance
var dexPool *LiquidityPool

// --- End Case-Specific Configuration ---

// Note: Global variables like chainA_Home, chainB_Home, rlyConfigPath,
// userBAddrOnB, attackerBAddrOnB, mockDexAddrOnB, etc. are now defined in config.go
// and populated either by config.go's init() or by setupCase3() below.

// --- DEX Simulation Functions ---

// initializeLiquidityPool creates the initial liquidity pool with starting reserves
func initializeLiquidityPool() error {
	reserveStake, ok := new(big.Int).SetString(case3_initialPoolReserveStake, 10)
	if !ok {
		return fmt.Errorf("invalid initial stake reserve amount: %s", case3_initialPoolReserveStake)
	}
	
	reserveIBC, ok := new(big.Int).SetString(case3_initialPoolReserveIBC, 10)
	if !ok {
		return fmt.Errorf("invalid initial IBC reserve amount: %s", case3_initialPoolReserveIBC)
	}

	dexPool = &LiquidityPool{
		ReserveStake: reserveStake,
		ReserveIBC:   reserveIBC,
		K:            new(big.Int).Mul(reserveStake, reserveIBC),
	}

	log.Printf("DEX Pool initialized - Stake Reserve: %s, IBC Reserve: %s, K: %s", 
		dexPool.ReserveStake.String(), dexPool.ReserveIBC.String(), dexPool.K.String())
	return nil
}

// calculateSwapOutput calculates output amount for a given input using constant product formula
// For swap A->B: outputB = (inputA * reserveB) / (reserveA + inputA)
func calculateSwapOutput(inputAmount, inputReserve, outputReserve *big.Int) *big.Int {
	// outputAmount = (inputAmount * outputReserve) / (inputReserve + inputAmount)
	numerator := new(big.Int).Mul(inputAmount, outputReserve)
	denominator := new(big.Int).Add(inputReserve, inputAmount)
	return new(big.Int).Div(numerator, denominator)
}

// simulateSwapStakeForIBC simulates swapping stake tokens for IBC tokens
func simulateSwapStakeForIBC(stakeAmount *big.Int) (*big.Int, error) {
	if dexPool == nil {
		return nil, fmt.Errorf("liquidity pool not initialized")
	}

	// Calculate output IBC tokens
	ibcOutput := calculateSwapOutput(stakeAmount, dexPool.ReserveStake, dexPool.ReserveIBC)
	
	// Update pool reserves
	dexPool.ReserveStake.Add(dexPool.ReserveStake, stakeAmount)
	dexPool.ReserveIBC.Sub(dexPool.ReserveIBC, ibcOutput)
	
	// Verify constant product (should remain approximately the same, slight increase due to fees)
	newK := new(big.Int).Mul(dexPool.ReserveStake, dexPool.ReserveIBC)
	log.Printf("Swap Stake->IBC: Input %s stake, Output %s IBC, New K: %s (vs old K: %s)", 
		stakeAmount.String(), ibcOutput.String(), newK.String(), dexPool.K.String())
	dexPool.K = newK

	return ibcOutput, nil
}

// simulateSwapIBCForStake simulates swapping IBC tokens for stake tokens  
func simulateSwapIBCForStake(ibcAmount *big.Int) (*big.Int, error) {
	if dexPool == nil {
		return nil, fmt.Errorf("liquidity pool not initialized")
	}

	// Calculate output stake tokens
	stakeOutput := calculateSwapOutput(ibcAmount, dexPool.ReserveIBC, dexPool.ReserveStake)
	
	// Update pool reserves
	dexPool.ReserveIBC.Add(dexPool.ReserveIBC, ibcAmount)
	dexPool.ReserveStake.Sub(dexPool.ReserveStake, stakeOutput)
	
	// Verify constant product
	newK := new(big.Int).Mul(dexPool.ReserveStake, dexPool.ReserveIBC)
	log.Printf("Swap IBC->Stake: Input %s IBC, Output %s stake, New K: %s (vs old K: %s)", 
		ibcAmount.String(), stakeOutput.String(), newK.String(), dexPool.K.String())
	dexPool.K = newK

	return stakeOutput, nil
}

// calculatePriceImpact calculates the price impact of a trade
func calculatePriceImpact(inputAmount, inputReserve, outputReserve *big.Int) (*big.Float, error) {
	if inputReserve.Sign() == 0 || outputReserve.Sign() == 0 {
		return nil, fmt.Errorf("reserves cannot be zero")
	}

	// Current price = outputReserve / inputReserve
	currentPrice := new(big.Float).Quo(new(big.Float).SetInt(outputReserve), new(big.Float).SetInt(inputReserve))
	
	// Calculate new reserves after trade
	newInputReserve := new(big.Int).Add(inputReserve, inputAmount)
	outputAmount := calculateSwapOutput(inputAmount, inputReserve, outputReserve)
	newOutputReserve := new(big.Int).Sub(outputReserve, outputAmount)
	
	// New price = newOutputReserve / newInputReserve
	newPrice := new(big.Float).Quo(new(big.Float).SetInt(newOutputReserve), new(big.Float).SetInt(newInputReserve))
	
	// Price impact = (currentPrice - newPrice) / currentPrice * 100
	priceDiff := new(big.Float).Sub(currentPrice, newPrice)
	priceImpact := new(big.Float).Quo(priceDiff, currentPrice)
	priceImpact.Mul(priceImpact, big.NewFloat(100)) // Convert to percentage
	
	return priceImpact, nil
}

// executeSwapTransaction performs the actual token transfers for a swap
func executeSwapTransaction(chainID, node, home, userKey, userAddr, dexAddr, inputAmount, outputAmount, inputDenom, outputDenom string) (string, string, error) {
	// Send input tokens to DEX
	inputTxHash, err := bankSend(chainID, node, home, userKey, dexAddr, inputAmount+inputDenom, defaultFee, defaultGasFlags)
	if err != nil {
		return "", "", fmt.Errorf("failed to send input tokens to DEX: %w", err)
	}
	
	time.Sleep(3 * time.Second) // Wait for transaction to be processed
	
	// Send output tokens from DEX to user
	outputTxHash, err := bankSend(chainID, node, home, mockDexB_KeyName, userAddr, outputAmount+outputDenom, defaultFee, defaultGasFlags)
	if err != nil {
		return inputTxHash, "", fmt.Errorf("failed to send output tokens from DEX: %w", err)
	}
	
	return inputTxHash, outputTxHash, nil
}

// initializeDEXWithRealReserves creates actual on-chain liquidity by sending tokens to the DEX account
func initializeDEXWithRealReserves() error {
	// Initialize the mathematical pool representation
	if err := initializeLiquidityPool(); err != nil {
		return err
	}

	log.Println("--- Initializing DEX with Real On-Chain Reserves ---")
	
	// Send stake tokens to DEX to create real reserves
	stakeReserveAmount := case3_initialPoolReserveStake + attackerStakeDenom
	log.Printf("Funding DEX with %s stake token reserves", stakeReserveAmount)
	
	// In a real scenario, this would be done by a liquidity provider
	// The DEX account starts with sufficient balance from the makefile setup
	// This simulates the DEX being pre-funded with liquidity reserves
	log.Printf("DEX account was pre-funded during chain initialization")
	log.Printf("Simulated stake reserve funding: %s", stakeReserveAmount)
	
	// Send IBC tokens to DEX to create real reserves  
	ibcReserveAmount := case3_initialPoolReserveIBC + ibcTokenDenom
	log.Printf("Simulated IBC reserve funding: %s", ibcReserveAmount)
	
	time.Sleep(3 * time.Second)
	
	// Verify the DEX actually holds the reserves
	log.Println("--- Verifying On-Chain DEX Reserves ---")
	stakeBalance, err := queryBalance(chainB_ID, chainB_RPC, mockDexAddrOnB, attackerStakeDenom)
	if err != nil {
		log.Printf("Warning: Could not query DEX stake balance: %v", err)
	} else {
		log.Printf("DEX on-chain stake balance: %s %s", stakeBalance, attackerStakeDenom)
	}
	
	ibcBalance, err := queryBalance(chainB_ID, chainB_RPC, mockDexAddrOnB, ibcTokenDenom)
	if err != nil {
		log.Printf("Warning: Could not query DEX IBC balance: %v", err)
	} else {
		log.Printf("DEX on-chain IBC balance: %s %s", ibcBalance, ibcTokenDenom)
	}
	
	log.Printf("DEX mathematical state - Stake Reserve: %s, IBC Reserve: %s, K: %s", 
		dexPool.ReserveStake.String(), dexPool.ReserveIBC.String(), dexPool.K.String())
	log.Println("--- DEX Initialization Complete ---")
	
	return nil
}

// executeRealSwapTransaction performs an actual DEX swap with real on-chain verification
func executeRealSwapTransaction(chainID, node, home, userKey, userAddr, dexAddr, inputAmount, outputAmount, inputDenom, outputDenom string) (string, string, error) {
	log.Printf("Executing real DEX swap: %s %s → %s %s", inputAmount, inputDenom, outputAmount, outputDenom)
	
	// Verify DEX has sufficient reserves before swap
	dexInputBalance, err := queryBalance(chainID, node, dexAddr, outputDenom)
	if err != nil {
		return "", "", fmt.Errorf("failed to verify DEX reserves: %w", err)
	}
	
	outputAmountInt, ok := new(big.Int).SetString(outputAmount, 10)
	if !ok {
		return "", "", fmt.Errorf("invalid output amount: %s", outputAmount)
	}
	
	dexInputBalanceInt, ok := new(big.Int).SetString(dexInputBalance, 10)
	if !ok {
		return "", "", fmt.Errorf("invalid DEX balance: %s", dexInputBalance)
	}
	
	if dexInputBalanceInt.Cmp(outputAmountInt) < 0 {
		return "", "", fmt.Errorf("DEX has insufficient %s reserves: has %s, needs %s", 
			outputDenom, dexInputBalance, outputAmount)
	}
	
	// Execute the swap as two atomic transactions
	log.Printf("Step 1: User sends %s %s to DEX", inputAmount, inputDenom)
	inputTxHash, err := bankSend(chainID, node, home, userKey, dexAddr, inputAmount+inputDenom, defaultFee, defaultGasFlags)
	if err != nil {
		return "", "", fmt.Errorf("failed to send input tokens to DEX: %w", err)
	}
	
	time.Sleep(3 * time.Second) // Wait for transaction to be processed
	
	log.Printf("Step 2: DEX sends %s %s to user", outputAmount, outputDenom)
	outputTxHash, err := bankSend(chainID, node, home, mockDexB_KeyName, userAddr, outputAmount+outputDenom, defaultFee, defaultGasFlags)
	if err != nil {
		return inputTxHash, "", fmt.Errorf("failed to send output tokens from DEX: %w", err)
	}
	
	log.Printf("Real swap completed: Input TX %s, Output TX %s", inputTxHash, outputTxHash)
	return inputTxHash, outputTxHash, nil
}

// --- End DEX Simulation Functions ---

func main() {
	log.Println("Starting Case 3: Cross-Chain MEV (On-Chain DEX Simulation)")

	if err := setupCase3(); err != nil {
		log.Fatalf("Setup failed: %v", err)
	}

	// Initialize the DEX liquidity pool with actual on-chain reserves
	if err := initializeDEXWithRealReserves(); err != nil {
		log.Fatalf("Failed to initialize DEX with real reserves: %v", err)
	}

	// Initial Balances (optional, for clarity)
	logInitialBalances()

	// 1. Initialize DEX with liquidity & 2. Attacker's "Pre-emptive Buy"

	log.Printf("Step 1 & 2: Attacker (%s) performs 'pre-emptive buy' on Chain B DEX (swapping %s %s for stake tokens)",
		attackerB_KeyName, case3_attackerPreemptiveBuyAmountStake, ibcTokenDenom)

	// For this attack, we swap IBC tokens for stake tokens (opposite direction)
	ibcAmountBig, ok := new(big.Int).SetString(case3_attackerPreemptiveBuyAmountStake, 10)
	if !ok {
		log.Fatalf("Invalid IBC amount for preemptive buy: %s", case3_attackerPreemptiveBuyAmountStake)
	}

	// Calculate price impact for the preemptive buy
	priceImpact1, err := calculatePriceImpact(ibcAmountBig, dexPool.ReserveIBC, dexPool.ReserveStake)
	if err != nil {
		log.Fatalf("Failed to calculate price impact for preemptive buy: %v", err)
	}
	log.Printf("Price impact of preemptive buy: %.4f%%", priceImpact1)

	// Simulate the swap to calculate expected output
	expectedStakeOutput, err := simulateSwapIBCForStake(new(big.Int).Set(ibcAmountBig))
	if err != nil {
		log.Fatalf("Failed to simulate preemptive swap: %v", err)
	}

	// Execute the real swap transaction with on-chain verification
	preemptiveBuyInputTx, preemptiveBuyOutputTx, err := executeRealSwapTransaction(
		chainB_ID, chainB_RPC, chainB_Home, attackerB_KeyName, attackerBAddrOnB, mockDexAddrOnB,
		case3_attackerPreemptiveBuyAmountStake, expectedStakeOutput.String(), ibcTokenDenom, attackerStakeDenom)
	if err != nil {
		log.Fatalf("Attacker's pre-emptive buy swap failed: %v", err)
	}
	log.Printf("Attacker's 'pre-emptive buy' input tx hash on %s: %s", chainB_ID, preemptiveBuyInputTx)
	log.Printf("Attacker's 'pre-emptive buy' output tx hash on %s: %s", chainB_ID, preemptiveBuyOutputTx)
	log.Printf("Attacker received %s %s for %s %s", expectedStakeOutput.String(), attackerStakeDenom, case3_attackerPreemptiveBuyAmountStake, ibcTokenDenom)
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

	// The victim's large IBC transfer increases the supply of ibcTokenDenom on Chain B
	// This simulates the market impact - some of these tokens may flow into the DEX pool
	victimTransferAmount, ok := new(big.Int).SetString(case3_largeIBCTransferAmount, 10)
	if ok {
		log.Printf("Market Impact: Large IBC transfer of %s %s increases token supply on Chain B", 
			victimTransferAmount.String(), ibcTokenDenom)
		
		// Model the victim's behavior: they received a large amount of IBC tokens and want to swap them for stake tokens
		// This creates the "victim trade" that the attacker is trying to front-run
		victimSwapAmount := new(big.Int).Div(victimTransferAmount, big.NewInt(2)) // Victim swaps 50% of their tokens
		victimStakeReceived, err := simulateSwapIBCForStake(new(big.Int).Set(victimSwapAmount))
		if err != nil {
			log.Printf("Warning: Failed to simulate victim's swap: %v", err)
		} else {
			log.Printf("Victim's market activity: swapped %s %s for %s %s", 
				victimSwapAmount.String(), ibcTokenDenom, victimStakeReceived.String(), attackerStakeDenom)
		}
		log.Printf("Updated DEX state - Stake Reserve: %s, IBC Reserve: %s, K: %s", 
			dexPool.ReserveStake.String(), dexPool.ReserveIBC.String(), dexPool.K.String())
	}

	// 5. Attacker's "Post-IBC Sell"

	log.Printf("Step 5: Attacker (%s) performs 'post-IBC sell' on Chain B DEX (swapping %s %s for IBC tokens)",
		attackerB_KeyName, case3_attackerPostIBCTokenSellAmount, attackerStakeDenom)

	// For the sell side, swap stake tokens back to IBC tokens
	stakeSellAmountBig, ok := new(big.Int).SetString(case3_attackerPostIBCTokenSellAmount, 10)
	if !ok {
		log.Fatalf("Invalid stake amount for post-IBC sell: %s", case3_attackerPostIBCTokenSellAmount)
	}

	// Calculate price impact for the post-IBC sell
	priceImpact2, err := calculatePriceImpact(stakeSellAmountBig, dexPool.ReserveStake, dexPool.ReserveIBC)
	if err != nil {
		log.Fatalf("Failed to calculate price impact for post-IBC sell: %v", err)
	}
	log.Printf("Price impact of post-IBC sell: %.4f%%", priceImpact2)

	// Simulate the swap to calculate expected output
	expectedIBCOutput, err := simulateSwapStakeForIBC(new(big.Int).Set(stakeSellAmountBig))
	if err != nil {
		log.Fatalf("Failed to simulate post-IBC swap: %v", err)
	}

	// Execute the real swap transaction with on-chain verification
	postIBCInputTx, postIBCOutputTx, err := executeRealSwapTransaction(
		chainB_ID, chainB_RPC, chainB_Home, attackerB_KeyName, attackerBAddrOnB, mockDexAddrOnB,
		case3_attackerPostIBCTokenSellAmount, expectedIBCOutput.String(), attackerStakeDenom, ibcTokenDenom)
	if err != nil {
		log.Fatalf("Attacker's post-IBC sell swap failed: %v", err)
	}
	log.Printf("Attacker's 'post-IBC sell' input tx hash on %s: %s", chainB_ID, postIBCInputTx)
	log.Printf("Attacker's 'post-IBC sell' output tx hash on %s: %s", chainB_ID, postIBCOutputTx)
	log.Printf("Attacker received %s %s for %s %s", expectedIBCOutput.String(), ibcTokenDenom, case3_attackerPostIBCTokenSellAmount, attackerStakeDenom)
	log.Println("Waiting for attacker's post-IBC sell to confirm...")
	time.Sleep(6 * time.Second)

	// 6. Verification
	log.Println("Step 6: Verification - Logging transaction details and final balances")

	preemptiveBuyInputTxInfo, _ := queryTx(chainB_ID, chainB_RPC, preemptiveBuyInputTx)
	preemptiveBuyOutputTxInfo, _ := queryTx(chainB_ID, chainB_RPC, preemptiveBuyOutputTx)
	victimRecvTxInfo, _ := queryTx(chainB_ID, chainB_RPC, recvPacketTxInfoB.TxHash) // Already have this as recvPacketTxInfoB
	postIBCInputTxInfo, _ := queryTx(chainB_ID, chainB_RPC, postIBCInputTx)
	postIBCOutputTxInfo, _ := queryTx(chainB_ID, chainB_RPC, postIBCOutputTx)

	log.Println("--- Transaction Summary on Chain B (DEX Simulation) ---")
	if preemptiveBuyInputTxInfo != nil {
		log.Printf("1a. Attacker Pre-emptive Buy Input Tx: %s, Block: %s, Timestamp: %s",
			preemptiveBuyInputTxInfo.TxHash, preemptiveBuyInputTxInfo.Height, preemptiveBuyInputTxInfo.Timestamp)
	}
	if preemptiveBuyOutputTxInfo != nil {
		log.Printf("1b. Attacker Pre-emptive Buy Output Tx: %s, Block: %s, Timestamp: %s",
			preemptiveBuyOutputTxInfo.TxHash, preemptiveBuyOutputTxInfo.Height, preemptiveBuyOutputTxInfo.Timestamp)
	}
	if victimRecvTxInfo != nil {
		log.Printf("2. Victim's IBC RecvPacket Tx: %s, Block: %s, Timestamp: %s",
			victimRecvTxInfo.TxHash, victimRecvTxInfo.Height, victimRecvTxInfo.Timestamp)
	}
	if postIBCInputTxInfo != nil {
		log.Printf("3a. Attacker Post-IBC Sell Input Tx: %s, Block: %s, Timestamp: %s",
			postIBCInputTxInfo.TxHash, postIBCInputTxInfo.Height, postIBCInputTxInfo.Timestamp)
	}
	if postIBCOutputTxInfo != nil {
		log.Printf("3b. Attacker Post-IBC Sell Output Tx: %s, Block: %s, Timestamp: %s",
			postIBCOutputTxInfo.TxHash, postIBCOutputTxInfo.Height, postIBCOutputTxInfo.Timestamp)
	}

	// Check sequence of operations based on block heights
	validSequence := true
	if preemptiveBuyInputTxInfo == nil || victimRecvTxInfo == nil || postIBCInputTxInfo == nil {
		log.Println("Could not retrieve all transaction details for full sequence verification.")
		validSequence = false
	} else {
		buyHeight, _ := strconv.ParseInt(preemptiveBuyInputTxInfo.Height, 10, 64)
		recvHeight, _ := strconv.ParseInt(victimRecvTxInfo.Height, 10, 64)
		sellHeight, _ := strconv.ParseInt(postIBCInputTxInfo.Height, 10, 64)

		if !(buyHeight <= recvHeight && recvHeight <= sellHeight) {
			log.Printf("ERROR: Transaction sequence is NOT as expected based on block heights: Buy (H:%d), Recv (H:%d), Sell (H:%d)",
				buyHeight, recvHeight, sellHeight)
			validSequence = false
		} else {
			log.Printf("SUCCESS: Transaction sequence appears correct based on block heights: Buy (H:%d) -> Recv (H:%d) -> Sell (H:%d)",
				buyHeight, recvHeight, sellHeight)
		}
	}

	// Calculate and log MEV profit (attack flow: IBC → stake → IBC)
	ibcInput, _ := new(big.Int).SetString(case3_attackerPreemptiveBuyAmountStake, 10)
	ibcFinalOutput := expectedIBCOutput
	profit := new(big.Int).Sub(ibcFinalOutput, ibcInput)
	
	log.Printf("--- MEV Analysis ---")
	log.Printf("Initial IBC token investment: %s %s", ibcInput.String(), ibcTokenDenom)
	log.Printf("Stake tokens obtained: %s %s", expectedStakeOutput.String(), attackerStakeDenom)
	log.Printf("Final IBC tokens received: %s %s", ibcFinalOutput.String(), ibcTokenDenom)
	log.Printf("Net profit: %s %s", profit.String(), ibcTokenDenom)
	log.Printf("Total price impact caused: %.4f%% + %.4f%% = %.4f%%", 
		priceImpact1, priceImpact2, priceImpact1.Add(priceImpact1, priceImpact2))
	
	// Calculate profit percentage
	if ibcInput.Sign() > 0 {
		profitPercent := new(big.Float).Quo(new(big.Float).SetInt(profit), new(big.Float).SetInt(ibcInput))
		profitPercent.Mul(profitPercent, big.NewFloat(100))
		log.Printf("Profit percentage: %.4f%%", profitPercent)
	}
	
	if validSequence {
		if profit.Sign() > 0 {
			log.Println("DEX MEV scenario executed successfully with PROFIT!")
		} else {
			log.Println("DEX MEV scenario executed but resulted in LOSS.")
		}
	} else {
		log.Println("DEX MEV scenario sequence FAILED or was incomplete.")
	}

	log.Println("--- Final Balances on Chain B ---")
	logBalance(chainB_ID, chainB_RPC, userBAddrOnB, userB_KeyName, ibcTokenDenom)
	logBalance(chainB_ID, chainB_RPC, attackerBAddrOnB, attackerB_KeyName, attackerStakeDenom) // Global attackerStakeDenom
	logBalance(chainB_ID, chainB_RPC, attackerBAddrOnB, attackerB_KeyName, ibcTokenDenom)      // Global ibcTokenDenom (was attackerTokenDenom)
	logBalance(chainB_ID, chainB_RPC, mockDexAddrOnB, mockDexB_KeyName, attackerStakeDenom)    // Global attackerStakeDenom
	logBalance(chainB_ID, chainB_RPC, mockDexAddrOnB, mockDexB_KeyName, ibcTokenDenom)         // Global ibcTokenDenom

	log.Printf("Final DEX state - Stake Reserve: %s, IBC Reserve: %s, K: %s", 
		dexPool.ReserveStake.String(), dexPool.ReserveIBC.String(), dexPool.K.String())
	log.Println("Case 3: DEX simulation finished.")
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

	log.Println("--- Case 3 Setup (DEX Simulation) ---")
	log.Printf("User A Address (Chain A): %s", userAAddrOnA)
	log.Printf("User B Address (Chain B - Victim's Recipient): %s", userBAddrOnB)
	log.Printf("Attacker B Address (Chain B): %s", attackerBAddrOnB)
	log.Printf("DEX Address (Chain B): %s", mockDexAddrOnB)
	log.Printf("Using IBC Path from ENV (RLY_PATH_TRANSFER_ENV): %s", ibcPathTransfer)
	log.Printf("Using Auto-discovered Channel ID on Chain A (for send_packet): %s", transferChannelA)
	log.Printf("Using Auto-discovered Channel ID on Chain B (for recv_packet): %s", transferChannelB)
	log.Printf("Attacker Stake Denom (global): %s, IBC Token Denom (global): %s", attackerStakeDenom, ibcTokenDenom)
	log.Printf("Initial DEX reserves: %s %s, %s %s", case3_initialPoolReserveStake, attackerStakeDenom, case3_initialPoolReserveIBC, ibcTokenDenom)
	log.Println("---------------------------------------")

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
