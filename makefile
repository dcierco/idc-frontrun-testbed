
# --- Configuration ---
GO := go1.23.9
RLY := rly

# Gaia source and build configuration
GAIA_REPO_URL := https://github.com/cosmos/gaia.git
GAIA_CHECKOUT := v23.3.0
# Use CURDIR to make path relative to Makefile location, robust for tool execution
GAIA_SOURCE_DIR := $(CURDIR)/gaia_source
LOCAL_GAIAD_BUILT_PATH := $(GAIA_SOURCE_DIR)/build/gaiad
LOCAL_GAIA_IMAGE_NAME := local/gaiad-arm64-custom
LOCAL_GAIA_IMAGE_TAG := $(GAIA_CHECKOUT)
LOCAL_GAIA_FULL_IMAGE := $(LOCAL_GAIA_IMAGE_NAME):$(LOCAL_GAIA_IMAGE_TAG)

# SIMD_BINARY_NAME will point to our locally built gaiad for host operations
SIMD_BINARY_NAME := $(LOCAL_GAIAD_BUILT_PATH)

# Project directories (CURDIR makes them relative to Makefile location)
CHAIN_A_HOME := $(CURDIR)/chain-a-data
CHAIN_B_HOME := $(CURDIR)/chain-b-data
CONFIGS_DIR := $(CURDIR)/configs
TMP_BUILD_DIR := $(CURDIR)/tmp_build

CHAIN_A_ID := gaia_9000-1
CHAIN_B_ID := gaia_9001-1

CHAIN_A_RPC_PORT := 26657
CHAIN_A_GRPC_PORT := 9090
CHAIN_A_P2P_PORT := 26656
CHAIN_B_RPC_PORT := 27657
CHAIN_B_GRPC_PORT := 9190
CHAIN_B_P2P_PORT := 26756

CHAIN_A_RPC_LADDR := tcp://0.0.0.0:$(CHAIN_A_RPC_PORT)
CHAIN_A_RPC_CLIENT := http://localhost:$(CHAIN_A_RPC_PORT)
CHAIN_A_GRPC_LADDR := 0.0.0.0:$(CHAIN_A_GRPC_PORT)
CHAIN_A_GRPC_CLIENT := localhost:$(CHAIN_A_GRPC_PORT)

CHAIN_B_RPC_LADDR := tcp://0.0.0.0:$(CHAIN_B_RPC_PORT)
CHAIN_B_RPC_CLIENT := http://localhost:$(CHAIN_B_RPC_PORT)
CHAIN_B_GRPC_LADDR := 0.0.0.0:$(CHAIN_B_GRPC_PORT)
CHAIN_B_GRPC_CLIENT := localhost:$(CHAIN_B_GRPC_PORT)

USER_A_KEY := usera
USER_B_KEY := userb
ATTACKER_B_KEY := attackerb
MOCK_DEX_B_KEY := mockDexB
VALIDATOR_A_KEY := validatora
VALIDATOR_B_KEY := validatorb

STAKE_DENOM := uatom
TOKEN_DENOM := token
DEFAULT_ACCOUNT_BALANCE := 1000000000$(STAKE_DENOM),1000000000$(TOKEN_DENOM)
VALIDATOR_STAKE_AMOUNT := 500000000$(STAKE_DENOM)

RLY_CONFIG_DIR := $(HOME)/.relayer
RLY_CONFIG_FILE := $(RLY_CONFIG_DIR)/config/config.yaml
RLY_PATH_AB_TRANSFER := a-b-transfer
RLY_PATH_ORDERED := a-b-ordered
RLY_PATH_UNORDERED := a-b-unordered

SCRIPTS := $(wildcard main/case*.go)
SHARED_GO_FILES := main/config.go main/utils.go



# --- Targets ---

.PHONY: all verify dependencies fetch-gaia-source build-gaiad-local chains setup-rly-config clean-relayer start-chains-local stop-chains-local start-chains setup-relayer restore-relayer-keys link-relayer-paths discover-channels validate run run-case1 run-case2 run-case3 run-case4 clean help

all: verify chains setup-rly-config start-chains-local setup-relayer validate run
	@echo "All setup and run steps completed. Chains are running locally using locally built gaiad."
	@echo "To stop chains, run 'make stop-chains-local' or 'make clean'."

verify: build-gaiad-local
	@echo "Verifying main dependencies..."
	@command -v $(GO) >/dev/null 2>&1 || (echo "Go is not installed or not in PATH. Please install Go."; exit 1)
	@echo "Go found: $(shell $(GO) version)"
	@command -v $(RLY) >/dev/null 2>&1 || (echo "Relayer binary ('$(RLY)') is not installed or not in PATH."; exit 1)
	@echo "Relayer 'rly' found: $(shell $(RLY) version)"
	@test -f "$(SIMD_BINARY_NAME)" || (echo "Locally built chain binary ('$(SIMD_BINARY_NAME)') is not found at $(SIMD_BINARY_NAME). Run 'make build-gaiad-local'."; exit 1)
	@echo "Locally built chain binary '$(SIMD_BINARY_NAME)' found: $(shell $(SIMD_BINARY_NAME) version)"
	@command -v jq >/dev/null 2>&1 || (echo "'jq' is not installed. Please install jq."; exit 1)
	@echo "'jq' found."
	@echo "Verification successful."

fetch-gaia-source:
	@echo "Fetching Gaia source code to $(GAIA_SOURCE_DIR) and checking out $(GAIA_CHECKOUT)..."
	@mkdir -p $(GAIA_SOURCE_DIR)
	@if [ ! -d "$(GAIA_SOURCE_DIR)/.git" ]; then \
		git clone --depth 1 --branch $(GAIA_CHECKOUT) $(GAIA_REPO_URL) $(GAIA_SOURCE_DIR); \
	else \
		(cd $(GAIA_SOURCE_DIR) && git fetch --depth 1 origin $(GAIA_CHECKOUT) && git checkout $(GAIA_CHECKOUT)); \
	fi

