package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"
)

// --- Structs for JSON Parsing ---
type Attribute struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type Event struct {
	Type       string      `json:"type"`
	Attributes []Attribute `json:"attributes"`
}

type LogEntry struct {
	MsgIndex int     `json:"msg_index"`
	Log      string  `json:"log"`
	Events   []Event `json:"events"`
}

type TxResponse struct {
	Height    string     `json:"height"`
	TxHash    string     `json:"txhash"`
	Code      int        `json:"code"`
	RawLog    string     `json:"raw_log"`
	Logs      []LogEntry `json:"logs"`
	Events    []Event    `json:"events"`
	GasWanted string     `json:"gas_wanted"`
	GasUsed   string     `json:"gas_used"`
	Timestamp string     `json:"timestamp"`
}

type SearchTxsResult struct {
	Txs []TxResponse `json:"txs"`
}

type Coin struct {
	Denom  string `json:"denom"`
	Amount string `json:"amount"`
}

type BalanceResponse struct { // Used if querying all balances for an address
	Balances []Coin `json:"balances"`
}

type BlockData struct {
	Txs []string `json:"txs"` // List of base64-encoded transactions
}

type BlockHeader struct {
	Height string `json:"height"`
}

type Block struct {
	Header BlockHeader `json:"header"`
	Data   BlockData   `json:"data"`
}

type BlockResponse struct {
	BlockID interface{} `json:"block_id"` // Can be complex
	Block   Block       `json:"block"`
}

// --- End Structs ---

// --- Shared Helper Functions ---

func executeCommand(printCmd bool, name string, args ...string) (string, string, error) {
	cmd := exec.Command(name, args...)
	if printCmd {
		log.Printf("Executing: %s %s", name, strings.Join(args, " "))
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	outStr := strings.TrimSpace(stdout.String())
	errStr := strings.TrimSpace(stderr.String())

	if err != nil {
		return outStr, errStr, fmt.Errorf("command '%s %s' failed: %w\nStdout: %s\nStderr: %s", name, strings.Join(args, " "), err, outStr, errStr)
	}
	return outStr, errStr, nil
}

func getKeyAddress(chainID, node, keyName, home, keyring string) (string, error) {
	args := []string{"keys", "show", keyName, "-a",
		"--keyring-backend", keyring,
		"--home", home,
	}
	// --chain-id and --node flags are not supported by `simd keys show`
	// and are generally not needed as it operates on the local key store.
	// The --home and --keyring-backend flags are sufficient.

	stdout, _, err := executeCommand(false, simdBinary, args...)
	if err != nil {
		return "", fmt.Errorf("getting address for key '%s' (home: %s, keyring: %s): %w", keyName, home, keyring, err)
	}
	return strings.TrimSpace(stdout), nil
}

func ibcTransfer(chainID, node, home, fromKey, toAddr, srcPort, srcChannel, amountToken, fee, customGasFlags string) (string, error) {
	// Start with base command, subcommand, and its core positional arguments
	args := []string{"tx", "ibc-transfer", "transfer", srcPort, srcChannel, toAddr, amountToken}

	// Add the --from flag with fromKey
	args = append(args, "--from", fromKey)

	// Add other common transaction flags at the end
	args = append(args, "--chain-id", chainID)
	args = append(args, "--node", node)
	args = append(args, "--home", home)
	args = append(args, "--keyring-backend", keyringBackend) // Using global config
	args = append(args, "--fees", fee)
	args = append(args, "-y")
	args = append(args, "-o", "json")

	if customGasFlags != "" {
		args = append(args, strings.Split(customGasFlags, " ")...)
	} else {
		args = append(args, strings.Split(defaultGasFlags, " ")...) // Using global config
	}

	stdout, _, err := executeCommand(true, simdBinary, args...)
	if err != nil {
		return "", fmt.Errorf("ibc transfer failed: %w", err)
	}

	var resp TxResponse
	if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
		return "", fmt.Errorf("failed to unmarshal ibc transfer tx response: %w\nResponse: %s", err, stdout)
	}
	if resp.Code != 0 {
		return "", fmt.Errorf("ibc transfer tx failed with code %d: %s", resp.Code, resp.RawLog)
	}
	return resp.TxHash, nil
}

