# IBC Front-Running Vulnerability Test Suite

This repository contains a suite of Go scripts and a `Makefile` designed to simulate and test potential front-running vulnerabilities in a local Inter-Blockchain Communication (IBC) testbed environment. The scripts interact with two locally running Cosmos SDK-based blockchain chains (Chain A and Chain B) and the Go IBC relayer. Configuration is primarily managed via environment variables set by the `Makefile` and loaded by a shared `config.go` file.

## Overview

The primary goal is to explore scenarios where an attacker might exploit the timing of IBC packet relay and transaction processing to gain an unfair advantage. These tests cover:

1.  **Relayer Front-Running (Packet Delay/Manipulation)**
2.  **Validator Front-Running (Fee Manipulation)**
3.  **Cross-Chain MEV/DeFi Interaction (Mocked)**
4.  **Ordered vs. Unordered Channels (Impact on Relayer Front-Running)**

## Prerequisites

Before running these test scripts, ensure the following are installed and configured on your system:

1.  **Go:** Version 1.18+ recommended. (`go version`)
2.  **GNU Make:** For using the `Makefile`. (`make --version`)
3.  **jq:** A command-line JSON processor, used by the `Makefile`. (`jq --version`)
4.  **Cosmos SDK `simd` binary:**
    *   This is the standard simulation application binary from the Cosmos SDK.
    *   Install it by cloning the Cosmos SDK repository and running `make install`:
        ```bash
        git clone https://github.com/cosmos/cosmos-sdk
        cd cosmos-sdk
        # git checkout <latest-stable-tag> # Optional: checkout a specific version
        make install
        cd ..
        simd version # Verify installation
        ```
    *   Ensure `simd` (and your `$GOPATH/bin` or `$GOBIN`) is in your system `PATH`.
5.  **Go IBC Relayer (`rly`):**
    *   The `Makefile` can attempt to install this if not found (`make dependencies`).
    *   Ensure `rly` (and your `$GOPATH/bin` or `$GOBIN`) is in your system `PATH`.
    *   The relayer's main configuration directory is typically `~/.relayer/`.

## Setup and Execution using Makefile

The `Makefile` automates most of the setup and execution.

1.  **Clone the Repository:**
    ```bash
    git clone <repository-url>
    cd <repository-name>
    ```

2.  **Verify Dependencies & Environment:**
    Run `make verify` to check if `go`, `rly`, `simd`, and `jq` are installed and accessible.
    ```bash
    make verify
    ```

3.  **Install Dependencies (if needed):**
    If `rly` is not found, `make dependencies` will attempt to install it. It will also guide you if `simd` is missing.
    ```bash
    make dependencies
    ```

4.  **Initialize Go Module (if not already done):**
    ```bash
    go mod init ibc_frontrun_tests # Or your preferred module name
    go mod tidy
    ```

5.  **Set up Local Chains:**
    This command will create local data directories (`./chain-a-data`, `./chain-b-data`), initialize two `simd` chains, configure them, create necessary accounts, and fund them in genesis.
    ```bash
    make chains
    ```
    *   **Important:** The `Makefile` sets chain data paths, RPC/gRPC endpoints, and chain IDs. These are passed to the Go scripts via environment variables.

6.  **Configure IBC Relayer (`rly`):**
    The `Makefile` provides a basic setup. **Manual steps are often required for full relayer functionality.**
    ```bash
    make setup-rly-config
    ```
    This target will:
    *   Initialize `rly` config if not present.
    *   Add the local chains (Chain A & B) to the `rly` config using temporary JSON files.
    *   Create basic IBC paths (`a-b-transfer`, `a-b-ordered`, `a-b-unordered`).
    *   **Manual Steps Required After `make setup-rly-config`:**
        *   **Restore Relayer Keys:** You MUST restore or add keys for the relayer to use on both Chain A and Chain B. The `Makefile` will remind you of this.
            ```bash
            rly keys restore chain-a <relayer-key-name-A> "<mnemonic_for_key_A>"
            rly keys restore chain-b <relayer-key-name-B> "<mnemonic_for_key_B>"
            ```
        *   **Fund Relayer Accounts:** Ensure the relayer accounts have tokens for fees on both chains.
        *   **Link Paths:** After chains are running and keys are set, link the IBC paths to create channels:
            ```bash
            rly tx link a-b-transfer
            rly tx link a-b-ordered  # For Case 4
            rly tx link a-b-unordered # For Case 4
            ```
            You might need to run `rly tx link <path-name> --src-port transfer --dst-port transfer` if defaults are not sufficient.
        *   **Verify Channel IDs:** After linking, use `rly paths show <path-name>` to get the generated `src_channel_id` and `dst_channel_id`.
            *   **Update Script Constants (if necessary):** The `const` blocks in `case1_*.go`, `case2_*.go`, `case3_*.go`, and especially `case4_*.go` contain default channel IDs and path names.
                *   Path names like `ibcPathName`, `ibcPathOrdered`, `ibcPathUnordered` should match what the Makefile sets up for `rly`.
                *   Channel IDs like `channelA_ID_on_A`, `channelA_ID_Ordered`, etc., **must be updated** in the Go scripts to match the actual channel IDs created by `rly tx link`.

