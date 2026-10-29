package node

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/slmthydesp/minichain/chain"
)

type txJSON struct {
	To    string `json:"to"`
	Value uint64 `json:"value"`
	Nonce uint64 `json:"nonce"`
	Sig   string `json:"sig"`
}

type blockJSON struct {
	Height     uint64   `json:"height"`
	ParentHash string   `json:"parentHash"`
	StateRoot  string   `json:"stateRoot"`
	Hash       string   `json:"hash"`
	Txs        []txJSON `json:"txs"`
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
	if n.peer == "" {
		mux.HandleFunc("GET /blocks", n.handleBlocks)
	}
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
		if _, err := fmt.Sscan(raw, &after); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	blocks := n.chain.BlocksAfter(after)
	out := make([]blockJSON, 0, len(blocks))
	for _, block := range blocks {
		out = append(out, encodeBlock(block))
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
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
	sender, err := n.chain.AddTx(tx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if n.peer == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	req, err := http.NewRequest(http.MethodPost, n.peer+"/tx", bytes.NewReader(body))
	if err != nil {
		n.chain.DropTx(sender, tx.Nonce)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		n.chain.DropTx(sender, tx.Nonce)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusNoContent {
		n.chain.DropTx(sender, tx.Nonce)
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(respBody)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func encodeBlock(block chain.Block) blockJSON {
	txs := make([]txJSON, len(block.Txs))
	for i, tx := range block.Txs {
		txs[i] = txJSON{
			To:    tx.To.Hex(),
			Value: tx.Value,
			Nonce: tx.Nonce,
			Sig:   "0x" + hex.EncodeToString(tx.Sig),
		}
	}
	return blockJSON{
		Height:     block.Height,
		ParentHash: block.ParentHash.Hex(),
		StateRoot:  block.StateRoot.Hex(),
		Hash:       block.Hash().Hex(),
		Txs:        txs,
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
	return chain.Tx{To: to, Value: in.Value, Nonce: in.Nonce, Sig: sigBytes}, nil
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
	block := chain.Block{Height: in.Height, ParentHash: parent, Txs: txs, StateRoot: root}
	if !strings.EqualFold(block.Hash().Hex(), in.Hash) {
		return chain.Block{}, errors.New("block hash mismatch")
	}
	return block, nil
}

func decodeHex(s string) ([]byte, error) {
	s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	return hex.DecodeString(s)
}