build-gaiad-local: fetch-gaia-source
	@echo "Building gaiad ($(GAIA_CHECKOUT)) locally for your architecture..."
	@echo "Target binary path: $(LOCAL_GAIAD_BUILT_PATH)"
	@echo "Detecting compatible Go version..."
	@if command -v go1.23.9 >/dev/null 2>&1; then \
		echo "Using go1.23.9 for compatibility"; \
		(cd $(GAIA_SOURCE_DIR) && LEDGER_ENABLED=false go1.23.9 build -mod=readonly -tags "netgo" -ldflags '-X github.com/cosmos/cosmos-sdk/version.Name=gaia -X github.com/cosmos/cosmos-sdk/version.AppName=gaiad -X github.com/cosmos/cosmos-sdk/version.Version=$(GAIA_CHECKOUT) -w -s' -trimpath -o build/ ./...); \
	elif command -v go1.23 >/dev/null 2>&1; then \
		echo "Using go1.23 for compatibility"; \
		(cd $(GAIA_SOURCE_DIR) && LEDGER_ENABLED=false go1.23 build -mod=readonly -tags "netgo" -ldflags '-X github.com/cosmos/cosmos-sdk/version.Name=gaia -X github.com/cosmos/cosmos-sdk/version.AppName=gaiad -X github.com/cosmos/cosmos-sdk/version.Version=$(GAIA_CHECKOUT) -w -s' -trimpath -o build/ ./...); \
	else \
		echo "Using default go with toolchain override"; \
		(cd $(GAIA_SOURCE_DIR) && LEDGER_ENABLED=false GOTOOLCHAIN=local go build -mod=readonly -tags "netgo" -ldflags '-X github.com/cosmos/cosmos-sdk/version.Name=gaia -X github.com/cosmos/cosmos-sdk/version.AppName=gaiad -X github.com/cosmos/cosmos-sdk/version.Version=$(GAIA_CHECKOUT) -w -s' -trimpath -o build/ ./... || echo "Build failed with version mismatch - this is expected with mixed Go versions"); \
	fi
	@test -f "$(LOCAL_GAIAD_BUILT_PATH)" || (echo "Build failed, $(LOCAL_GAIAD_BUILT_PATH) not found."; exit 1)
	@echo "gaiad binary built at $(LOCAL_GAIAD_BUILT_PATH)"



dependencies: build-gaiad-local
	@echo "Ensuring Go IBC relayer ('rly') is installed..."
	@command -v $(RLY) >/dev/null 2>&1 || { \
		echo "'rly' not found. Attempting to install..."; \
		cd $$(mktemp -d) && $(GO) install github.com/cosmos/relayer/v2/cmd/rly@latest && cd -; \
		command -v $(RLY) >/dev/null 2>&1 || (echo "Failed to install 'rly'. Please install it manually."; exit 1); \
	}
	@echo "'rly' is available: $(shell $(RLY) version)"
	@echo "Dependencies (local gaiad, rly) checked/installed."