7.  **Start the Chains:**
    The `Makefile` provides commands to start the chains. You need to run these in separate terminal windows.
    ```bash
    make start-chains
    ```
    This will output commands like:
    ```
    simd start --home ./chain-a-data --rpc.laddr tcp://0.0.0.0:26657 --grpc.address 0.0.0.0:9090
    simd start --home ./chain-b-data --rpc.laddr tcp://0.0.0.0:27657 --grpc.address 0.0.0.0:9190
    ```
    Ensure both chains are running and producing blocks.

8.  **Run the Tests:**
    Once chains are running and the relayer is configured (but `rly start` should generally be **stopped** for controlled tests), run the Go scripts:
    ```bash
    make run
    ```
    This executes all `case*.go` scripts sequentially, passing necessary configuration (chain homes, RPCs, etc.) via environment variables.

    Alternatively, to run everything from verification to tests (excluding manual relayer key restoration and linking):
    ```bash
    make all
    ```

## Script Configuration Details

*   **Environment Variables (via `config.go`):**
    *   The `Makefile` sets crucial environment variables (e.g., `CHAIN_A_HOME_ENV`, `CHAIN_A_RPC_ENV`, `RLY_CONFIG_FILE_ENV`).
    *   The `config.go` file reads these variables at the start of each script.
    *   If required environment variables are missing, the scripts will exit with an error, prompting you to use the `Makefile`.

*   **Constants in `.go` scripts:**
    *   While core paths and RPCs are from environment variables, each `caseX_*.go` script still has a `const` block.
    *   **You MUST review these `const` blocks for:**
        *   Key names (`userA_KeyName`, `attackerB_KeyName`, etc.) - ensure they match the keys created by `make chains`.
        *   Token denominations and amounts (`ibcTokenDenom`, `ibcTransferAmount`, etc.).
        *   Specific IBC path names (`ibcPathName`, `ibcPathOrdered`, `ibcPathUnordered`) - ensure these match the paths configured by `make setup-rly-config`.
        *   **Crucially for Case 4 (and other cases):** The `channelA_ID_Ordered`, `channelA_ID_Unordered`, `channelA_ID_on_A`, etc. constants. These **must be updated** to reflect the actual channel IDs created after running `rly tx link <path-name>`. Use `rly paths show <path-name>` to find these.

## Relayer Status During Tests

*   For scenarios requiring controlled relaying (most of these tests use `rly tx relay-packets ... --sequence ...`), ensure your main relayer process (`rly start`) is **stopped** for the paths under test. This prevents the relayer from processing packets before the script intends.
*   If a test scenario *expects* the relayer to pick up packets automatically, you might run `rly start` for the relevant path, but be mindful of its speed relative to the script's actions.

## Interpreting Results

*   **Log Output:** Each script prints detailed logs about its actions, commands executed, and verification outcomes.
*   **SUCCESS/FAILURE Messages:** Look for explicit "SUCCESS," "FAILURE," "POTENTIAL," or "ERROR" messages.
*   **Transaction Order:** The primary goal is to see if the attacker's transaction is processed before the victim's IBC packet (`RecvPacket`) on the target chain. Block heights and transaction indices (where checked) are key.
*   **Channel Ordering (Case 4):** Verify that packets on the `ordered` channel are processed in sequence on Chain B, while on the `unordered` channel, they may not be.

## Troubleshooting

*   **`make` command errors:** Check the error output for clues. Ensure all prerequisites (`go`, `make`, `jq`, `simd`) are installed.
*   **"Environment variables are not set" (from Go scripts):** You likely ran a `go run caseX_*.go` command directly without the `Makefile` or without manually setting all required `_ENV` variables. Use `make run`.
*   **`simd` or `rly` command failures within scripts:** The script output (stderr) should indicate the cause (e.g., chain not reachable, account not funded, incorrect flags, relayer misconfiguration).
*   **"Packet sequence not found" / "RecvPacket transaction not found":**
    *   Ensure chains are running and healthy.
    *   Verify relayer paths are correctly configured and linked, and keys are funded.
    *   Check that the channel IDs in the script's `const` block are correct for the linked paths.
*   **Relayer Interference:** If `rly start` is running unexpectedly, it might process packets too quickly. Stop it for controlled tests.
*   **`sed: command not found` or `jq: command not found`:** Install these utilities.

## Cleanup

To remove the local chain data generated by `make chains`:
```bash
make clean-chain-data
```
Or for a more general cleanup:
```bash
make clean
```

## Disclaimer

These scripts and the Makefile are for educational and testing purposes in a controlled local environment. Simulating front-running is complex, and actual blockchain network conditions can vary significantly.
