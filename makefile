# Makefile for IBC Front-Running Tests

# --- Configuration ---
# User-configurable variables
GO := go
RLY := rly
# SIMD_BINARY_NAME is now fixed to 'simd' as per standard Cosmos SDK installation
SIMD_BINARY_NAME := simd
SIMD_BINARY := $(shell command -v $(SIMD_BINARY_NAME) 2> /dev/null)

# Project layout (chain data will be created within this repo)
REPO_DIR := $(shell pwd)
CHAIN_A_HOME := $(REPO_DIR)/chain-a-data
CHAIN_B_HOME := $(REPO_DIR)/chain-b-data

CHAIN_A_ID := chain-a
CHAIN_B_ID := chain-b
# RPC/gRPC ports for chains (ensure these are distinct and match Go script configs)
CHAIN_A_RPC_PORT := 26657
CHAIN_A_GRPC_PORT := 9090
CHAIN_B_RPC_PORT := 27657
CHAIN_B_GRPC_PORT := 9190

CHAIN_A_RPC_LADDR := tcp://0.0.0.0:$(CHAIN_A_RPC_PORT)# Listening address for simd start
CHAIN_A_RPC_CLIENT := http://localhost:$(CHAIN_A_RPC_PORT)# Client address for Go scripts
CHAIN_A_GRPC_LADDR := 0.0.0.0:$(CHAIN_A_GRPC_PORT)
CHAIN_A_GRPC_CLIENT := localhost:$(CHAIN_A_GRPC_PORT)

CHAIN_B_RPC_LADDR := tcp://0.0.0.0:$(CHAIN_B_RPC_PORT)
CHAIN_B_RPC_CLIENT := http://localhost:$(CHAIN_B_RPC_PORT)
CHAIN_B_GRPC_LADDR := 0.0.0.0:$(CHAIN_B_GRPC_PORT)
CHAIN_B_GRPC_CLIENT := localhost:$(CHAIN_B_GRPC_PORT)


# Accounts (these are names, actual addresses will be generated)
USER_A_KEY := usera
USER_B_KEY := userb
ATTACKER_B_KEY := attackerb
MOCK_DEX_B_KEY := mockDexB # For Case 3
VALIDATOR_A_KEY := validatora
VALIDATOR_B_KEY := validatorb

# Funding amounts
STAKE_DENOM := stake
TOKEN_DENOM := token
DEFAULT_ACCOUNT_BALANCE := 1000000000$(STAKE_DENOM),1000000000$(TOKEN_DENOM)
VALIDATOR_STAKE_AMOUNT := 500000000$(STAKE_DENOM)

# Relayer configuration
RLY_CONFIG_DIR := $(HOME)/.relayer
RLY_CONFIG_FILE := $(RLY_CONFIG_DIR)/config/config.yaml
# Path names for rly (must match what's in your rly config and script constants)
RLY_PATH_AB_TRANSFER := a-b-transfer
RLY_PATH_ORDERED := a-b-ordered     # For Case 4
RLY_PATH_UNORDERED := a-b-unordered # For Case 4

# Go script names (assuming they are in the current directory)
SCRIPTS := $(wildcard main/case*.go)

# --- Targets ---

.PHONY: all verify dependencies chains setup-rly-config start-chains run clean help

all: verify dependencies chains setup-rly-config start-chains run
	@echo "All setup and run steps completed."

verify:
	@echo "Verifying dependencies..."
	@command -v $(GO) >/dev/null 2>&1 || (echo "Go is not installed or not in PATH. Please install Go."; exit 1)
	@echo "Go found: $(shell $(GO) version)"
	@command -v $(RLY) >/dev/null 2>&1 || (echo "Relayer binary ('$(RLY)') is not installed or not in PATH. Run 'make dependencies' or install manually."; exit 1)
	@echo "Relayer 'rly' found: $(shell $(RLY) version)"
	@command -v $(SIMD_BINARY_NAME) >/dev/null 2>&1 || (echo "Chain binary ('$(SIMD_BINARY_NAME)') is not found. Please install it from Cosmos SDK ('make install' in cosmos-sdk repo)."; exit 1)
	@echo "Chain binary '$(SIMD_BINARY_NAME)' found: $(shell $(SIMD_BINARY_NAME) version)"
	@command -v jq >/dev/null 2>&1 || (echo "'jq' is not installed. Please install jq (e.g., 'sudo apt-get install jq' or 'brew install jq')."; exit 1)
	@echo "'jq' found."
	@echo "Verification successful."