func bankSend(chainID, node, home, fromKey, toAddr, amountToken, fee, customGasFlags string) (string, error) {
	// Start with base command, subcommand, and all its positional arguments (including fromKey)
	args := []string{"tx", "bank", "send", fromKey, toAddr, amountToken}

	// Add common transaction flags at the end
	args = append(args, "--chain-id", chainID)
	args = append(args, "--node", node)
	args = append(args, "--home", home)
	args = append(args, "--keyring-backend", keyringBackend) // Using global config
	args = append(args, "--fees", fee)
	args = append(args, "-y")
	args = append(args, "-o", "json")

	if customGasFlags != "" {
		args = append(args, strings.Split(customGasFlags, " ")...)
	} else {
		args = append(args, strings.Split(defaultGasFlags, " ")...) // Using global config
	}

	stdout, _, err := executeCommand(true, simdBinary, args...)
	if err != nil {
		return "", fmt.Errorf("bank send failed: %w", err)
	}

	var resp TxResponse
	if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
		return "", fmt.Errorf("failed to unmarshal bank send tx response: %w\nResponse: %s", err, stdout)
	}
	if resp.Code != 0 {
		return "", fmt.Errorf("bank send tx failed with code %d: %s", resp.Code, resp.RawLog)
	}
	return resp.TxHash, nil
}

func queryTx(chainID, node, txHash string) (*TxResponse, error) {
	args := []string{"query", "tx", txHash,
		"--chain-id", chainID,
		"--node", node,
		"-o", "json",
	}
	stdout, _, err := executeCommand(false, simdBinary, args...)
	if err != nil {
		return nil, fmt.Errorf("querying tx %s failed: %w", txHash, err)
	}

	var resp TxResponse
	if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal query tx response for %s: %w\nResponse: %s", txHash, err, stdout)
	}
	if resp.TxHash == "" { // Basic check if unmarshalling was incomplete or tx not found
		return nil, fmt.Errorf("tx %s not found or invalid response: %s", txHash, stdout)
	}
	return &resp, nil
}

func findPacketSequenceFromTx(chainID, node, txHash, srcPortID, srcChannelID string) (string, error) {
	txInfo, err := queryTx(chainID, node, txHash)
	if err != nil {
		return "", fmt.Errorf("finding packet sequence, queryTx failed for %s: %w", txHash, err)
	}

	if txInfo.Code != 0 {
		return "", fmt.Errorf("tx %s failed with code %d: %s", txHash, txInfo.Code, txInfo.RawLog)
	}

	for _, logEntry := range txInfo.Logs {
		for _, event := range logEntry.Events {
			if event.Type == "send_packet" {
				var packetSrcPort, packetSrcChannel, packetSequence string
				for _, attr := range event.Attributes {
					// Decode attribute keys if necessary (they might be base64 encoded)
					// For this example, assuming they are plain strings as in original scripts
					decodedKey := attr.Key // Placeholder if decoding is needed
					decodedValue := attr.Value // Placeholder

					if decodedKey == "packet_src_port" {
						packetSrcPort = decodedValue
					}
					if decodedKey == "packet_src_channel" {
						packetSrcChannel = decodedValue
					}
					if decodedKey == "packet_sequence" {
						packetSequence = decodedValue
					}
				}
				if packetSrcPort == srcPortID && packetSrcChannel == srcChannelID && packetSequence != "" {
					return packetSequence, nil
				}
			}
		}
	}

	// Also check top-level events (newer Gaia versions put events here instead of in logs)
	for _, event := range txInfo.Events {
		if event.Type == "send_packet" {
			var packetSrcPort, packetSrcChannel, packetSequence string
			for _, attr := range event.Attributes {
				decodedKey := attr.Key
				decodedValue := attr.Value

				if decodedKey == "packet_src_port" {
					packetSrcPort = decodedValue
				}
				if decodedKey == "packet_src_channel" {
					packetSrcChannel = decodedValue
				}
				if decodedKey == "packet_sequence" {
					packetSequence = decodedValue
				}
			}
			if packetSrcPort == srcPortID && packetSrcChannel == srcChannelID && packetSequence != "" {
				return packetSequence, nil
			}
		}
	}

	return "", fmt.Errorf("packet sequence not found in tx %s for port %s, channel %s. Raw log: %s", txHash, srcPortID, srcChannelID, txInfo.RawLog)
}