chains: clean-chain-data build-gaiad-local
	@echo "Setting up chain data directories using $(SIMD_BINARY_NAME)..."
	@mkdir -p $(CHAIN_A_HOME) $(CHAIN_B_HOME) $(CONFIGS_DIR)

	@echo "Initializing Chain A ($(CHAIN_A_ID)) with $(SIMD_BINARY_NAME)..."
	@$(SIMD_BINARY_NAME) init $(VALIDATOR_A_KEY) --chain-id $(CHAIN_A_ID) --home $(CHAIN_A_HOME) --default-denom $(STAKE_DENOM)
	@echo "Initializing Chain B ($(CHAIN_B_ID)) with $(SIMD_BINARY_NAME)..."
	@$(SIMD_BINARY_NAME) init $(VALIDATOR_B_KEY) --chain-id $(CHAIN_B_ID) --home $(CHAIN_B_HOME) --default-denom $(STAKE_DENOM)

	@echo "Configuring Chain A (home: $(CHAIN_A_HOME)) for container..."
	@sed -i.bak 's/"stake"/"$(STAKE_DENOM)"/g' $(CHAIN_A_HOME)/config/genesis.json
	@sed -i.bak 's/enable = false/enable = true/g' $(CHAIN_A_HOME)/config/app.toml
	@sed -i.bak 's/swagger = false/swagger = true/g' $(CHAIN_A_HOME)/config/app.toml
	@sed -i.bak 's|^address = "tcp://localhost:1317"|address = "tcp://0.0.0.0:1317"|' $(CHAIN_A_HOME)/config/app.toml
	@sed -i.bak 's|^address = "localhost:9090"|address = "0.0.0.0:9090"|' $(CHAIN_A_HOME)/config/app.toml
	@sed -i.bak 's|^grpc-web.address = "localhost:9091"|grpc-web.address = "0.0.0.0:9091"|' $(CHAIN_A_HOME)/config/app.toml
	@sed -i.bak 's|^laddr = "tcp://127.0.0.1:26657"|laddr = "tcp://0.0.0.0:26657"|' $(CHAIN_A_HOME)/config/config.toml
	@sed -i.bak 's|^laddr = "tcp://0.0.0.0:26656"|laddr = "tcp://0.0.0.0:$(CHAIN_A_P2P_PORT)"|' $(CHAIN_A_HOME)/config/config.toml
	@sed -i.bak 's/^minimum-gas-prices = .*/minimum-gas-prices = "0$(STAKE_DENOM)"/' $(CHAIN_A_HOME)/config/app.toml

	@echo "Configuring Chain B (home: $(CHAIN_B_HOME)) for container..."
	@sed -i.bak 's/"stake"/"$(STAKE_DENOM)"/g' $(CHAIN_B_HOME)/config/genesis.json
	@sed -i.bak 's/enable = false/enable = true/g' $(CHAIN_B_HOME)/config/app.toml
	@sed -i.bak 's/swagger = false/swagger = true/g' $(CHAIN_B_HOME)/config/app.toml
	@sed -i.bak 's|^address = "tcp://localhost:1317"|address = "tcp://0.0.0.0:1317"|' $(CHAIN_B_HOME)/config/app.toml
	@sed -i.bak 's|^address = "localhost:9090"|address = "0.0.0.0:9090"|' $(CHAIN_B_HOME)/config/app.toml
	@sed -i.bak 's|^grpc-web.address = "localhost:9091"|grpc-web.address = "0.0.0.0:9091"|' $(CHAIN_B_HOME)/config/app.toml
	@sed -i.bak 's|^laddr = "tcp://127.0.0.1:26657"|laddr = "tcp://0.0.0.0:26657"|' $(CHAIN_B_HOME)/config/config.toml
	@sed -i.bak 's|^laddr = "tcp://0.0.0.0:26656"|laddr = "tcp://0.0.0.0:$(CHAIN_B_P2P_PORT)"|' $(CHAIN_B_HOME)/config/config.toml
	@sed -i.bak 's/^minimum-gas-prices = .*/minimum-gas-prices = "0$(STAKE_DENOM)"/' $(CHAIN_B_HOME)/config/app.toml

	@echo "Adding accounts to Chain A..."
	@$(SIMD_BINARY_NAME) keys add $(USER_A_KEY) --keyring-backend test --home $(CHAIN_A_HOME) --output json > /dev/null
	@$(SIMD_BINARY_NAME) keys add $(VALIDATOR_A_KEY) --keyring-backend test --home $(CHAIN_A_HOME) --output json | jq -r .mnemonic > $(CHAIN_A_HOME)/$(VALIDATOR_A_KEY).mnemonic

	@echo "Adding accounts to genesis for Chain A..."
	@$(SIMD_BINARY_NAME) genesis add-genesis-account $$($(SIMD_BINARY_NAME) keys show $(USER_A_KEY) -a --keyring-backend test --home $(CHAIN_A_HOME)) $(DEFAULT_ACCOUNT_BALANCE) --home $(CHAIN_A_HOME) --keyring-backend test
	@$(SIMD_BINARY_NAME) genesis add-genesis-account $$($(SIMD_BINARY_NAME) keys show $(VALIDATOR_A_KEY) -a --keyring-backend test --home $(CHAIN_A_HOME)) 1500000000$(STAKE_DENOM),1000000000$(TOKEN_DENOM),1000000000stake --home $(CHAIN_A_HOME) --keyring-backend test

	@echo "Generating gentx for Chain A validator $(VALIDATOR_A_KEY)..."
	@mkdir -p $(CHAIN_A_HOME)/config/gentx
	@$(SIMD_BINARY_NAME) genesis gentx $(VALIDATOR_A_KEY) $(VALIDATOR_STAKE_AMOUNT) --chain-id $(CHAIN_A_ID) --keyring-backend test --home $(CHAIN_A_HOME) --output-document $(CHAIN_A_HOME)/config/gentx/gentx-$(VALIDATOR_A_KEY).json

	@echo "Adding accounts to Chain B..."
	@$(SIMD_BINARY_NAME) keys add $(USER_B_KEY) --keyring-backend test --home $(CHAIN_B_HOME) --output json > /dev/null
	@$(SIMD_BINARY_NAME) keys add $(ATTACKER_B_KEY) --keyring-backend test --home $(CHAIN_B_HOME) --output json > /dev/null
	@$(SIMD_BINARY_NAME) keys add $(MOCK_DEX_B_KEY) --keyring-backend test --home $(CHAIN_B_HOME) --output json > /dev/null
	@$(SIMD_BINARY_NAME) keys add $(VALIDATOR_B_KEY) --keyring-backend test --home $(CHAIN_B_HOME) --output json | jq -r .mnemonic > $(CHAIN_B_HOME)/$(VALIDATOR_B_KEY).mnemonic

	@echo "Adding accounts to genesis for Chain B..."
	@$(SIMD_BINARY_NAME) genesis add-genesis-account $$($(SIMD_BINARY_NAME) keys show $(USER_B_KEY) -a --keyring-backend test --home $(CHAIN_B_HOME)) $(DEFAULT_ACCOUNT_BALANCE) --home $(CHAIN_B_HOME) --keyring-backend test
	@$(SIMD_BINARY_NAME) genesis add-genesis-account $$($(SIMD_BINARY_NAME) keys show $(ATTACKER_B_KEY) -a --keyring-backend test --home $(CHAIN_B_HOME)) $(DEFAULT_ACCOUNT_BALANCE) --home $(CHAIN_B_HOME) --keyring-backend test
	@$(SIMD_BINARY_NAME) genesis add-genesis-account $$($(SIMD_BINARY_NAME) keys show $(MOCK_DEX_B_KEY) -a --keyring-backend test --home $(CHAIN_B_HOME)) $(DEFAULT_ACCOUNT_BALANCE) --home $(CHAIN_B_HOME) --keyring-backend test
	@$(SIMD_BINARY_NAME) genesis add-genesis-account $$($(SIMD_BINARY_NAME) keys show $(VALIDATOR_B_KEY) -a --keyring-backend test --home $(CHAIN_B_HOME)) 1500000000$(STAKE_DENOM),1000000000$(TOKEN_DENOM),1000000000stake --home $(CHAIN_B_HOME) --keyring-backend test

	@echo "Generating gentx for Chain B validator $(VALIDATOR_B_KEY)..."
	@mkdir -p $(CHAIN_B_HOME)/config/gentx
	@$(SIMD_BINARY_NAME) genesis gentx $(VALIDATOR_B_KEY) $(VALIDATOR_STAKE_AMOUNT) --chain-id $(CHAIN_B_ID) --keyring-backend test --home $(CHAIN_B_HOME) --output-document $(CHAIN_B_HOME)/config/gentx/gentx-$(VALIDATOR_B_KEY).json

	@echo "Collecting gentxs for Chain A..."
	@$(SIMD_BINARY_NAME) genesis collect-gentxs --home $(CHAIN_A_HOME)
	@echo "Collecting gentxs for Chain B..."
	@$(SIMD_BINARY_NAME) genesis collect-gentxs --home $(CHAIN_B_HOME)

	@echo "Chain data initialization complete in $(CHAIN_A_HOME) and $(CHAIN_B_HOME)"

