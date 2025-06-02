package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
)

// --- Global Configuration Variables ---
// These will be populated from environment variables or have default values
var (
	simdBinary = "simd"
	rlyBinary  = "rly"

	chainA_ID   string
	chainA_RPC  string
	chainA_Home string

	chainB_ID   string
	chainB_RPC  string
	chainB_Home string

	rlyConfigPath string

	// Default values, can be overridden by specific case scripts if needed
	keyringBackend  = "test"
	defaultSrcPort  = "transfer"
	defaultDstPort  = "transfer"
	defaultIBCVersion = "ics20-1" // As used in Makefile for path creation
	defaultGasFlags = "--gas=auto --gas-adjustment=1.2"
	defaultFee      = "200000uatom"

	// Common Key Names (used across different cases)
	// These should match the keys created in the Makefile's `chains` target
	userA_KeyName     = "usera"      // Corresponds to USER_A_KEY in Makefile
	userB_KeyName     = "userb"      // Corresponds to USER_B_KEY in Makefile
	attackerB_KeyName = "attackerb"  // Corresponds to ATTACKER_B_KEY in Makefile
	mockDexB_KeyName  = "mockDexB"   // Corresponds to MOCK_DEX_B_KEY in Makefile

	// Common Token Denominations
	ibcTokenDenom      = "token" // Matches TOKEN_DENOM from Makefile
	attackerStakeDenom = "uatom" // Matches STAKE_DENOM from Makefile (used in case3 for dex interaction)

	// Addresses - these will be populated by setup() in each case script
	userAAddrOnA          string
	userBAddrOnB          string
	attackerBAddrOnB      string
	attackerReceiverAddrOnB string // Often attackerBAddrOnB, but can be different
	mockDexAddrOnB        string

	// Relayer Path Names - populated from environment variables set by Makefile
	// These correspond to RLY_PATH_AB_TRANSFER, RLY_PATH_ORDERED, RLY_PATH_UNORDERED in Makefile
	ibcPathTransfer  string
	ibcPathOrdered   string
	ibcPathUnordered string

	// Auto-discovered Channel IDs - populated from environment variables set by Makefile
	// These are discovered automatically after relayer paths are linked
	transferChannelA  string
	transferChannelB  string
	orderedChannelA   string
	orderedChannelB   string
	unorderedChannelA string
	unorderedChannelB string
)

// init is automatically called when the package is loaded.
func init() {
	envVars := map[string]*string{
		"CHAIN_A_ID_ENV":      &chainA_ID,
		"CHAIN_A_RPC_ENV":     &chainA_RPC,
		"CHAIN_A_HOME_ENV":    &chainA_Home,
		"CHAIN_B_ID_ENV":      &chainB_ID,
		"CHAIN_B_RPC_ENV":     &chainB_RPC,
		"CHAIN_B_HOME_ENV":    &chainB_Home,
		"RLY_CONFIG_FILE_ENV": &rlyConfigPath,
		// Add new env vars for relayer paths
		"RLY_PATH_TRANSFER_ENV":  &ibcPathTransfer,
		"RLY_PATH_ORDERED_ENV":   &ibcPathOrdered,
		"RLY_PATH_UNORDERED_ENV": &ibcPathUnordered,
		// Add new env vars for auto-discovered channel IDs
		"TRANSFER_CHANNEL_A_ENV":  &transferChannelA,
		"TRANSFER_CHANNEL_B_ENV":  &transferChannelB,
		"ORDERED_CHANNEL_A_ENV":   &orderedChannelA,
		"ORDERED_CHANNEL_B_ENV":   &orderedChannelB,
		"UNORDERED_CHANNEL_A_ENV": &unorderedChannelA,
		"UNORDERED_CHANNEL_B_ENV": &unorderedChannelB,
	}

	// Variables that have defaults and can be overridden by ENV
	envVarsWithDefaults := map[string]*string{
		"SIMD_BINARY_ENV": &simdBinary, // Default "simd"
		"RLY_BINARY_ENV":  &rlyBinary,  // Default "rly"
	}

	missingVars := []string{}
	for envKey, valPtr := range envVars {
		val := os.Getenv(envKey)
		if val == "" {
			// These are considered mandatory without defaults
			missingVars = append(missingVars, envKey)
		}
		*valPtr = val
	}

	for envKey, valPtr := range envVarsWithDefaults {
		val := os.Getenv(envKey)
		if val != "" { // If ENV var is set, it overrides the Go default
			*valPtr = val
		}
	}

	// Expand rlyConfigPath if it was loaded and might contain ~ or $HOME
	if rlyConfigPath != "" {
		if strings.HasPrefix(rlyConfigPath, "~/") {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				log.Fatalf("FATAL: Could not get user home directory to expand rlyConfigPath: %v", err)
			}
			rlyConfigPath = filepath.Join(homeDir, rlyConfigPath[2:])
		}
		rlyConfigPath = os.ExpandEnv(rlyConfigPath) // Handles $HOME or other env vars in the path
	}

	if len(missingVars) > 0 {
		log.Fatalf("FATAL: Required environment variables are not set: %v. Please run this script using the Makefile (e.g., 'make run') which sets these variables.", missingVars)
	}

	// Log the loaded configuration for verification
	log.Println("--- Script Configuration Loaded ---")
	log.Printf("SIMD Binary: %s", simdBinary)
	log.Printf("RLY Binary: %s", rlyBinary)
	log.Printf("Chain A ID: %s, RPC: %s, Home: %s", chainA_ID, chainA_RPC, chainA_Home)
	log.Printf("Chain B ID: %s, RPC: %s, Home: %s", chainB_ID, chainB_RPC, chainB_Home)
	log.Printf("Relayer Config Path: %s", rlyConfigPath)
	log.Printf("Relayer Path (Transfer): %s", ibcPathTransfer)
	log.Printf("Relayer Path (Ordered): %s", ibcPathOrdered)
	log.Printf("Relayer Path (Unordered): %s", ibcPathUnordered)
	log.Printf("Auto-discovered Channels - Transfer: A=%s, B=%s", transferChannelA, transferChannelB)
	log.Printf("Auto-discovered Channels - Ordered: A=%s, B=%s", orderedChannelA, orderedChannelB)
	log.Printf("Auto-discovered Channels - Unordered: A=%s, B=%s", unorderedChannelA, unorderedChannelB)

	log.Printf("Default Keyring Backend: %s", keyringBackend)
	log.Printf("Default Source Port: %s", defaultSrcPort)
	log.Printf("Default Destination Port: %s", defaultDstPort)
	log.Printf("Default IBC Version: %s", defaultIBCVersion)
	log.Printf("Default Gas Flags: %s", defaultGasFlags)
	log.Printf("Default Fee: %s", defaultFee)

	log.Printf("User A Key Name: %s", userA_KeyName)
	log.Printf("User B Key Name: %s", userB_KeyName)
	log.Printf("Attacker B Key Name: %s", attackerB_KeyName)
	log.Printf("Mock DEX B Key Name: %s", mockDexB_KeyName)
	log.Printf("IBC Token Denom: %s", ibcTokenDenom)
	log.Printf("Attacker Stake Denom: %s", attackerStakeDenom)
	log.Println("-----------------------------------")
}
