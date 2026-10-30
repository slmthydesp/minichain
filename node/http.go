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

type txJSON struct {
	To       string `json:"to"`
	Value    uint64 `json:"value"`
	Nonce    uint64 `json:"nonce"`
	GasLimit uint64 `json:"gasLimit"`
	GasPrice uint64 `json:"gasPrice"`
	ChainID  uint64 `json:"chainId"`
	Sig      string `json:"sig"`
}

type receiptJSON struct {
	Status            uint64 `json:"status"`
	GasUsed           uint64 `json:"gasUsed"`
	CumulativeGasUsed uint64 `json:"cumulativeGasUsed"`
}

type blockJSON struct {
	Height       uint64        `json:"height"`
	ParentHash   string        `json:"parentHash"`
	StateRoot    string        `json:"stateRoot"`
	ReceiptsRoot string        `json:"receiptsRoot"`
	Hash         string        `json:"hash"`
	Coinbase     string        `json:"coinbase"`
	GasLimit     uint64        `json:"gasLimit"`
	GasUsed      uint64        `json:"gasUsed"`
	Txs          []txJSON      `json:"txs"`
	Receipts     []receiptJSON `json:"receipts"`
}

type statusJSON struct {
	Height    uint64 `json:"height"`
	StateRoot string `json:"stateRoot"`
	Head      string `json:"head"`
}

func (n *Node) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /tx", n.handleTx)
	mux.HandleFunc("GET /status", n.handleStatus)
	mux.HandleFunc("GET /blocks", n.handleBlocks)
	mux.HandleFunc("GET /block", n.handleBlock)
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
		txs[i] = txJSON{
			To:       tx.To.Hex(),
			Value:    tx.Value,
			Nonce:    tx.Nonce,
			GasLimit: tx.GasLimit,
			GasPrice: tx.GasPrice,
			ChainID:  tx.ChainID,
			Sig:      "0x" + hex.EncodeToString(tx.Sig),
		}
	}
	receipts := make([]receiptJSON, len(block.Receipts))
	for i, receipt := range block.Receipts {
		receipts[i] = receiptJSON{
			Status:            receipt.Status,
			GasUsed:           receipt.GasUsed,
			CumulativeGasUsed: receipt.CumulativeGasUsed,
		}
	}
	return blockJSON{
		Height:       block.Height,
		ParentHash:   block.ParentHash.Hex(),
		StateRoot:    block.StateRoot.Hex(),
		ReceiptsRoot: block.ReceiptsRoot.Hex(),
		Hash:         block.Hash().Hex(),
		Coinbase:     block.Coinbase.Hex(),
		GasLimit:     block.GasLimit,
		GasUsed:      block.GasUsed,
		Txs:          txs,
		Receipts:     receipts,
	}
}

func decodeTx(body []byte) (chain.Tx, error) {
	var in txJSON
	if err := json.Unmarshal(body, &in); err != nil {
		return chain.Tx{}, err
	}
	to, err := chain.ParseAddress(in.To)
	if err != nil {
		return chain.Tx{}, err
	}
	sigBytes, err := decodeHex(in.Sig)
	if err != nil {
		return chain.Tx{}, err
	}
	return chain.Tx{
		To: to, Value: in.Value, Nonce: in.Nonce,
		GasLimit: in.GasLimit, GasPrice: in.GasPrice, ChainID: in.ChainID,
		Sig: sigBytes,
	}, nil
}

func decodeBlock(in blockJSON) (chain.Block, error) {
	parent, err := chain.ParseHash(in.ParentHash)
	if err != nil {
		return chain.Block{}, err
	}
	root, err := chain.ParseHash(in.StateRoot)
	if err != nil {
		return chain.Block{}, err
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
		receipts[i] = chain.Receipt{
			Status:            receipt.Status,
			GasUsed:           receipt.GasUsed,
			CumulativeGasUsed: receipt.CumulativeGasUsed,
		}
	}
	block := chain.Block{
		Height: in.Height, ParentHash: parent, Coinbase: coinbase,
		GasLimit: in.GasLimit, GasUsed: in.GasUsed, Txs: txs, StateRoot: root,
		ReceiptsRoot: receiptsRoot, Receipts: receipts,
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
