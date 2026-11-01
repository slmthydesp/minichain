package node

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/slmthydesp/minichain/chain"
)

// txJSON 是交易的 HTTP 表示。金额、nonce、gas 和 chainId 都是十进制整数，地址和签名是十六进制。
type txJSON struct {
	To       string `json:"to"`
	Data     string `json:"data,omitempty"`
	Value    uint64 `json:"value"`
	Nonce    uint64 `json:"nonce"`
	GasLimit uint64 `json:"gasLimit"`
	GasPrice uint64 `json:"gasPrice"`
	ChainID  uint64 `json:"chainId"`
	Sig      string `json:"sig"`
}

type logJSON struct {
	Address string   `json:"address"`
	Topics  []string `json:"topics"`
	Data    string   `json:"data"`
}

type receiptJSON struct {
	Status            uint64    `json:"status"`
	GasUsed           uint64    `json:"gasUsed"`
	CumulativeGasUsed uint64    `json:"cumulativeGasUsed"`
	Logs              []logJSON `json:"logs"`
}

type blockJSON struct {
	Height           uint64        `json:"height"`
	ParentHash       string        `json:"parentHash"`
	StateRoot        string        `json:"stateRoot"`
	TransactionsRoot string        `json:"transactionsRoot"`
	ReceiptsRoot     string        `json:"receiptsRoot"`
	Hash             string        `json:"hash"`
	Coinbase         string        `json:"coinbase"`
	GasLimit         uint64        `json:"gasLimit"`
	GasUsed          uint64        `json:"gasUsed"`
	Time             uint64        `json:"time"`
	Txs              []txJSON      `json:"txs"`
	Receipts         []receiptJSON `json:"receipts"`
}

type statusJSON struct {
	Height    uint64 `json:"height"`
	StateRoot string `json:"stateRoot"`
	Head      string `json:"head"`
}

type accountJSON struct {
	Address   string `json:"address"`
	Balance   uint64 `json:"balance"`
	Nonce     uint64 `json:"nonce"`
	NextNonce uint64 `json:"nextNonce"`
}

// Handler 提供提交交易、查询链头、按高度拉块、按哈希取块和查询账户。
func (n *Node) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /tx", n.handleTx)
	mux.HandleFunc("GET /status", n.handleStatus)
	mux.HandleFunc("GET /blocks", n.handleBlocks)
	mux.HandleFunc("GET /block", n.handleBlock)
	mux.HandleFunc("GET /account", n.handleAccount)
	return mux
}

func (n *Node) handleStatus(w http.ResponseWriter, _ *http.Request) {
	head := n.chain.Head()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(statusJSON{
		Height:    head.Height,
		StateRoot: head.StateRoot.Hex(),
		Head:      head.Hash().Hex(),
	})
}