dependencies:
	@echo "Installing/checking Go IBC relayer ('rly')..."
	@command -v $(RLY) >/dev/null 2>&1 || { \
		echo "'rly' not found. Attempting to install..."; \
		cd $$(mktemp -d) && $(GO) install github.com/cosmos/relayer/v2/cmd/rly@latest && cd -; \
		command -v $(RLY) >/dev/null 2>&1 || (echo "Failed to install 'rly'. Please install it manually."; exit 1); \
	}
	@echo "'rly' is available: $(shell $(RLY) version)"
	@echo "Ensuring 'simd' is installed (from Cosmos SDK)..."
	@command -v $(SIMD_BINARY_NAME) >/dev/null 2>&1 || { \
		echo "'$(SIMD_BINARY_NAME)' not found. Please install it from the Cosmos SDK repository:"; \
		echo "  git clone https://github.com/cosmos/cosmos-sdk"; \
		echo "  cd cosmos-sdk"; \
		echo "  make install"; \
		echo "  cd .."; \
		echo "Then, re-run 'make dependencies' or 'make verify'."; \
		exit 1; \
	}
	@echo "'$(SIMD_BINARY_NAME)' is available: $(shell $(SIMD_BINARY_NAME) version)"
	@echo "Dependencies checked/installed."


chains: clean-chain-data
	@echo "Setting up chain data directories..."
	@mkdir -p $(CHAIN_A_HOME) $(CHAIN_B_HOME) $(REPO_DIR)/configs # Ensure configs dir is also made here

	@echo "Initializing Chain A ($(CHAIN_A_ID))..."
	@$(SIMD_BINARY_NAME) init $(VALIDATOR_A_KEY) --chain-id $(CHAIN_A_ID) --home $(CHAIN_A_HOME) --default-denom $(STAKE_DENOM)
	@echo "Initializing Chain B ($(CHAIN_B_ID))..."
	@$(SIMD_BINARY_NAME) init $(VALIDATOR_B_KEY) --chain-id $(CHAIN_B_ID) --home $(CHAIN_B_HOME) --default-denom $(STAKE_DENOM)

	@echo "Configuring Chain A (home: $(CHAIN_A_HOME))..."
	@sed -i.bak 's/"stake"/"$(STAKE_DENOM)"/g' $(CHAIN_A_HOME)/config/genesis.json
	@sed -i.bak 's/enable = false/enable = true/g' $(CHAIN_A_HOME)/config/app.toml
	@sed -i.bak 's/swagger = false/swagger = true/g' $(CHAIN_A_HOME)/config/app.toml
	@sed -i.bak 's#^laddr = "tcp://127.0.0.1:26657"#laddr = "$(CHAIN_A_RPC_LADDR)"#' $(CHAIN_A_HOME)/config/config.toml
	@sed -i.bak 's#^address = "tcp://localhost:26657"#address = "$(CHAIN_A_RPC_LADDR)"#' $(CHAIN_A_HOME)/config/app.toml # For GRPC Web
	@sed -i.bak 's#^address = "localhost:9090"#address = "$(CHAIN_A_GRPC_LADDR)"#' $(CHAIN_A_HOME)/config/app.toml # For GRPC

	@echo "Configuring Chain B (home: $(CHAIN_B_HOME))..."
	@sed -i.bak 's/"stake"/"$(STAKE_DENOM)"/g' $(CHAIN_B_HOME)/config/genesis.json
	@sed -i.bak 's/enable = false/enable = true/g' $(CHAIN_B_HOME)/config/app.toml
	@sed -i.bak 's/swagger = false/swagger = true/g' $(CHAIN_B_HOME)/config/app.toml
	@sed -i.bak 's#^laddr = "tcp://127.0.0.1:26657"#laddr = "$(CHAIN_B_RPC_LADDR)"#' $(CHAIN_B_HOME)/config/config.toml
	@sed -i.bak 's#^address = "tcp://localhost:26657"#address = "$(CHAIN_B_RPC_LADDR)"#' $(CHAIN_B_HOME)/config/app.toml
	@sed -i.bak 's#^address = "localhost:9090"#address = "$(CHAIN_B_GRPC_LADDR)"#' $(CHAIN_B_HOME)/config/app.toml

	@echo "Adding accounts to Chain A..."
	@$(SIMD_BINARY_NAME) keys add $(USER_A_KEY) --keyring-backend test --home $(CHAIN_A_HOME) --output json
	@$(SIMD_BINARY_NAME) keys add $(VALIDATOR_A_KEY) --keyring-backend test --home $(CHAIN_A_HOME) --output json

	@echo "Adding accounts to Chain A and capturing mnemonics..."
	@VALIDATOR_A_KEY_INFO=$$($(SIMD_BINARY_NAME) keys add $(VALIDATOR_A_KEY) --keyring-backend test --home $(CHAIN_A_HOME) --output json)
	@echo "$$VALIDATOR_A_KEY_INFO" | jq -r .mnemonic > $(CHAIN_A_HOME)/$(VALIDATOR_A_KEY).mnemonic
	@echo "Validator A key added. Mnemonic stored temporarily."

	@echo "Adding accounts to genesis for Chain A..."
	@$(SIMD_BINARY_NAME) genesis add-genesis-account $$($(SIMD_BINARY_NAME) keys show $(USER_A_KEY) -a --keyring-backend test --home $(CHAIN_A_HOME)) $(DEFAULT_ACCOUNT_BALANCE) --home $(CHAIN_A_HOME) --keyring-backend test
	@$(SIMD_BINARY_NAME) genesis add-genesis-account $$($(SIMD_BINARY_NAME) keys show $(VALIDATOR_A_KEY) -a --keyring-backend test --home $(CHAIN_A_HOME)) $(DEFAULT_ACCOUNT_BALANCE) --home $(CHAIN_A_HOME) --keyring-backend test

	@echo "Generating gentx for Chain A validator $(VALIDATOR_A_KEY)..."
	@VALIDATOR_A_KEY_JSON=$$($(SIMD_BINARY_NAME) keys show $(VALIDATOR_A_KEY) --keyring-backend test --home $(CHAIN_A_HOME) --output json)
	@VALIDATOR_A_ESCAPED_PUBKEY_STR=$$(echo "$$VALIDATOR_A_KEY_JSON" | jq -r .pubkey)
	@VALIDATOR_A_PUBKEY_B64=$$(echo "$$VALIDATOR_A_ESCAPED_PUBKEY_STR" | jq -r .key)
	@VALIDATOR_A_GENTX_PUBKEY="{\"@type\":\"/cosmos.crypto.secp256k1.PubKey\",\"key\":\"$$VALIDATOR_A_PUBKEY_B64\"}"
	@$(SIMD_BINARY_NAME) genesis gentx $(VALIDATOR_A_KEY) $(VALIDATOR_STAKE_AMOUNT) \
		--chain-id $(CHAIN_A_ID) \
		--keyring-backend test \
		--home $(CHAIN_A_HOME) \
		--from $(VALIDATOR_A_KEY) \
		--pubkey "$$VALIDATOR_A_GENTX_PUBKEY"

	@echo "Adding accounts to Chain B and capturing mnemonics..."
	@VALIDATOR_B_KEY_INFO=$$($(SIMD_BINARY_NAME) keys add $(VALIDATOR_B_KEY) --keyring-backend test --home $(CHAIN_B_HOME) --output json)
	@echo "$$VALIDATOR_B_KEY_INFO" | jq -r .mnemonic > $(CHAIN_B_HOME)/$(VALIDATOR_B_KEY).mnemonic
	@echo "Validator B key added. Mnemonic stored temporarily."
	@$(SIMD_BINARY_NAME) keys add $(USER_B_KEY) --keyring-backend test --home $(CHAIN_B_HOME) --output json
	@$(SIMD_BINARY_NAME) keys add $(ATTACKER_B_KEY) --keyring-backend test --home $(CHAIN_B_HOME) --output json
	@$(SIMD_BINARY_NAME) keys add $(MOCK_DEX_B_KEY) --keyring-backend test --home $(CHAIN_B_HOME) --output json

	@echo "Adding accounts to genesis for Chain B..."
	@$(SIMD_BINARY_NAME) genesis add-genesis-account $$($(SIMD_BINARY_NAME) keys show $(USER_B_KEY) -a --keyring-backend test --home $(CHAIN_B_HOME)) $(DEFAULT_ACCOUNT_BALANCE) --home $(CHAIN_B_HOME) --keyring-backend test
	@$(SIMD_BINARY_NAME) genesis add-genesis-account $$($(SIMD_BINARY_NAME) keys show $(ATTACKER_B_KEY) -a --keyring-backend test --home $(CHAIN_B_HOME)) $(DEFAULT_ACCOUNT_BALANCE) --home $(CHAIN_B_HOME) --keyring-backend test
	@$(SIMD_BINARY_NAME) genesis add-genesis-account $$($(SIMD_BINARY_NAME) keys show $(MOCK_DEX_B_KEY) -a --keyring-backend test --home $(CHAIN_B_HOME)) $(DEFAULT_ACCOUNT_BALANCE) --home $(CHAIN_B_HOME) --keyring-backend test
	@$(SIMD_BINARY_NAME) genesis add-genesis-account $$($(SIMD_BINARY_NAME) keys show $(VALIDATOR_B_KEY) -a --keyring-backend test --home $(CHAIN_B_HOME)) $(DEFAULT_ACCOUNT_BALANCE) --home $(CHAIN_B_HOME) --keyring-backend test

	@echo "Generating gentx for Chain B validator $(VALIDATOR_B_KEY)..."
	@# (Using the complex pubkey extraction from your latest Makefile)
	@VALIDATOR_B_KEY_JSON=$$($(SIMD_BINARY_NAME) keys show $(VALIDATOR_B_KEY) --keyring-backend test --home $(CHAIN_B_HOME) --output json)
	@VALIDATOR_B_ESCAPED_PUBKEY_STR=$$(echo "$$VALIDATOR_B_KEY_JSON" | jq -r .pubkey)
	@VALIDATOR_B_PUBKEY_B64=$$(echo "$$VALIDATOR_B_ESCAPED_PUBKEY_STR" | jq -r .key)
	@VALIDATOR_B_GENTX_PUBKEY="{\"@type\":\"/cosmos.crypto.secp256k1.PubKey\",\"key\":\"$$VALIDATOR_B_PUBKEY_B64\"}"
	@$(SIMD_BINARY_NAME) genesis gentx $(VALIDATOR_B_KEY) $(VALIDATOR_STAKE_AMOUNT) \
		--chain-id $(CHAIN_B_ID) \
		--keyring-backend test \
		--home $(CHAIN_B_HOME) \
		--from $(VALIDATOR_B_KEY) \
		--pubkey "$$VALIDATOR_B_GENTX_PUBKEY"

	@echo "Collecting gentxs for Chain A..."
	@$(SIMD_BINARY_NAME) genesis collect-gentxs --home $(CHAIN_A_HOME)
	@echo "Collecting gentxs for Chain B..."
	@$(SIMD_BINARY_NAME) genesis collect-gentxs --home $(CHAIN_B_HOME)

	@echo "Chain setup complete. Data in $(CHAIN_A_HOME) and $(CHAIN_B_HOME)"
	@echo "IMPORTANT: Update your Go scripts' const blocks to use these paths and client RPC/gRPC addresses:"
	@echo "  Chain A Home: $(CHAIN_A_HOME), RPC: $(CHAIN_A_RPC_CLIENT), gRPC: $(CHAIN_A_GRPC_CLIENT)"
	@echo "  Chain B Home: $(CHAIN_B_HOME), RPC: $(CHAIN_B_RPC_CLIENT), gRPC: $(CHAIN_B_GRPC_CLIENT)"
	@echo "Also, ensure your relayer configuration ('$(RLY_CONFIG_FILE)') points to these local chains."


