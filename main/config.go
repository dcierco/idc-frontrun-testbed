package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// --- Global Configuration Variables ---
// These will be populated from environment variables
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

	// Key names - these are often stable and could remain const,
	// but can also be made configurable via ENV if needed.
	// For now, let's assume they are stable and keep them as consts
	// within each script's original const block if they don't change per 'make' run.
	// If they DO need to change via Makefile, add them here and load from ENV.

	// IBC Path/Channel IDs - These are critical and specific.
	// It's best if these are also passed via ENV or are very clearly documented
	// if they remain consts. For this update, I'll assume they might still be
	// consts in the scripts for now, as they are highly specific to the test case.
	// If you want them from ENV, add them here. Example:
	// channelA_ID_on_A_Env string
)

// init is automatically called when the package is loaded.
func init() {
	envVars := map[string]*string{
		"CHAIN_A_ID_ENV":   &chainA_ID,
		"CHAIN_A_RPC_ENV":  &chainA_RPC,
		"CHAIN_A_HOME_ENV": &chainA_Home,
		"CHAIN_B_ID_ENV":   &chainB_ID,
		"CHAIN_B_RPC_ENV":  &chainB_RPC,
		"CHAIN_B_HOME_ENV": &chainB_Home,
		"RLY_CONFIG_FILE_ENV": &rlyConfigPath,
	}

	missingVars := []string{}
	for envKey, valPtr := range envVars {
		val := os.Getenv(envKey)
		if val == "" {
			// For SIMD_BINARY_ENV and RLY_BINARY_ENV, we have defaults, so they aren't strictly "missing"
			// if the ENV var isn't set. Only add to missingVars if it's one of the core path/ID vars.
			if envKey != "SIMD_BINARY_ENV" && envKey != "RLY_BINARY_ENV" {
				missingVars = append(missingVars, envKey)
			}
		}
		*valPtr = val // Assign even if empty, defaults for simd/rly will be used if not set by ENV
	}

	if os.Getenv("SIMD_BINARY_ENV") != "" {
		simdBinary = os.Getenv("SIMD_BINARY_ENV")
	}
	if os.Getenv("RLY_BINARY_ENV") != "" {
		rlyBinary = os.Getenv("RLY_BINARY_ENV")
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


	if len(missingVars) > 0 { // missingVars is from your original loop
		log.Fatalf("FATAL: Required environment variables are not set: %v. Please run this script using the Makefile (e.g., 'make run') which sets these variables.", missingVars)
	}

	// Log the loaded configuration for verification
	log.Println("--- Script Configuration Loaded ---")
	log.Printf("SIMD Binary: %s", simdBinary)
	log.Printf("RLY Binary: %s", rlyBinary)
	log.Printf("Chain A ID: %s, RPC: %s, Home: %s", chainA_ID, chainA_RPC, chainA_Home)
	log.Printf("Chain B ID: %s, RPC: %s, Home: %s", chainB_ID, chainB_RPC, chainB_Home)
	log.Printf("Relayer Config Path: %s", rlyConfigPath)
	log.Println("-----------------------------------")
}