clean-relayer:
	@echo "Cleaning relayer configuration..."
	@command -v $(RLY) >/dev/null 2>&1 || (echo "Relayer 'rly' not found. Run 'make dependencies'."; exit 1)
	@if [ -f "$(RLY_CONFIG_FILE)" ]; then \
		echo "Removing existing chains, paths, and keys..."; \
		echo "Y" | $(RLY) keys delete chain-a $(VALIDATOR_A_KEY) --home $(RLY_CONFIG_DIR) 2>/dev/null || true; \
		echo "Y" | $(RLY) keys delete chain-b $(VALIDATOR_B_KEY) --home $(RLY_CONFIG_DIR) 2>/dev/null || true; \
		$(RLY) chains delete chain-a --home $(RLY_CONFIG_DIR) 2>/dev/null || true; \
		$(RLY) chains delete chain-b --home $(RLY_CONFIG_DIR) 2>/dev/null || true; \
		$(RLY) paths delete $(RLY_PATH_AB_TRANSFER) --home $(RLY_CONFIG_DIR) 2>/dev/null || true; \
		$(RLY) paths delete $(RLY_PATH_ORDERED) --home $(RLY_CONFIG_DIR) 2>/dev/null || true; \
		$(RLY) paths delete $(RLY_PATH_UNORDERED) --home $(RLY_CONFIG_DIR) 2>/dev/null || true; \
	fi
	@echo "Relayer configuration cleaned."

setup-rly-config: clean-relayer
	@echo "Setting up relayer configuration..."
	@command -v $(RLY) >/dev/null 2>&1 || (echo "Relayer 'rly' not found. Run 'make dependencies'."; exit 1)
	@if [ ! -f "$(RLY_CONFIG_FILE)" ]; then $(RLY) config init --home $(RLY_CONFIG_DIR); else echo "Using existing relayer config at $(RLY_CONFIG_FILE)"; fi
	@mkdir -p $(CONFIGS_DIR)
	@printf '{\n  "type": "cosmos",\n  "value": {\n    "key": "$(VALIDATOR_A_KEY)",\n    "chain-id": "$(CHAIN_A_ID)",\n    "rpc-addr": "$(strip $(CHAIN_A_RPC_CLIENT))",\n    "grpc-addr": "$(strip $(CHAIN_A_GRPC_CLIENT))",\n    "account-prefix": "cosmos",\n    "keyring-backend": "test",\n    "gas-adjustment": 3.0,\n    "gas-prices": "1.5$(STAKE_DENOM)",\n    "debug": false,\n    "timeout": "20s",\n    "output-format": "json",\n    "sign-mode": "direct"\n  }\n}' > $(CONFIGS_DIR)/chain-a.json
	@printf '{\n  "type": "cosmos",\n  "value": {\n    "key": "$(VALIDATOR_B_KEY)",\n    "chain-id": "$(CHAIN_B_ID)",\n    "rpc-addr": "$(strip $(CHAIN_B_RPC_CLIENT))",\n    "grpc-addr": "$(strip $(CHAIN_B_GRPC_CLIENT))",\n    "account-prefix": "cosmos",\n    "keyring-backend": "test",\n    "gas-adjustment": 3.0,\n    "gas-prices": "1.5$(STAKE_DENOM)",\n    "debug": false,\n    "timeout": "20s",\n    "output-format": "json",\n    "sign-mode": "direct"\n  }\n}' > $(CONFIGS_DIR)/chain-b.json
	@echo "Adding chains to relayer configuration..."
	@$(RLY) chains add --file $(CONFIGS_DIR)/chain-a.json --home $(RLY_CONFIG_DIR)
	@$(RLY) chains add --file $(CONFIGS_DIR)/chain-b.json --home $(RLY_CONFIG_DIR)
	@echo "Creating relayer paths..."
	@$(RLY) paths new $(CHAIN_A_ID) $(CHAIN_B_ID) $(RLY_PATH_AB_TRANSFER) --src-port transfer --dst-port transfer --order ordered --version ics20-1 --home $(RLY_CONFIG_DIR)
	@$(RLY) paths new $(CHAIN_A_ID) $(CHAIN_B_ID) $(RLY_PATH_ORDERED) --src-port transfer --dst-port transfer --order ordered --version ics20-1 --home $(RLY_CONFIG_DIR)
	@$(RLY) paths new $(CHAIN_A_ID) $(CHAIN_B_ID) $(RLY_PATH_UNORDERED) --src-port transfer --dst-port transfer --order unordered --version ics20-1 --home $(RLY_CONFIG_DIR)
	@echo "MANUAL STEP: Restore relayer keys for $(CHAIN_A_ID) ($(VALIDATOR_A_KEY)) and $(CHAIN_B_ID) ($(VALIDATOR_B_KEY)) using mnemonics from $(CHAIN_A_HOME)/$(VALIDATOR_A_KEY).mnemonic and $(CHAIN_B_HOME)/$(VALIDATOR_B_KEY).mnemonic"
	@echo "e.g., rly keys restore $(CHAIN_A_ID) $(VALIDATOR_A_KEY) \"$$(cat $(CHAIN_A_HOME)/$(VALIDATOR_A_KEY).mnemonic)\" --home $(RLY_CONFIG_DIR)"
	@echo "Relayer configuration complete. Next: 1. Start chains ('make start-chains-local') 2. Setup relayer ('make setup-relayer') 3. Run tests ('make run')"

start-chains:
	@echo "Manual start commands (run these in separate terminals):"
	@echo "  $(SIMD_BINARY_NAME) start --home $(CHAIN_A_HOME) --rpc.laddr $(CHAIN_A_RPC_LADDR) --grpc.address $(CHAIN_A_GRPC_LADDR) --log_level info"
	@echo "  $(SIMD_BINARY_NAME) start --home $(CHAIN_B_HOME) --rpc.laddr $(CHAIN_B_RPC_LADDR) --grpc.address $(CHAIN_B_GRPC_LADDR) --log_level info"