func relaySpecificPacket(rlyCfg, pathName, srcChanID, sequence string) error {
	args := []string{"tx", "flush", pathName, srcChanID,
		"--home", rlyCfg, // Using global rlyConfigPath passed to function
	}

	_, stderr, err := executeCommand(true, rlyBinary, args...)
	if err != nil {
		// Check for common non-fatal stderr messages from rly
		lowerErr := strings.ToLower(stderr)
		if strings.Contains(lowerErr, "no packets to relay found") ||
			strings.Contains(lowerErr, "already relayed") ||
			strings.Contains(lowerErr, "0/0 packets relayed") || // Newer rly versions
			strings.Contains(lowerErr, "light client state is not within trust period") ||
			strings.Contains(lowerErr, "failed to send messages: 0/1 messages failed") ||
			strings.Contains(lowerErr, "result does not exist") { // can happen if already relayed by another process
			log.Printf("Relay command (path %s, seq %s, chan %s) had non-critical stderr: %s (Original error: %v)", pathName, sequence, srcChanID, stderr, err)
			return nil // Treat as non-fatal for the script's flow
		}
		return fmt.Errorf("relay packet (path %s, seq %s, chan %s) failed: %w. Stderr: %s", pathName, sequence, srcChanID, err, stderr)
	}
	log.Printf("Relay command (path %s, seq %s, chan %s) potentially successful. Stderr: %s", pathName, sequence, srcChanID, stderr)
	return nil
}

func findRecvPacketTx(chainID, node, dstPortID, dstChannelID, sequence string) (*TxResponse, error) {
	eventQueries := fmt.Sprintf("recv_packet.packet_dst_port='%s' AND recv_packet.packet_dst_channel='%s' AND recv_packet.packet_sequence='%s'", dstPortID, dstChannelID, sequence)
	args := []string{"query", "txs", "--query", eventQueries,
		"--node", node,
		"--chain-id", chainID,
		"-o", "json",
		"--limit", "1",       // We only expect one such event for a unique sequence
		"--order_by", "asc", // Get the first one if multiple (should not happen for unique seq)
	}
	stdout, _, err := executeCommand(false, simdBinary, args...)
	if err != nil {
		return nil, fmt.Errorf("querying txs by events for RecvPacket failed (port %s, chan %s, seq %s): %w", dstPortID, dstChannelID, sequence, err)
	}

	var searchResult SearchTxsResult
	if err := json.Unmarshal([]byte(stdout), &searchResult); err != nil {
		return nil, fmt.Errorf("failed to unmarshal SearchTxsResult for RecvPacket (port %s, chan %s, seq %s): %w\nResponse: %s", dstPortID, dstChannelID, sequence, err, stdout)
	}

	if len(searchResult.Txs) == 0 {
		return nil, fmt.Errorf("no RecvPacket transaction found for port %s, channel %s, sequence %s", dstPortID, dstChannelID, sequence)
	}
	if len(searchResult.Txs) > 1 {
		log.Printf("Warning: Found multiple RecvPacket transactions for port %s, channel %s, sequence %s. Using the first one.", dstPortID, dstChannelID, sequence)
	}

	// Verify the event attributes more carefully from the first Tx found
	txToVerify := searchResult.Txs[0]
	for _, logEntry := range txToVerify.Logs {
		for _, event := range logEntry.Events {
			if event.Type == "recv_packet" {
				var foundDstPort, foundDstChannel, foundSeq string
				for _, attr := range event.Attributes {
					// Assuming plain string keys/values as before
					if attr.Key == "packet_dst_port" {
						foundDstPort = attr.Value
					}
					if attr.Key == "packet_dst_channel" {
						foundDstChannel = attr.Value
					}
					if attr.Key == "packet_sequence" {
						foundSeq = attr.Value
					}
				}
				if foundDstPort == dstPortID && foundDstChannel == dstChannelID && foundSeq == sequence {
					return &txToVerify, nil // Return the full TxResponse
				}
			}
		}
	}

	// Also check top-level events (newer Gaia versions put events here instead of in logs)
	for _, event := range txToVerify.Events {
		if event.Type == "recv_packet" {
			var foundDstPort, foundDstChannel, foundSeq string
			for _, attr := range event.Attributes {
				if attr.Key == "packet_dst_port" {
					foundDstPort = attr.Value
				}
				if attr.Key == "packet_dst_channel" {
					foundDstChannel = attr.Value
				}
				if attr.Key == "packet_sequence" {
					foundSeq = attr.Value
				}
			}
			if foundDstPort == dstPortID && foundDstChannel == dstChannelID && foundSeq == sequence {
				return &txToVerify, nil // Return the full TxResponse
			}
		}
	}

	return nil, fmt.Errorf("no RecvPacket transaction found with matching event attributes for port %s, channel %s, sequence %s, despite initial query success. TxHash: %s, RawLog: %s", dstPortID, dstChannelID, sequence, txToVerify.TxHash, txToVerify.RawLog)
}