setup-rly-config:
	@echo "Setting up relayer configuration..."
	@command -v $(RLY) >/dev/null 2>&1 || (echo "Relayer 'rly' not found. Run 'make dependencies'."; exit 1)

	@echo "Checking relayer configuration..."
	@if [ ! -f "$(RLY_CONFIG_FILE)" ]; then \
		echo "Initializing new relayer config..."; \
		$(RLY) config init --home $(RLY_CONFIG_DIR); \
	else \
		echo "Using existing relayer config at $(RLY_CONFIG_FILE)"; \
	fi

	@echo "Creating chain configuration files..."
	@mkdir -p $(REPO_DIR)/configs
	@printf '{\n  "type": "cosmos",\n  "value": {\n    "key": "$(VALIDATOR_A_KEY)",\n    "chain-id": "$(CHAIN_A_ID)",\n    "rpc-addr": "$(strip $(CHAIN_A_RPC_CLIENT))",\n    "grpc-addr": "$(strip $(CHAIN_A_GRPC_CLIENT))",\n    "account-prefix": "cosmos",\n    "keyring-backend": "test",\n    "gas-adjustment": 1.5,\n    "gas-prices": "0.001$(STAKE_DENOM)",\n    "debug": true,\n    "timeout": "10s",\n    "output-format": "json",\n    "sign-mode": "direct"\n  }\n}' > $(REPO_DIR)/configs/chain-a.json
	@printf '{\n  "type": "cosmos",\n  "value": {\n    "key": "$(VALIDATOR_B_KEY)",\n    "chain-id": "$(CHAIN_B_ID)",\n    "rpc-addr": "$(strip $(CHAIN_B_RPC_CLIENT))",\n    "grpc-addr": "$(strip $(CHAIN_B_GRPC_CLIENT))",\n    "account-prefix": "cosmos",\n    "keyring-backend": "test",\n    "gas-adjustment": 1.5,\n    "gas-prices": "0.001$(STAKE_DENOM)",\n    "debug": true,\n    "timeout": "10s",\n    "output-format": "json",\n    "sign-mode": "direct"\n  }\n}' > $(REPO_DIR)/configs/chain-b.json

	@echo "Adding or updating chains in relayer config..."
	@-$(RLY) chains delete $(CHAIN_A_ID) --home $(RLY_CONFIG_DIR) 2>/dev/null || true
	@-$(RLY) chains delete $(CHAIN_B_ID) --home $(RLY_CONFIG_DIR) 2>/dev/null || true
	@$(RLY) chains add --file $(REPO_DIR)/configs/chain-a.json --home $(RLY_CONFIG_DIR)
	@$(RLY) chains add --file $(REPO_DIR)/configs/chain-b.json --home $(RLY_CONFIG_DIR)

	@echo "Restoring relayer keys (manual step)..."
	@echo "MANUAL STEP: Restore relayer keys for $(CHAIN_A_ID) and $(CHAIN_B_ID) using:"
	@echo "  rly keys restore $(CHAIN_A_ID) $(VALIDATOR_A_KEY) \"<mnemonic>\" --home $(RLY_CONFIG_DIR)"
	@echo "  rly keys restore $(CHAIN_B_ID) $(VALIDATOR_B_KEY) \"<mnemonic>\" --home $(RLY_CONFIG_DIR)"

	@echo "Creating or updating IBC paths..."
	@-$(RLY) paths delete $(RLY_PATH_AB_TRANSFER) --home $(RLY_CONFIG_DIR) 2>/dev/null || true
	@-$(RLY) paths delete $(RLY_PATH_ORDERED) --home $(RLY_CONFIG_DIR) 2>/dev/null || true
	@-$(RLY) paths delete $(RLY_PATH_UNORDERED) --home $(RLY_CONFIG_DIR) 2>/dev/null || true
	@$(RLY) paths new $(CHAIN_A_ID) $(CHAIN_B_ID) $(RLY_PATH_AB_TRANSFER) --src-port transfer --dst-port transfer --order ordered --version ics20-1 --home $(RLY_CONFIG_DIR)
	@$(RLY) paths new $(CHAIN_A_ID) $(CHAIN_B_ID) $(RLY_PATH_ORDERED) --src-port transfer --dst-port transfer --order ordered --version ics20-1 --home $(RLY_CONFIG_DIR)
	@$(RLY) paths new $(CHAIN_A_ID) $(CHAIN_B_ID) $(RLY_PATH_UNORDERED) --src-port transfer --dst-port transfer --order unordered --version ics20-1 --home $(RLY_CONFIG_DIR)

	@echo "Relayer configuration complete. Next steps:"
	@echo "1. Start your chains using 'make start-chains'"
	@echo "2. Restore relayer keys as shown above"
	@echo "3. Link paths with 'rly tx link $(RLY_PATH_AB_TRANSFER)'"
	@echo "4. Run your test scripts with 'make run'"