func (n *Node) handleBlocks(w http.ResponseWriter, r *http.Request) {
	var after uint64
	if raw := r.URL.Query().Get("after"); raw != "" {
		n, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		after = n
	}
	blocks := n.chain.BlocksAfter(after)
	out := make([]blockJSON, 0, len(blocks))
	for _, block := range blocks {
		out = append(out, encodeBlock(block))
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (n *Node) handleBlock(w http.ResponseWriter, r *http.Request) {
	hash, err := chain.ParseHash(r.URL.Query().Get("hash"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	block, ok := n.chain.Block(hash)
	if !ok {
		http.Error(w, "unknown block", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(encodeBlock(block))
}

// handleAccount 返回链头上这个地址的余额和 nonce，以及下一笔可用的 nextNonce。
// 地址不存在时余额和 nonce 都是 0。nextNonce 会跳过本机交易池里已经占住的号码。
func (n *Node) handleAccount(w http.ResponseWriter, r *http.Request) {
	addr, err := chain.ParseAddress(r.URL.Query().Get("address"))
	if err != nil {
		http.Error(w, "bad address", http.StatusBadRequest)
		return
	}
	balance, nonce := uint64(0), uint64(0)
	if acct, ok := n.chain.Account(addr); ok {
		balance, nonce = acct.Balance, acct.Nonce
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(accountJSON{
		Address: addr.Hex(), Balance: balance, Nonce: nonce, NextNonce: n.chain.NextNonce(addr),
	})
}

// handleTx 先在本地准入。本地拒绝时返回 400，并且不转发给 peer。
// 本地收下后返回 204。转发给 peer 是尽力而为，对方拒绝或连不上也不改变这个结果。
func (n *Node) handleTx(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	tx, err := decodeTx(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := n.chain.AddTx(tx); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	for _, peer := range n.peers {
		n.forwardTx(peer, body)
	}
	w.WriteHeader(http.StatusNoContent)
}

// forwardTx 把同一份交易 JSON 转给一个 peer。失败时丢掉错误，本地交易仍然保留。
func (n *Node) forwardTx(peer string, body []byte) {
	req, err := http.NewRequest(http.MethodPost, peer+"/tx", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
}

func encodeBlock(block chain.Block) blockJSON {
	txs := make([]txJSON, len(block.Txs))
	for i, tx := range block.Txs {
		item := txJSON{
			Value:    tx.Value,
			Nonce:    tx.Nonce,
			GasLimit: tx.GasLimit,
			GasPrice: tx.GasPrice,
			ChainID:  tx.ChainID,
			Sig:      "0x" + hex.EncodeToString(tx.Sig),
		}
		if !tx.Create {
			item.To = tx.To.Hex()
		}
		if len(tx.Data) > 0 {
			item.Data = "0x" + hex.EncodeToString(tx.Data)
		}
		txs[i] = item
	}
	receipts := make([]receiptJSON, len(block.Receipts))
	for i, receipt := range block.Receipts {
		logs := make([]logJSON, len(receipt.Logs))
		for j, lg := range receipt.Logs {
			topics := make([]string, len(lg.Topics))
			for k, topic := range lg.Topics {
				topics[k] = topic.Hex()
			}
			data := "0x"
			if len(lg.Data) > 0 {
				data = "0x" + hex.EncodeToString(lg.Data)
			}
			logs[j] = logJSON{Address: lg.Address.Hex(), Topics: topics, Data: data}
		}
		receipts[i] = receiptJSON{
			Status:            receipt.Status,
			GasUsed:           receipt.GasUsed,
			CumulativeGasUsed: receipt.CumulativeGasUsed,
			Logs:              logs,
		}
	}
	return blockJSON{
		Height:           block.Height,
		ParentHash:       block.ParentHash.Hex(),
		StateRoot:        block.StateRoot.Hex(),
		TransactionsRoot: block.TransactionsRoot.Hex(),
		ReceiptsRoot:     block.ReceiptsRoot.Hex(),
		Hash:             block.Hash().Hex(),
		Coinbase:         block.Coinbase.Hex(),
		GasLimit:         block.GasLimit,
		GasUsed:          block.GasUsed,
		Time:             block.Time,
		Txs:              txs,
		Receipts:         receipts,
	}
}

func decodeTx(body []byte) (chain.Tx, error) {
	var in txJSON
	if err := json.Unmarshal(body, &in); err != nil {
		return chain.Tx{}, err
	}
	var to chain.Address
	var err error
	create := strings.TrimSpace(in.To) == ""
	if !create {
		to, err = chain.ParseAddress(in.To)
		if err != nil {
			return chain.Tx{}, err
		}
	}
	var data []byte
	if in.Data != "" {
		data, err = decodeHex(in.Data)
		if err != nil {
			return chain.Tx{}, err
		}
	}
	sigBytes, err := decodeHex(in.Sig)
	if err != nil {
		return chain.Tx{}, err
	}
	return chain.Tx{
		To: to, Create: create, Data: data, Value: in.Value, Nonce: in.Nonce,
		GasLimit: in.GasLimit, GasPrice: in.GasPrice, ChainID: in.ChainID,
		Sig: sigBytes,
	}, nil
}

// decodeBlock 还原区块并重算哈希。声明的哈希对不上就拒绝，避免把被改过的块接进链。
func decodeBlock(in blockJSON) (chain.Block, error) {
	parent, err := chain.ParseHash(in.ParentHash)
	if err != nil {
		return chain.Block{}, err
	}
	root, err := chain.ParseHash(in.StateRoot)
	if err != nil {
		return chain.Block{}, err
	}
	var txRoot chain.Hash
	if in.TransactionsRoot != "" {
		txRoot, err = chain.ParseHash(in.TransactionsRoot)
		if err != nil {
			return chain.Block{}, err
		}
	}
	var receiptsRoot chain.Hash
	if in.ReceiptsRoot != "" {
		receiptsRoot, err = chain.ParseHash(in.ReceiptsRoot)
		if err != nil {
			return chain.Block{}, err
		}
	}
	coinbase, err := chain.ParseAddress(in.Coinbase)
	if err != nil {
		return chain.Block{}, err
	}
	txs := make([]chain.Tx, len(in.Txs))
	for i, item := range in.Txs {
		raw, err := json.Marshal(item)
		if err != nil {
			return chain.Block{}, err
		}
		txs[i], err = decodeTx(raw)
		if err != nil {
			return chain.Block{}, err
		}
	}
	receipts := make([]chain.Receipt, len(in.Receipts))
	for i, receipt := range in.Receipts {
		logs := make([]chain.Log, len(receipt.Logs))
		for j, lg := range receipt.Logs {
			addr, err := chain.ParseAddress(lg.Address)
			if err != nil {
				return chain.Block{}, err
			}
			topics := make([]chain.Hash, len(lg.Topics))
			for k, topic := range lg.Topics {
				topics[k], err = chain.ParseHash(topic)
				if err != nil {
					return chain.Block{}, err
				}
			}
			var data []byte
			if lg.Data != "" && lg.Data != "0x" {
				data, err = decodeHex(lg.Data)
				if err != nil {
					return chain.Block{}, err
				}
			}
			logs[j] = chain.Log{Address: addr, Topics: topics, Data: data}
		}
		receipts[i] = chain.Receipt{
			Status:            receipt.Status,
			GasUsed:           receipt.GasUsed,
			CumulativeGasUsed: receipt.CumulativeGasUsed,
			Logs:              logs,
		}
	}
	block := chain.Block{
		Height: in.Height, ParentHash: parent, Coinbase: coinbase,
		GasLimit: in.GasLimit, GasUsed: in.GasUsed, Time: in.Time, Txs: txs, StateRoot: root,
		TransactionsRoot: txRoot, ReceiptsRoot: receiptsRoot, Receipts: receipts,
	}
	if !strings.EqualFold(block.Hash().Hex(), in.Hash) {
		return chain.Block{}, errors.New("block hash mismatch")
	}
	return block, nil
}

func decodeHex(s string) ([]byte, error) {
	s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	return hex.DecodeString(s)
}
