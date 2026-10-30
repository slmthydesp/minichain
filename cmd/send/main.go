package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"

	"github.com/slmthydesp/minichain/chain"
	"github.com/slmthydesp/minichain/sig"
)

func main() {
	url := flag.String("url", "", "node URL")
	toHex := flag.String("to", "", "recipient address")
	value := flag.Uint64("value", 0, "amount")
	nonceStr := flag.String("nonce", "", "account nonce")
	gasLimit := flag.Uint64("gas-limit", 21000, "gas limit")
	gasPrice := flag.Uint64("gas-price", 1, "gas price")
	keyHex := flag.String("key", sig.DemoKeyHex, "hex private key")
	flag.Parse()
	if *url == "" || *toHex == "" || *nonceStr == "" {
		fmt.Fprintln(os.Stderr, "url, to, and nonce are required")
		os.Exit(1)
	}
	nonce, err := strconv.ParseUint(*nonceStr, 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nonce: %v\n", err)
		os.Exit(1)
	}
	key, err := sig.ParseKey(*keyHex)
	if err != nil {
		fmt.Fprintf(os.Stderr, "key: %v\n", err)
		os.Exit(1)
	}
	to, err := chain.ParseAddress(*toHex)
	if err != nil {
		fmt.Fprintf(os.Stderr, "to: %v\n", err)
		os.Exit(1)
	}
	tx, err := chain.Sign(key, to, *value, nonce, *gasLimit, *gasPrice, chain.ChainID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sign: %v\n", err)
		os.Exit(1)
	}
	body, err := json.Marshal(map[string]any{
		"to":       tx.To.Hex(),
		"value":    tx.Value,
		"nonce":    tx.Nonce,
		"gasLimit": tx.GasLimit,
		"gasPrice": tx.GasPrice,
		"chainId":  tx.ChainID,
		"sig":      "0x" + hex.EncodeToString(tx.Sig),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "json: %v\n", err)
		os.Exit(1)
	}
	resp, err := http.Post(*url+"/tx", "application/json", bytes.NewReader(body))
	if err != nil {
		fmt.Fprintf(os.Stderr, "post: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "body: %v\n", err)
		os.Exit(1)
	}
	if resp.StatusCode != http.StatusNoContent {
		fmt.Fprintf(os.Stderr, "status %d: %s\n", resp.StatusCode, respBody)
		os.Exit(1)
	}
}