func queryBalance(chainID, node, address, denom string) (string, error) {
	args := []string{"query", "bank", "balances", address,
		"--node", node,
		"--chain-id", chainID,
		"-o", "json",
	}

	// Capture stderr as well, as `simd` might output "account ... not found" to stderr and exit with 0 or error.
	stdout, execStdErr, execErr := executeCommand(false, simdBinary, args...)

	// Handle cases where the account or denom might not exist.
	if execErr != nil {
		// If executeCommand itself returned an error (e.g., command not found, non-zero exit)
		// Check if stderr indicates "not found" or similar, meaning zero balance.
		if strings.Contains(execStdErr, "not found") || strings.Contains(execStdErr, "no balance") || strings.Contains(stdout, "amount:\"0\"") || strings.Contains(stdout, "amount\":\"0\"") {
			return "0", nil
		}
		// For other execution errors, return the error.
		return "Error", fmt.Errorf("query balance for %s denom %s failed: %w. Stderr: %s. Stdout: %s", address, denom, execErr, execStdErr, stdout)
	}

	// If command executed successfully (exit code 0), try to unmarshal the balances response.
	var balancesResp struct {
		Balances []Coin `json:"balances"`
	}
	if errUnmarshal := json.Unmarshal([]byte(stdout), &balancesResp); errUnmarshal != nil {
		// If unmarshal fails, but stdout is empty or indicates no balance, treat as zero.
		if strings.TrimSpace(stdout) == "" || strings.Contains(stdout, `"balances":[]`) || stdout == "{}" {
			return "0", nil
		}
		return "Error", fmt.Errorf("failed to unmarshal balance query response for %s, denom %s: %w. Output: %s", address, denom, errUnmarshal, stdout)
	}

	// Look for the specific denom in the balances array
	for _, coin := range balancesResp.Balances {
		if coin.Denom == denom {
			if coin.Amount == "" { // Sometimes amount might be an empty string for 0.
				return "0", nil
			}
			return coin.Amount, nil
		}
	}

	// If denom not found in balances, return 0
	// and there was no execution error, it implies a zero balance for that specific denom.
	return "0", nil
}

func queryBlock(chainID, node, height string) (*BlockResponse, error) {
	args := []string{"query", "block", height,
		"--node", node,
		// "--chain-id", chainID, // `query block` might not need chain-id if node is specific
		"-o", "json",
	}
	stdout, _, err := executeCommand(false, simdBinary, args...)
	if err != nil {
		return nil, fmt.Errorf("querying block %s on node %s failed: %w", height, node, err)
	}

	var resp BlockResponse
	if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal query block response for height %s: %w\nResponse: %s", height, err, stdout)
	}
	return &resp, nil
}

// sha256HashBytes computes the SHA256 hash of a byte slice and returns its uppercase hex representation.
func sha256HashBytes(data []byte) string {
	h := sha256.New()
	h.Write(data)
	return strings.ToUpper(hex.EncodeToString(h.Sum(nil)))
}

// getTxHashFromBytes decodes a base64 encoded transaction string (from block data)
// and computes its SHA256 hash, returning the uppercase hex string.
func getTxHashFromBytes(b64Tx string) (string, error) {
	txBytes, err := base64.StdEncoding.DecodeString(b64Tx)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64 tx: %w", err)
	}
	return sha256HashBytes(txBytes), nil
}
// --- End Shared Helper Functions ---
