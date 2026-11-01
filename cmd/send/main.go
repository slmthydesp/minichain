// 命令 send 用私钥签一笔交易，并 POST 到节点的 /tx。to 留空是创建合约。chain id 固定为 chain.ChainID。
// --value 按最小单位填写，10^18 是 1 个币。
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
	"strings"

	"github.com/slmthydesp/minichain/chain"
	"github.com/slmthydesp/minichain/sig"
)

func main() {
	url := flag.String("url", "", "node URL")
	toHex := flag.String("to", "", "recipient address, empty to create a contract")
	value := flag.Uint64("value", 0, "amount in the smallest unit; 10^18 is one coin")
	nonceStr := flag.String("nonce", "", "account nonce; empty queries the node")
	gasLimit := flag.Uint64("gas-limit", 21000, "gas limit")
	gasPrice := flag.Uint64("gas-price", 1, "gas price")
	dataHex := flag.String("data", "", "hex calldata or init code")
	keyHex := flag.String("key", sig.DemoKeyHex, "hex private key")
	flag.Parse()
	if *url == "" {
		fmt.Fprintln(os.Stderr, "url is required")
		os.Exit(1)
	}
	key, err := sig.ParseKey(*keyHex)
	if err != nil {
		fmt.Fprintf(os.Stderr, "key: %v\n", err)
		os.Exit(1)
	}
	var nonce uint64
	if *nonceStr == "" {
		raw, err := sig.KeyAddress(key)
		if err != nil {
			fmt.Fprintf(os.Stderr, "key: %v\n", err)
			os.Exit(1)
		}
		var addr chain.Address
		copy(addr[:], raw[:])
		nonce, err = queryNextNonce(*url, addr.Hex())
		if err != nil {
			fmt.Fprintf(os.Stderr, "nonce: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "nonce: %d\n", nonce)
	} else {
		nonce, err = strconv.ParseUint(*nonceStr, 10, 64)
		if err != nil {
			fmt.Fprintf(os.Stderr, "nonce: %v\n", err)
			os.Exit(1)
		}
	}
	var data []byte
	if *dataHex != "" {
		data, err = hex.DecodeString(strings.TrimPrefix(strings.TrimPrefix(*dataHex, "0x"), "0X"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "data: %v\n", err)
			os.Exit(1)
		}
	}
	var tx chain.Tx
	toJSON := ""
	if *toHex == "" {
		tx, err = chain.SignCreate(key, *value, nonce, *gasLimit, *gasPrice, chain.ChainID, data)
	} else {
		var to chain.Address
		to, err = chain.ParseAddress(*toHex)
		if err != nil {
			fmt.Fprintf(os.Stderr, "to: %v\n", err)
			os.Exit(1)
		}
		toJSON = to.Hex()
		if len(data) == 0 {
			tx, err = chain.Sign(key, to, *value, nonce, *gasLimit, *gasPrice, chain.ChainID)
		} else {
			tx, err = chain.SignCall(key, to, *value, nonce, *gasLimit, *gasPrice, chain.ChainID, data)
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "sign: %v\n", err)
		os.Exit(1)
	}
	body, err := json.Marshal(map[string]any{
		"to":       toJSON,
		"data":     "0x" + hex.EncodeToString(tx.Data),
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

// queryNextNonce 向节点要这个地址下一笔还能放进交易池的 nonce。
func queryNextNonce(nodeURL, address string) (uint64, error) {
	nodeURL = strings.TrimRight(nodeURL, "/")
	resp, err := http.Get(nodeURL + "/account?address=" + address)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("status %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}
	var got struct {
		NextNonce uint64 `json:"nextNonce"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		return 0, err
	}
	return got.NextNonce, nil
}