start-chains:
	@echo "Starting chains (manual step - run these in separate terminals):"
	@echo "  $(SIMD_BINARY_NAME) start --home $(CHAIN_A_HOME) --rpc.laddr $(CHAIN_A_RPC_LADDR) --grpc.address $(CHAIN_A_GRPC_LADDR) --log_level info"
	@echo "  $(SIMD_BINARY_NAME) start --home $(CHAIN_B_HOME) --rpc.laddr $(CHAIN_B_RPC_LADDR) --grpc.address $(CHAIN_B_GRPC_LADDR) --log_level info"
	@echo "Ensure chains are running and producing blocks before proceeding."

run:
	@echo "Running Go test scripts..."
	@echo "Ensure chains are running and relayer paths are established (but 'rly start' might need to be off for controlled tests)."
	@# Pass necessary config to Go scripts via environment variables
	@for script in $(SCRIPTS); do \
		echo "--- Running $$script ---"; \
		CHAIN_A_HOME_ENV=$(CHAIN_A_HOME) \
		CHAIN_B_HOME_ENV=$(CHAIN_B_HOME) \
		CHAIN_A_RPC_ENV=$(CHAIN_A_RPC_CLIENT) \
		CHAIN_B_RPC_ENV=$(CHAIN_B_RPC_CLIENT) \
		CHAIN_A_ID_ENV=$(CHAIN_A_ID) \
		CHAIN_B_ID_ENV=$(CHAIN_B_ID) \
		RLY_CONFIG_FILE_ENV=$(RLY_CONFIG_FILE) \
		$(GO) run $$script || echo "Error running $$script. Continuing..."; \
		echo "--- Finished $$script ---"; \
		echo "Pausing for 10 seconds before next script..."; \
		sleep 10; \
	done
	@echo "All scripts finished."

