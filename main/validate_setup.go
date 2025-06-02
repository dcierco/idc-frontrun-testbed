package main

import (
	"fmt"
	"log"
	"time"
)

func main() {
	log.Println("=== IBC Front-Running Testbed Setup Validation ===")
	
	// Test 1: Verify configuration is loaded
	log.Println("\n1. Verifying configuration...")
	if err := validateConfig(); err != nil {
		log.Fatalf("Configuration validation failed: %v", err)
	}
	log.Println("✓ Configuration loaded successfully")

	// Test 2: Check chain connectivity
	log.Println("\n2. Testing chain connectivity...")
	if err := validateChains(); err != nil {
		log.Fatalf("Chain connectivity failed: %v", err)
	}
	log.Println("✓ Both chains are accessible")

	// Test 3: Verify relayer paths and channels
	log.Println("\n3. Validating relayer setup...")
	if err := validateRelayerSetup(); err != nil {
		log.Fatalf("Relayer setup validation failed: %v", err)
	}
	log.Println("✓ Relayer paths and channels are properly configured")

	// Test 4: Test basic IBC transfer
	log.Println("\n4. Testing basic IBC functionality...")
	if err := testBasicIBCTransfer(); err != nil {
		log.Fatalf("Basic IBC test failed: %v", err)
	}
	log.Println("✓ Basic IBC transfer successful")

	log.Println("\n=== All Validations Passed! ===")
	log.Println("The testbed is ready for front-running experiments.")
	log.Println("You can now run 'make run' to execute all test cases.")
}

func validateConfig() error {
	// Check all required configuration variables
	if chainA_ID == "" || chainB_ID == "" {
		return fmt.Errorf("chain IDs not set")
	}
	if chainA_RPC == "" || chainB_RPC == "" {
		return fmt.Errorf("chain RPC endpoints not set")
	}
	if ibcPathTransfer == "" || ibcPathOrdered == "" || ibcPathUnordered == "" {
		return fmt.Errorf("relayer paths not set")
	}
	if transferChannelA == "" || transferChannelB == "" {
		return fmt.Errorf("transfer channel IDs not discovered")
	}
	if orderedChannelA == "" || orderedChannelB == "" {
		return fmt.Errorf("ordered channel IDs not discovered")
	}
	if unorderedChannelA == "" || unorderedChannelB == "" {
		return fmt.Errorf("unordered channel IDs not discovered")
	}
	
	log.Printf("  Chain A: %s (%s)", chainA_ID, chainA_RPC)
	log.Printf("  Chain B: %s (%s)", chainB_ID, chainB_RPC)
	log.Printf("  Transfer channels: A=%s, B=%s", transferChannelA, transferChannelB)
	log.Printf("  Ordered channels: A=%s, B=%s", orderedChannelA, orderedChannelB)
	log.Printf("  Unordered channels: A=%s, B=%s", unorderedChannelA, unorderedChannelB)
	
	return nil
}

func validateChains() error {
	// Test Chain A connectivity
	_, stderr, err := executeCommand(false, simdBinary, "status", "--node", chainA_RPC)
	if err != nil {
		return fmt.Errorf("Chain A (%s) not accessible: %v, stderr: %s", chainA_RPC, err, stderr)
	}
	
	// Test Chain B connectivity
	_, stderr, err = executeCommand(false, simdBinary, "status", "--node", chainB_RPC)
	if err != nil {
		return fmt.Errorf("Chain B (%s) not accessible: %v, stderr: %s", chainB_RPC, err, stderr)
	}
	
	return nil
}