start-chains-local: stop-chains-local dependencies
	@echo "Starting chains locally using $(SIMD_BINARY_NAME)..."
	@echo "Starting Chain A in background..."
	@nohup $(SIMD_BINARY_NAME) start --home $(CHAIN_A_HOME) --rpc.laddr $(CHAIN_A_RPC_LADDR) --grpc.address $(CHAIN_A_GRPC_LADDR) --p2p.laddr tcp://0.0.0.0:$(CHAIN_A_P2P_PORT) --log_level info > chain-a.log 2>&1 & echo $$! > chain-a.pid
	@echo "Starting Chain B in background..."
	@nohup $(SIMD_BINARY_NAME) start --home $(CHAIN_B_HOME) --rpc.laddr $(CHAIN_B_RPC_LADDR) --grpc.address $(CHAIN_B_GRPC_LADDR) --p2p.laddr tcp://0.0.0.0:$(CHAIN_B_P2P_PORT) --log_level info > chain-b.log 2>&1 & echo $$! > chain-b.pid
	@echo "Waiting for chains to initialize (60 seconds)... View logs with 'tail -f chain-a.log' or 'tail -f chain-b.log'"
	@sleep 60
	@echo "Checking chain statuses and waiting for sync..."
	@echo "Waiting for Chain A to be accessible and synced..."
	@timeout 120s bash -c 'while ! $(SIMD_BINARY_NAME) status --node $(CHAIN_A_RPC_CLIENT) > /dev/null 2>&1; do echo "Chain A not yet accessible, waiting..."; sleep 5; done'
	@timeout 120s bash -c 'while ! $(SIMD_BINARY_NAME) status --node $(CHAIN_A_RPC_CLIENT) 2>/dev/null | jq -r .sync_info.catching_up | grep -q false; do echo "Chain A still syncing, waiting..."; sleep 5; done' || echo "Chain A sync check timed out, continuing anyway"
	@echo "Waiting for Chain B to be accessible and synced..."
	@timeout 120s bash -c 'while ! $(SIMD_BINARY_NAME) status --node $(CHAIN_B_RPC_CLIENT) > /dev/null 2>&1; do echo "Chain B not yet accessible, waiting..."; sleep 5; done'
	@timeout 120s bash -c 'while ! $(SIMD_BINARY_NAME) status --node $(CHAIN_B_RPC_CLIENT) 2>/dev/null | jq -r .sync_info.catching_up | grep -q false; do echo "Chain B still syncing, waiting..."; sleep 5; done' || echo "Chain B sync check timed out, continuing anyway"
	@if ! timeout 10s $(SIMD_BINARY_NAME) status --node $(CHAIN_A_RPC_CLIENT) > /dev/null 2>&1; then echo "Error: Chain A does not seem to be running or accessible at $(CHAIN_A_RPC_CLIENT). Check chain-a.log."; exit 1; fi
	@if ! timeout 10s $(SIMD_BINARY_NAME) status --node $(CHAIN_B_RPC_CLIENT) > /dev/null 2>&1; then echo "Error: Chain B does not seem to be running or accessible at $(CHAIN_B_RPC_CLIENT). Check chain-b.log."; exit 1; fi
	@echo "Chains are running locally. PIDs saved in chain-a.pid and chain-b.pid"

stop-chains-local:
	@echo "Stopping local chains..."
	@if [ -f chain-a.pid ]; then kill $$(cat chain-a.pid) 2>/dev/null || true; rm -f chain-a.pid; fi
	@if [ -f chain-b.pid ]; then kill $$(cat chain-b.pid) 2>/dev/null || true; rm -f chain-b.pid; fi
	@pkill -f "$(SIMD_BINARY_NAME).*start.*--home.*chain-[ab]-data" || true
	@echo "Local chains stopped."

setup-relayer: restore-relayer-keys link-relayer-paths discover-channels
	@echo "Relayer setup complete with automated key restoration, path linking, and channel discovery."

validate:
	@echo "Running setup validation..."
	@if [ ! -f "$(CONFIGS_DIR)/channels.env" ]; then echo "Error: Channel mappings not found. Run 'make setup-relayer' first."; exit 1; fi
	@set -a && source $(CONFIGS_DIR)/channels.env && set +a && \
	CHAIN_A_HOME_ENV=$(CHAIN_A_HOME) \
	CHAIN_B_HOME_ENV=$(CHAIN_B_HOME) \
	CHAIN_A_RPC_ENV=$(CHAIN_A_RPC_CLIENT) \
	CHAIN_B_RPC_ENV=$(CHAIN_B_RPC_CLIENT) \
	CHAIN_A_ID_ENV=$(CHAIN_A_ID) \
	CHAIN_B_ID_ENV=$(CHAIN_B_ID) \
	RLY_CONFIG_FILE_ENV=$(RLY_CONFIG_DIR) \
	SIMD_BINARY_ENV=$(SIMD_BINARY_NAME) \
	RLY_BINARY_ENV=$(RLY) \
	RLY_PATH_TRANSFER_ENV=$(RLY_PATH_AB_TRANSFER) \
	RLY_PATH_ORDERED_ENV=$(RLY_PATH_ORDERED) \
	RLY_PATH_UNORDERED_ENV=$(RLY_PATH_UNORDERED) \
	TRANSFER_CHANNEL_A_ENV=$$TRANSFER_CHANNEL_A \
	TRANSFER_CHANNEL_B_ENV=$$TRANSFER_CHANNEL_B \
	ORDERED_CHANNEL_A_ENV=$$ORDERED_CHANNEL_A \
	ORDERED_CHANNEL_B_ENV=$$ORDERED_CHANNEL_B \
	UNORDERED_CHANNEL_A_ENV=$$UNORDERED_CHANNEL_A \
	UNORDERED_CHANNEL_B_ENV=$$UNORDERED_CHANNEL_B \
	$(GO) run $(SHARED_GO_FILES) main/validate_setup.go
	@echo "Validation complete. The testbed is ready for experiments."

restore-relayer-keys:
	@echo "Restoring relayer keys automatically using saved mnemonics..."
	@if [ ! -f "$(CHAIN_A_HOME)/$(VALIDATOR_A_KEY).mnemonic" ]; then echo "Error: $(CHAIN_A_HOME)/$(VALIDATOR_A_KEY).mnemonic not found. Run 'make chains' first."; exit 1; fi
	@if [ ! -f "$(CHAIN_B_HOME)/$(VALIDATOR_B_KEY).mnemonic" ]; then echo "Error: $(CHAIN_B_HOME)/$(VALIDATOR_B_KEY).mnemonic not found. Run 'make chains' first."; exit 1; fi
	@echo "Removing existing keys and restoring fresh for chain-a ($(CHAIN_A_ID))..."
	@echo "Y" | $(RLY) keys delete chain-a $(VALIDATOR_A_KEY) --home $(RLY_CONFIG_DIR) 2>/dev/null || true
	@$(RLY) keys restore chain-a $(VALIDATOR_A_KEY) "$$(cat $(CHAIN_A_HOME)/$(VALIDATOR_A_KEY).mnemonic)" --home $(RLY_CONFIG_DIR) --coin-type 118
	@echo "Removing existing keys and restoring fresh for chain-b ($(CHAIN_B_ID))..."
	@echo "Y" | $(RLY) keys delete chain-b $(VALIDATOR_B_KEY) --home $(RLY_CONFIG_DIR) 2>/dev/null || true
	@$(RLY) keys restore chain-b $(VALIDATOR_B_KEY) "$$(cat $(CHAIN_B_HOME)/$(VALIDATOR_B_KEY).mnemonic)" --home $(RLY_CONFIG_DIR) --coin-type 118
	@echo "Relayer keys restored successfully with fresh keys."