clean-chain-data:
	@echo "Cleaning up previous chain data..."
	@rm -rf $(CHAIN_A_HOME) $(CHAIN_B_HOME) $(REPO_DIR)/configs

clean: clean-chain-data
	@echo "Cleanup complete."

help:
	@echo "Available targets:"
	@echo "  all                 - Verifies, installs dependencies, sets up chains, (attempts basic rly config), and runs tests."
	@echo "  verify              - Checks if Go, rly, simd, and jq are installed and accessible."
	@echo "  dependencies        - Installs 'rly' if not found and checks for 'simd'."
	@echo "  chains              - Initializes fresh chain data, accounts, and genesis for Chain A & B."
	@echo "  setup-rly-config    - Basic relayer config initialization. Manual steps for keys and linking are needed."
	@echo "  start-chains        - Prints commands to start the chains manually."
	@echo "  run                 - Executes all 'case*.go' test scripts, passing config via environment variables."
	@echo "  clean-chain-data    - Removes local chain data directories and temp configs."
	@echo "  clean               - Removes local chain data and temp configs."
	@echo ""
	@echo "Configuration Variables (can be overridden, e.g., 'make chains STAKE_DENOM=uatom'):"
	@echo "  STAKE_DENOM         - Staking/fee denom for chains (default: stake)"
	@echo "  TOKEN_DENOM         - IBC transfer token denom (default: token)"
	@echo "  ... and others defined at the top of the Makefile."