func validateRelayerSetup() error {
	// Check if relayer paths exist and show channel information
	paths := []string{ibcPathTransfer, ibcPathOrdered, ibcPathUnordered}
	channels := [][]string{
		{transferChannelA, transferChannelB},
		{orderedChannelA, orderedChannelB},
		{unorderedChannelA, unorderedChannelB},
	}
	
	for i, path := range paths {
		log.Printf("  Checking path: %s", path)
		stdout, stderr, err := executeCommand(false, rlyBinary, "paths", "show", path, "--home", rlyConfigPath)
		if err != nil {
			return fmt.Errorf("relayer path %s not found: %v, stderr: %s", path, err, stderr)
		}
		
		// Verify channel IDs are not empty
		if channels[i][0] == "" || channels[i][1] == "" {
			return fmt.Errorf("channel IDs for path %s are empty", path)
		}
		
		log.Printf("    ✓ Channels: A=%s, B=%s", channels[i][0], channels[i][1])
		_ = stdout // We have the path info but don't need to parse it for validation
	}
	
	return nil
}

func testBasicIBCTransfer() error {
	// Get user addresses first
	userAAddr, err := getKeyAddress(chainA_ID, chainA_RPC, userA_KeyName, chainA_Home, keyringBackend)
	if err != nil {
		return fmt.Errorf("failed to get userA address: %v", err)
	}
	
	userBAddr, err := getKeyAddress(chainB_ID, chainB_RPC, userB_KeyName, chainB_Home, keyringBackend)
	if err != nil {
		return fmt.Errorf("failed to get userB address: %v", err)
	}
	
	log.Printf("  UserA address: %s", userAAddr)
	log.Printf("  UserB address: %s", userBAddr)
	
	// Check initial balance of userB on Chain B
	initialBalance, err := queryBalance(chainB_ID, chainB_RPC, userBAddr, ibcTokenDenom)
	if err != nil {
		log.Printf("  Warning: Could not query initial balance: %v", err)
		initialBalance = "unknown"
	}
	log.Printf("  UserB initial %s balance: %s", ibcTokenDenom, initialBalance)
	
	// Send a small test transfer
	testAmount := "1" + ibcTokenDenom
	log.Printf("  Sending test transfer: %s from userA to userB", testAmount)
	
	txHash, err := ibcTransfer(chainA_ID, chainA_RPC, chainA_Home, userA_KeyName, userBAddr,
		defaultSrcPort, transferChannelA, testAmount, defaultFee, defaultGasFlags)
	if err != nil {
		return fmt.Errorf("test IBC transfer failed: %v", err)
	}
	
	log.Printf("  Transfer submitted, tx hash: %s", txHash)
	log.Printf("  Waiting for transaction to be indexed...")
	time.Sleep(5 * time.Second)
	
	// Find packet sequence
	packetSequence, err := findPacketSequenceFromTx(chainA_ID, chainA_RPC, txHash, defaultSrcPort, transferChannelA)
	if err != nil {
		return fmt.Errorf("could not find packet sequence: %v", err)
	}
	log.Printf("  Packet sequence: %s", packetSequence)
	
	// Relay the packet
	log.Printf("  Relaying packet...")
	err = relaySpecificPacket(rlyConfigPath, ibcPathTransfer, transferChannelA, packetSequence)
	if err != nil {
		log.Printf("  Warning: Relay command failed: %v (packet might already be relayed)", err)
	}
	
	// Wait for processing
	log.Printf("  Waiting for packet to be processed...")
	time.Sleep(8 * time.Second)
	
	// Verify the packet was received
	_, err = findRecvPacketTx(chainB_ID, chainB_RPC, defaultDstPort, transferChannelB, packetSequence)
	if err != nil {
		return fmt.Errorf("packet was not received on Chain B: %v", err)
	}
	
	// Check final balance
	finalBalance, err := queryBalance(chainB_ID, chainB_RPC, userBAddr, ibcTokenDenom)
	if err != nil {
		log.Printf("  Warning: Could not query final balance: %v", err)
	} else {
		log.Printf("  UserB final %s balance: %s", ibcTokenDenom, finalBalance)
	}
	
	log.Printf("  ✓ Test IBC transfer completed successfully")
	return nil
}