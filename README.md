# IBC Front-Running Vulnerability Research Testbed

A fully automated, reproducible testbed for researching Inter-Blockchain Communication (IBC) front-running vulnerabilities in Cosmos SDK-based blockchains.

## 🎯 What This Is

This testbed simulates potential front-running attacks in IBC (Inter-Blockchain Communication) environments by running two local blockchain networks and demonstrating how attackers might exploit timing vulnerabilities in cross-chain transactions.

**Front-running** is when an attacker observes a pending transaction and submits their own transaction with higher fees or better timing to be processed first, potentially profiting from the knowledge of the upcoming transaction.

## 🧪 Experiment Scenarios

This testbed demonstrates **4 different front-running attack vectors**:

1. **Relayer Front-Running**: Attacker delays or manipulates packet relay timing
2. **Validator Fee Front-Running**: Attacker uses higher fees to get processed before victim's IBC packet
3. **Cross-Chain MEV**: Attacker performs "sandwich" attacks around large IBC transfers (mocked DeFi scenario)
4. **Channel Ordering Impact**: Comparing attack success on ordered vs unordered IBC channels

## 🚀 Quick Start (One Command Setup)

### Prerequisites

You need these installed on your system:
- **Go** (1.23+): Download from [golang.org](https://golang.org/dl/)
- **Make**: Usually pre-installed on macOS/Linux
- **jq**: JSON processor - install with `brew install jq` (macOS) or `apt install jq` (Linux)
- **Relayer**: Will be automatically installed during setup

### One-Command Setup & Run

```bash
git clone <your-repo-url>
cd ibc-frontrun-testbed
make all
```

That's it! This single command will:
1. Verify all dependencies
2. Download and build Gaia blockchain software locally
3. Initialize two blockchain networks
4. Start the chains locally in background
5. Set up and configure the IBC relayer
6. Automatically discover IBC channel IDs
7. Validate the complete setup
8. Run all 4 front-running test scenarios

## Understanding the Results

### What to Look For

Each test case will output detailed logs. Look for these key indicators:

**SUCCESS Messages:**
- `SUCCESS: Attacker's tx processed before victim's RecvPacket`
- `SUCCESSFUL Front-run`

**FAILURE Messages:**
- `FAILURE: Attacker's tx processed after victim's RecvPacket`
- `FAILED Front-run`

**Example Successful Attack Output:**
```
SUCCESS: Attacker's transaction (index 0) appeared BEFORE RecvPacket transaction (index 1) in block 123 due to higher fee.
```

### Test Case Explanations

#### Case 1: Relayer Front-Running (`make run-case1`)
- **What it tests**: Can an attacker delay legitimate IBC packets and insert their own transaction first?
- **Attack method**: Attacker submits transaction before relaying victim's packet
- **Success criteria**: Attacker's transaction appears in an earlier block than the victim's IBC packet
- **Real-world impact**: Attackers could delay cross-chain transfers to manipulate prices

#### Case 2: Validator Fee Front-Running (`make run-case2`)
- **What it tests**: Can higher fees guarantee transaction ordering within the same block?
- **Attack method**: Attacker uses higher fees to prioritize their transaction
- **Success criteria**: Attacker's high-fee transaction appears before victim's IBC packet in the same block
- **Real-world impact**: Fee-based MEV attacks on cross-chain transactions

#### Case 3: Cross-Chain MEV - Sandwich Attack (`make run-case3`)
- **What it tests**: Can attackers profit by trading before and after large IBC transfers?
- **Attack method**: Buy → Victim's large transfer → Sell (price manipulation)
- **Success criteria**: Attacker's "buy" → Victim's transfer → Attacker's "sell" in correct sequence
- **Real-world impact**: Price manipulation around large cross-chain moves

#### Case 4: Channel Ordering Impact (`make run-case4`)
- **What it tests**: Do ordered vs unordered IBC channels affect front-running success?
- **Attack method**: Compare attack success on different channel types
- **Success criteria**: Different behaviors between ordered and unordered channels
- **Real-world impact**: Protocol design decisions affect security

## Manual Operation & Individual Test Cases

### Running Individual Test Cases (For Debugging)

You can run individual test cases for easier debugging:

```bash
# Run specific test cases
make run-case1  # Case 1: Relayer Front-Running
make run-case2  # Case 2: Validator Fee Front-Running
make run-case3  # Case 3: Cross-Chain MEV (Sandwich Attack)
make run-case4  # Case 4: Channel Ordering Impact

# Run all test cases at once
make run
```

### Manual Setup (If you want to run components separately)

```bash
# 1. Build everything
make dependencies

# 2. Set up chains
make chains

# 3. Start chains locally
make start-chains-local

# 4. Set up relayer (automated)
make setup-relayer

# 5. Validate setup
make validate

# 6. Run experiments (all cases)
make run

# 7. Clean up when done
make clean
```

## Troubleshooting

### Common Issues

**"Command not found" errors:**
```bash
# Install missing tools
brew install go jq  # macOS
# or
apt install golang-go jq  # Ubuntu/Debian
```

**"Port already in use" errors:**
```bash
# Clean up any existing processes
make clean
# Kill any stray processes
pkill -f "gaiad\|simd"
```

**"Channel IDs not discovered" errors:**
```bash
# Re-run relayer setup
make setup-relayer
```

**Chains not responding:**
```bash
# Check chain logs
tail -f chain-a.log
tail -f chain-b.log

# Restart chains
make stop-chains-local
make start-chains-local
```

### Getting Help

If you encounter issues:

1. **Check the logs**: Each step shows detailed output
2. **Run validation**: `make validate` to test the setup
3. **Clean and retry**: `make clean && make all`
4. **Check prerequisites**: Ensure all required software is installed

## Project Structure

```
ibc-frontrun-testbed/
├── Makefile                    # Automation scripts with individual targets
├── main/
│   ├── config.go              # Shared configuration
│   ├── utils.go               # Shared utilities
│   ├── case1_relayer_frontrun.go      # Relayer timing attack
│   ├── case2_validator_fee_frontrun.go # Fee-based priority attack
│   ├── case3_cross_chain_mev_mocked.go # Cross-chain MEV sandwich
│   ├── case4_channel_order_frontrun.go # Channel ordering comparison
│   └── validate_setup.go      # Setup validation
└── configs/                  # Generated configurations and channel mappings
```

## Educational Value

This testbed is designed for:
- **Blockchain researchers** studying cross-chain security
- **Protocol developers** testing IBC implementations
- **Security auditors** analyzing front-running vectors
- **Students** learning about blockchain MEV and cross-chain protocols

## Important Notes

- **Local Testing Only**: This runs on localhost for research purposes
- **Simulated Environment**: Real networks have different timing and complexity
- **Educational Use**: Results may not directly translate to production networks
- **No Real Value**: Uses test tokens with no monetary value

## Cleaning Up

When you're done experimenting:

```bash
# Stop everything and clean up
make clean

# This removes:
# - Running blockchain processes
# - Generated blockchain data
# - Log files and process IDs
# - Temporary configurations
# - Downloaded source code (optional: remove gaia_source/ manually)
```

## Advanced Usage

### Debugging Individual Cases

For detailed debugging, run cases individually:

```bash
# Debug specific attack patterns
make run-case1  # Focus on relayer timing
make run-case2  # Focus on fee market dynamics
make run-case3  # Focus on MEV sandwich patterns
make run-case4  # Focus on channel ordering effects
```

### Customizing Attack Parameters

You can modify attack parameters by editing the case files in `main/`:
- `case2_attackerHighFee` - Adjust attacker's fee amount
- `case3_victimTransferAmount` - Change victim's transfer size
- `defaultFee` - Modify default transaction fees

## Next Steps

After running the experiments:

1. **Analyze the logs** to understand attack patterns and timing
2. **Run individual cases** to focus on specific attack vectors
3. **Modify test parameters** in the case files to test different scenarios
4. **Read the source code** to understand how each attack works
5. **Research countermeasures** to prevent these attacks

## Contributing

This is a research tool. Feel free to:
- Add new attack scenarios
- Improve existing test cases
- Enhance documentation
- Report bugs or suggest improvements

---

**Happy researching!** This testbed makes IBC front-running research accessible to everyone, from beginners to experts.