link-relayer-paths:
	@echo "Linking relayer paths automatically..."
	@echo "Waiting for chains to be fully ready..."
	@sleep 30
	@echo "Checking chain sync status before linking..."
	@timeout 30s bash -c 'while ! $(SIMD_BINARY_NAME) status --node $(CHAIN_A_RPC_CLIENT) 2>/dev/null | jq -r .sync_info.catching_up | grep -q false; do echo "Waiting for Chain A to sync..."; sleep 5; done' || echo "Chain A sync check timed out, continuing anyway"
	@timeout 30s bash -c 'while ! $(SIMD_BINARY_NAME) status --node $(CHAIN_B_RPC_CLIENT) 2>/dev/null | jq -r .sync_info.catching_up | grep -q false; do echo "Waiting for Chain B to sync..."; sleep 5; done' || echo "Chain B sync check timed out, continuing anyway"
	@echo "Verifying relayer can connect to both chains..."
	@echo "Testing relayer connection to chain-a..."
	@$(RLY) query header chain-a --home $(RLY_CONFIG_DIR) > /dev/null || (echo "ERROR: Relayer cannot connect to chain-a. Check chain status and relayer keys."; exit 1)
	@echo "Testing relayer connection to chain-b..."
	@$(RLY) query header chain-b --home $(RLY_CONFIG_DIR) > /dev/null || (echo "ERROR: Relayer cannot connect to chain-b. Check chain status and relayer keys."; exit 1)
	@echo "Both chains accessible. Starting path linking with debug output..."
	@echo "Linking $(RLY_PATH_AB_TRANSFER)..."
	@$(RLY) tx link $(RLY_PATH_AB_TRANSFER) --home $(RLY_CONFIG_DIR) --src-port transfer --dst-port transfer --debug || (echo "ERROR: Failed to link $(RLY_PATH_AB_TRANSFER). Retrying once..."; sleep 10; $(RLY) tx link $(RLY_PATH_AB_TRANSFER) --home $(RLY_CONFIG_DIR) --src-port transfer --dst-port transfer --debug || echo "WARNING: $(RLY_PATH_AB_TRANSFER) linking failed after retry")
	@echo "Linking $(RLY_PATH_ORDERED)..."
	@$(RLY) tx link $(RLY_PATH_ORDERED) --home $(RLY_CONFIG_DIR) --src-port transfer --dst-port transfer --debug || (echo "ERROR: Failed to link $(RLY_PATH_ORDERED). Retrying once..."; sleep 10; $(RLY) tx link $(RLY_PATH_ORDERED) --home $(RLY_CONFIG_DIR) --src-port transfer --dst-port transfer --debug || echo "WARNING: $(RLY_PATH_ORDERED) linking failed after retry")
	@echo "Linking $(RLY_PATH_UNORDERED)..."
	@$(RLY) tx link $(RLY_PATH_UNORDERED) --home $(RLY_CONFIG_DIR) --src-port transfer --dst-port transfer --debug || (echo "ERROR: Failed to link $(RLY_PATH_UNORDERED). Retrying once..."; sleep 10; $(RLY) tx link $(RLY_PATH_UNORDERED) --home $(RLY_CONFIG_DIR) --src-port transfer --dst-port transfer --debug || echo "WARNING: $(RLY_PATH_UNORDERED) linking failed after retry")
	@echo "Verifying path status..."
	@$(RLY) paths list --home $(RLY_CONFIG_DIR) || echo "Could not list paths"
	@echo "All relayer paths linking completed."

discover-channels:
	@echo "Discovering channel IDs and creating channel mapping file..."
	@mkdir -p $(CONFIGS_DIR)
	@echo "# Auto-generated channel mappings" > $(CONFIGS_DIR)/channels.env
	@echo "# Transfer path channels" >> $(CONFIGS_DIR)/channels.env
	@TRANSFER_A=$$($(RLY) q channels chain-a --home $(RLY_CONFIG_DIR) 2>/dev/null | head -1 | jq -r '.channel_id // "channel-0"') && echo "TRANSFER_CHANNEL_A=$$TRANSFER_A" >> $(CONFIGS_DIR)/channels.env
	@TRANSFER_B=$$($(RLY) q channels chain-b --home $(RLY_CONFIG_DIR) 2>/dev/null | head -1 | jq -r '.channel_id // "channel-0"') && echo "TRANSFER_CHANNEL_B=$$TRANSFER_B" >> $(CONFIGS_DIR)/channels.env
	@echo "# Ordered path channels" >> $(CONFIGS_DIR)/channels.env
	@echo "ORDERED_CHANNEL_A=channel-0" >> $(CONFIGS_DIR)/channels.env
	@echo "ORDERED_CHANNEL_B=channel-0" >> $(CONFIGS_DIR)/channels.env
	@echo "# Unordered path channels" >> $(CONFIGS_DIR)/channels.env
	@echo "UNORDERED_CHANNEL_A=channel-0" >> $(CONFIGS_DIR)/channels.env
	@echo "UNORDERED_CHANNEL_B=channel-0" >> $(CONFIGS_DIR)/channels.env
	@echo "Channel mappings saved to $(CONFIGS_DIR)/channels.env:"
	@cat $(CONFIGS_DIR)/channels.env

run: run-case1 run-case2 run-case3 run-case4
	@echo ""
	@echo "=============================================="
	@echo "  All IBC Front-Running Test Cases Completed"
	@echo "=============================================="
	@echo ""
	@echo " To see results, look for these messages in the logs above:"
	@echo "   • SUCCESS: Front-running attack succeeded"
	@echo "   • FAILURE: Front-running attack failed"
	@echo ""
	@echo " For detailed analysis, examine:"
	@echo "   • Transaction hashes and block heights"
	@echo "   • Block timing differences"
	@echo "   • Gas fees and transaction ordering"
	@echo ""
	@echo " Tip: Run individual cases with 'make run-case1', 'make run-case2', etc."
	@echo "=============================================="

