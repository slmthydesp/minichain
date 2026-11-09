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

var (
	exitFunc = os.Exit
	postFunc = http.Post
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		exitFunc(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("send", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	url := fs.String("url", "", "node URL")
	toHex := fs.String("to", "", "recipient address, empty to create a contract")
	value := fs.Uint64("value", 0, "amount in the smallest unit; 10^18 is one coin")
	nonceStr := fs.String("nonce", "", "account nonce; empty queries the node")
	gasLimit := fs.Uint64("gas-limit", 21000, "gas limit")
	gasPrice := fs.Uint64("gas-price", 1, "gas price")
	dataHex := fs.String("data", "", "hex calldata or init code")
	keyHex := fs.String("key", sig.DemoKeyHex, "hex private key")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *url == "" {
		return fmt.Errorf("url is required")
	}
	key, err := sig.ParseKey(*keyHex)
	if err != nil {
		return fmt.Errorf("key: %v", err)
	}
	var nonce uint64
	if *nonceStr == "" {
		raw, err := sig.KeyAddress(key)
		if err != nil {
			return fmt.Errorf("key: %v", err)
		}
		var addr chain.Address
		copy(addr[:], raw[:])
		nonce, err = queryNextNonce(*url, addr.Hex())
		if err != nil {
			return fmt.Errorf("nonce: %v", err)
		}
		fmt.Fprintf(os.Stderr, "nonce: %d\n", nonce)
	} else {
		nonce, err = strconv.ParseUint(*nonceStr, 10, 64)
		if err != nil {
			return fmt.Errorf("nonce: %v", err)
		}
	}
	var data []byte
	if *dataHex != "" {
		data, err = hex.DecodeString(strings.TrimPrefix(strings.TrimPrefix(*dataHex, "0x"), "0X"))
		if err != nil {
			return fmt.Errorf("data: %v", err)
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
			return fmt.Errorf("to: %v", err)
		}
		toJSON = to.Hex()
		if len(data) == 0 {
			tx, err = chain.Sign(key, to, *value, nonce, *gasLimit, *gasPrice, chain.ChainID)
		} else {
			tx, err = chain.SignCall(key, to, *value, nonce, *gasLimit, *gasPrice, chain.ChainID, data)
		}
	}
	if err != nil {
		return fmt.Errorf("sign: %v", err)
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
		return fmt.Errorf("json: %v", err)
	}
	resp, err := postFunc(*url+"/tx", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("post: %v", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("body: %v", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("status %d: %s", resp.StatusCode, respBody)
	}
	return nil
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