run-case1:
	@echo "=== Running Case 1: Relayer Front-Running ==="
	@echo "Ensure chains are running locally and relayer paths are established."
	@if [ ! -f "$(CONFIGS_DIR)/channels.env" ]; then echo "Error: Channel mappings not found. Run 'make setup-relayer' first."; exit 1; fi
	@echo "--- Running main/case1_relayer_frontrun.go (with $(SHARED_GO_FILES)) ---"
	@set -a && source $(CONFIGS_DIR)/channels.env && set +a && \
	CHAIN_A_HOME_ENV=$(CHAIN_A_HOME) \
	CHAIN_B_HOME_ENV=$(CHAIN_B_HOME) \
	CHAIN_A_RPC_ENV=$(CHAIN_A_RPC_CLIENT) \
	CHAIN_B_RPC_ENV=$(CHAIN_B_RPC_CLIENT) \
	CHAIN_A_ID_ENV=$(CHAIN_A_ID) \
	CHAIN_B_ID_ENV=$(CHAIN_B_ID) \
	RLY_CONFIG_FILE_ENV=$(RLY_CONFIG_DIR) \
	SIMD_BINARY_ENV=$(SIMD_BINARY_NAME) \
	RLY_BINARY_ENV=$(RLY) \
	RLY_PATH_TRANSFER_ENV=$(RLY_PATH_AB_TRANSFER) \
	RLY_PATH_ORDERED_ENV=$(RLY_PATH_ORDERED) \
	RLY_PATH_UNORDERED_ENV=$(RLY_PATH_UNORDERED) \
	TRANSFER_CHANNEL_A_ENV=$$TRANSFER_CHANNEL_A \
	TRANSFER_CHANNEL_B_ENV=$$TRANSFER_CHANNEL_B \
	ORDERED_CHANNEL_A_ENV=$$ORDERED_CHANNEL_A \
	ORDERED_CHANNEL_B_ENV=$$ORDERED_CHANNEL_B \
	UNORDERED_CHANNEL_A_ENV=$$UNORDERED_CHANNEL_A \
	UNORDERED_CHANNEL_B_ENV=$$UNORDERED_CHANNEL_B \
	$(GO) run $(SHARED_GO_FILES) main/case1_relayer_frontrun.go
	@echo "--- Finished Case 1 ---"

run-case2:
	@echo "=== Running Case 2: Validator Fee Front-Running ==="
	@echo "Ensure chains are running locally and relayer paths are established."
	@if [ ! -f "$(CONFIGS_DIR)/channels.env" ]; then echo "Error: Channel mappings not found. Run 'make setup-relayer' first."; exit 1; fi
	@echo "--- Running main/case2_validator_fee_frontrun.go (with $(SHARED_GO_FILES)) ---"
	@set -a && source $(CONFIGS_DIR)/channels.env && set +a && \
	CHAIN_A_HOME_ENV=$(CHAIN_A_HOME) \
	CHAIN_B_HOME_ENV=$(CHAIN_B_HOME) \
	CHAIN_A_RPC_ENV=$(CHAIN_A_RPC_CLIENT) \
	CHAIN_B_RPC_ENV=$(CHAIN_B_RPC_CLIENT) \
	CHAIN_A_ID_ENV=$(CHAIN_A_ID) \
	CHAIN_B_ID_ENV=$(CHAIN_B_ID) \
	RLY_CONFIG_FILE_ENV=$(RLY_CONFIG_DIR) \
	SIMD_BINARY_ENV=$(SIMD_BINARY_NAME) \
	RLY_BINARY_ENV=$(RLY) \
	RLY_PATH_TRANSFER_ENV=$(RLY_PATH_AB_TRANSFER) \
	RLY_PATH_ORDERED_ENV=$(RLY_PATH_ORDERED) \
	RLY_PATH_UNORDERED_ENV=$(RLY_PATH_UNORDERED) \
	TRANSFER_CHANNEL_A_ENV=$$TRANSFER_CHANNEL_A \
	TRANSFER_CHANNEL_B_ENV=$$TRANSFER_CHANNEL_B \
	ORDERED_CHANNEL_A_ENV=$$ORDERED_CHANNEL_A \
	ORDERED_CHANNEL_B_ENV=$$ORDERED_CHANNEL_B \
	UNORDERED_CHANNEL_A_ENV=$$UNORDERED_CHANNEL_A \
	UNORDERED_CHANNEL_B_ENV=$$UNORDERED_CHANNEL_B \
	$(GO) run $(SHARED_GO_FILES) main/case2_validator_fee_frontrun.go || echo "Case 2 failed or had errors."
	@echo "--- Finished Case 2 ---"

run-case3:
	@echo "=== Running Case 3: Cross-Chain MEV ==="
	@echo "Ensure chains are running locally and relayer paths are established."
	@if [ ! -f "$(CONFIGS_DIR)/channels.env" ]; then echo "Error: Channel mappings not found. Run 'make setup-relayer' first."; exit 1; fi
	@echo "--- Running main/case3_cross_chain_mev_dex.go (with $(SHARED_GO_FILES)) ---"
	@set -a && source $(CONFIGS_DIR)/channels.env && set +a && \
	CHAIN_A_HOME_ENV=$(CHAIN_A_HOME) \
	CHAIN_B_HOME_ENV=$(CHAIN_B_HOME) \
	CHAIN_A_RPC_ENV=$(CHAIN_A_RPC_CLIENT) \
	CHAIN_B_RPC_ENV=$(CHAIN_B_RPC_CLIENT) \
	CHAIN_A_ID_ENV=$(CHAIN_A_ID) \
	CHAIN_B_ID_ENV=$(CHAIN_B_ID) \
	RLY_CONFIG_FILE_ENV=$(RLY_CONFIG_DIR) \
	SIMD_BINARY_ENV=$(SIMD_BINARY_NAME) \
	RLY_BINARY_ENV=$(RLY) \
	RLY_PATH_TRANSFER_ENV=$(RLY_PATH_AB_TRANSFER) \
	RLY_PATH_ORDERED_ENV=$(RLY_PATH_ORDERED) \
	RLY_PATH_UNORDERED_ENV=$(RLY_PATH_UNORDERED) \
	TRANSFER_CHANNEL_A_ENV=$$TRANSFER_CHANNEL_A \
	TRANSFER_CHANNEL_B_ENV=$$TRANSFER_CHANNEL_B \
	ORDERED_CHANNEL_A_ENV=$$ORDERED_CHANNEL_A \
	ORDERED_CHANNEL_B_ENV=$$ORDERED_CHANNEL_B \
	UNORDERED_CHANNEL_A_ENV=$$UNORDERED_CHANNEL_A \
	UNORDERED_CHANNEL_B_ENV=$$UNORDERED_CHANNEL_B \
	$(GO) run $(SHARED_GO_FILES) main/case3_cross_chain_mev_dex.go
	@echo "--- Finished Case 3 ---"

run-case4:
	@echo "=== Running Case 4: Channel Ordering Impact ==="
	@echo "Ensure chains are running locally and relayer paths are established."
	@if [ ! -f "$(CONFIGS_DIR)/channels.env" ]; then echo "Error: Channel mappings not found. Run 'make setup-relayer' first."; exit 1; fi
	@echo "--- Running main/case4_channel_order_frontrun.go (with $(SHARED_GO_FILES)) ---"
	@set -a && source $(CONFIGS_DIR)/channels.env && set +a && \
	CHAIN_A_HOME_ENV=$(CHAIN_A_HOME) \
	CHAIN_B_HOME_ENV=$(CHAIN_B_HOME) \
	CHAIN_A_RPC_ENV=$(CHAIN_A_RPC_CLIENT) \
	CHAIN_B_RPC_ENV=$(CHAIN_B_RPC_CLIENT) \
	CHAIN_A_ID_ENV=$(CHAIN_A_ID) \
	CHAIN_B_ID_ENV=$(CHAIN_B_ID) \
	RLY_CONFIG_FILE_ENV=$(RLY_CONFIG_DIR) \
	SIMD_BINARY_ENV=$(SIMD_BINARY_NAME) \
	RLY_BINARY_ENV=$(RLY) \
	RLY_PATH_TRANSFER_ENV=$(RLY_PATH_AB_TRANSFER) \
	RLY_PATH_ORDERED_ENV=$(RLY_PATH_ORDERED) \
	RLY_PATH_UNORDERED_ENV=$(RLY_PATH_UNORDERED) \
	TRANSFER_CHANNEL_A_ENV=$$TRANSFER_CHANNEL_A \
	TRANSFER_CHANNEL_B_ENV=$$TRANSFER_CHANNEL_B \
	ORDERED_CHANNEL_A_ENV=$$ORDERED_CHANNEL_A \
	ORDERED_CHANNEL_B_ENV=$$ORDERED_CHANNEL_B \
	UNORDERED_CHANNEL_A_ENV=$$UNORDERED_CHANNEL_A \
	UNORDERED_CHANNEL_B_ENV=$$UNORDERED_CHANNEL_B \
	$(GO) run $(SHARED_GO_FILES) main/case4_channel_order_frontrun.go || echo "Case 4 failed or had errors."
	@echo "--- Finished Case 4 ---"

clean-chain-data:
	@echo "Cleaning up chain data..."
	@rm -rf $(CHAIN_A_HOME) $(CHAIN_B_HOME) $(CONFIGS_DIR) $(TMP_BUILD_DIR)
	@rm -f chain-a.log chain-b.log chain-a.pid chain-b.pid

clean: stop-chains-local clean-relayer
	@echo "Cleaning up all chain data and configurations..."
	@rm -rf $(CHAIN_A_HOME) $(CHAIN_B_HOME) $(CONFIGS_DIR) $(TMP_BUILD_DIR)
	@rm -f chain-a.log chain-b.log chain-a.pid chain-b.pid
	@rm -rf $(GAIA_SOURCE_DIR)
	@rm -rf $(RLY_CONFIG_DIR) || true
	@echo "Complete cleanup finished."



help:
	@echo " IBC Front-Running Testbed - Available Commands:"
	@echo ""
	@echo " MAIN TARGETS:"
	@echo "  all                 - Complete setup: builds, starts chains, configures relayer, and runs all tests"
	@echo "  run                 - Run all 4 front-running test cases (chains must be running)"
	@echo "  validate            - Validate complete testbed setup with a basic IBC test"
	@echo "  clean               - Complete cleanup: stops chains, removes all data and configurations"
	@echo ""
	@echo " INDIVIDUAL TEST CASES (for debugging):"
	@echo "  run-case1           - Relayer Front-Running (timing-based attack)"
	@echo "  run-case2           - Validator Fee Front-Running (fee-based priority attack)"
	@echo "  run-case3           - Cross-Chain MEV Sandwich Attack (Real DEX simulation)"
	@echo "  run-case4           - Channel Ordering Impact (ordered vs unordered channels)"
	@echo ""
	@echo " SETUP COMPONENTS:"
	@echo "  verify              - Check dependencies (Go, jq, etc.)"
	@echo "  dependencies        - Build local gaiad and install relayer"
	@echo "  chains              - Initialize blockchain data for both chains"
	@echo "  start-chains-local  - Start chains in background (view logs: tail -f chain-*.log)"
	@echo "  setup-relayer       - Configure IBC relayer (keys, paths, channels)"
	@echo "  stop-chains-local   - Stop running chains"
	@echo ""
	@echo " CLEANUP:"
	@echo "  clean-chain-data    - Remove chain data only"
	@echo "  clean-relayer       - Remove relayer configuration only"
	@echo ""
	@echo " Tip: After running tests, generate updated visualizations with:
	@echo "   cd visualizations && python3 ibc_diagrams.py"
	@echo ""
	@echo "Configuration Variables (can be overridden, e.g., 'make chains STAKE_DENOM=uphoton GAIA_CHECKOUT=v17.0.0'):"
	@echo "  STAKE_DENOM         - Staking/fee denom for chains (default: uatom)"
	@echo "  TOKEN_DENOM         - IBC transfer token denom (default: token)"
	@echo "  GAIA_CHECKOUT       - Tag/branch of Gaia to build (default: $(GAIA_CHECKOUT))"
	@echo "  ... and others defined at the top of the Makefile."
