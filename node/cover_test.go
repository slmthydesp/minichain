package node

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/slmthydesp/minichain/chain"
)

func TestBlocksAndBlockQueries(t *testing.T) {
	c := chain.New()
	if _, err := c.Mine(chain.Address{}, c.Head().Time+1); err != nil {
		t.Fatal(err)
	}
	n := NewMiner(c)
	srv := httptest.NewServer(n.Handler())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/blocks")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`"height"`)) {
		t.Fatalf("blocks %d %s", resp.StatusCode, body)
	}
	resp, err = http.Get(srv.URL + "/blocks?after=0")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("after status %d", resp.StatusCode)
	}

	resp, err = http.Get(srv.URL + "/block?hash=zz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad hash %d", resp.StatusCode)
	}
	resp, err = http.Get(srv.URL + "/block?hash=0x" + strings.Repeat("11", 32))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing %d", resp.StatusCode)
	}
	resp, err = http.Get(srv.URL + "/block?hash=" + c.Head().Hash().Hex())
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("block %d", resp.StatusCode)
	}
}

func TestHandleTxFailuresAndForward(t *testing.T) {
	n := NewMiner(chain.New())
	rec := httptest.NewRecorder()
	n.handleTx(rec, httptest.NewRequest(http.MethodPost, "/tx", errBody{}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("read %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	n.handleTx(rec, httptest.NewRequest(http.MethodPost, "/tx", strings.NewReader("{")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("json %d", rec.Code)
	}

	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(peer.Close)
	n = New(chain.New(), []string{peer.URL})
	n.forwardTx("://bad", []byte("x"))
	n.forwardTx("http://127.0.0.1:1", []byte("x"))
	n.forwardTx(peer.URL, []byte(`{}`))
}

func TestEncodeAndDecodeBlock(t *testing.T) {
	var addr chain.Address
	addr[19] = 3
	block := chain.Block{
		Height: 3, Time: 9, GasLimit: 1,
		Txs: []chain.Tx{
			{Create: true, Data: []byte{0x60}, Sig: []byte{1, 2}},
			{To: addr, Data: []byte{0x01}, Sig: []byte{3}},
		},
		Receipts: []chain.Receipt{{
			Status: 1,
			Logs: []chain.Log{
				{Address: addr, Topics: []chain.Hash{{1: 2}}, Data: []byte{0xab}},
				{Address: addr},
			},
		}},
	}
	item := encodeBlock(block)
	if item.Txs[0].To != "" || item.Txs[1].Data != "0x01" {
		t.Fatalf("encoded %#v", item.Txs)
	}
	if _, err := decodeBlock(item); err != nil {
		t.Fatal(err)
	}

	bad := item
	bad.ParentHash = "zz"
	if _, err := decodeBlock(bad); err == nil {
		t.Fatal("parent")
	}
	bad = item
	bad.StateRoot = "zz"
	if _, err := decodeBlock(bad); err == nil {
		t.Fatal("state")
	}
	bad = item
	bad.TransactionsRoot = "zz"
	if _, err := decodeBlock(bad); err == nil {
		t.Fatal("txs")
	}
	bad = item
	bad.ReceiptsRoot = "zz"
	if _, err := decodeBlock(bad); err == nil {
		t.Fatal("receipts")
	}
	bad = item
	bad.Coinbase = "zz"
	if _, err := decodeBlock(bad); err == nil {
		t.Fatal("coinbase")
	}
	bad = item
	bad.Txs = []txJSON{{To: "zz", Sig: "0x01"}}
	if _, err := decodeBlock(bad); err == nil {
		t.Fatal("tx")
	}
	bad = item
	bad.Receipts[0].Logs[0].Address = "zz"
	if _, err := decodeBlock(bad); err == nil {
		t.Fatal("log address")
	}
	bad = item
	bad.Receipts = []receiptJSON{{Logs: []logJSON{{Address: addr.Hex(), Topics: []string{"zz"}}}}}
	if _, err := decodeBlock(bad); err == nil {
		t.Fatal("topic")
	}
	bad = item
	bad.Receipts = []receiptJSON{{Logs: []logJSON{{Address: addr.Hex(), Data: "0xzz"}}}}
	if _, err := decodeBlock(bad); err == nil {
		t.Fatal("log data")
	}
	bad = item
	bad.Time++
	if _, err := decodeBlock(bad); err == nil {
		t.Fatal("hash")
	}
}

func TestDecodeTxFailures(t *testing.T) {
	if _, err := decodeTx([]byte("{")); err == nil {
		t.Fatal("json")
	}
	raw, _ := json.Marshal(txJSON{To: "zz", Sig: "0x01"})
	if _, err := decodeTx(raw); err == nil {
		t.Fatal("to")
	}
	raw, _ = json.Marshal(txJSON{Data: "0xzz", Sig: "0x01"})
	if _, err := decodeTx(raw); err == nil {
		t.Fatal("data")
	}
	raw, _ = json.Marshal(txJSON{Sig: "0xzz"})
	if _, err := decodeTx(raw); err == nil {
		t.Fatal("sig")
	}
}

type errBody struct{}

func (errBody) Read([]byte) (int, error) { return 0, errors.New("read") }
func (errBody) Close() error             { return nil }

func TestFollowPeerFailures(t *testing.T) {
	n := NewMiner(chain.New())
	if n.Chain() == nil {
		t.Fatal("chain")
	}
	if err := n.FollowOnce(context.Background()); err == nil {
		t.Fatal("expected not a follower")
	}
	n = New(chain.New(), []string{"http://127.0.0.1:1", "http://127.0.0.1:2"})
	if err := n.FollowOnce(context.Background()); err == nil {
		t.Fatal("expected dial error")
	}

	status := func(code int, body string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/block" {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte("missing"))
				return
			}
			w.WriteHeader(code)
			_, _ = w.Write([]byte(body))
		}))
	}
	for _, srv := range []*httptest.Server{
		status(http.StatusBadRequest, "nope"),
		status(http.StatusOK, "{"),
		status(http.StatusOK, `{"head":"zz"}`),
		status(http.StatusOK, `{"head":"0x`+strings.Repeat("ab", 32)+`"}`),
	} {
		err := NewFollower(chain.New(), srv.URL).FollowOnce(context.Background())
		srv.Close()
		if err == nil {
			t.Fatal("expected follow error")
		}
	}

	n = NewFollower(chain.New(), "http://node")
	n.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(errBody{}),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})}
	if _, _, err := n.get(context.Background(), "http://node/status"); err == nil {
		t.Fatal("expected body error")
	}
	if _, _, err := n.get(context.Background(), "http://a b"); err == nil {
		t.Fatal("expected bad url")
	}

	b := chain.Block{Height: 0, Time: 1, GasLimit: chain.BlockGasLimit, Coinbase: chain.Address{19: 9}}
	item := encodeBlock(b)
	raw, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/block" {
			_, _ = w.Write(raw)
			return
		}
		_, _ = w.Write([]byte(`{"head":"` + item.Hash + `"}`))
	}))
	t.Cleanup(srv.Close)
	if err := NewFollower(chain.New(), srv.URL).FollowOnce(context.Background()); err == nil {
		t.Fatal("expected append error")
	}

	badBlock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/block" {
			_, _ = w.Write([]byte("{"))
			return
		}
		_, _ = w.Write([]byte(`{"head":"0x` + strings.Repeat("cd", 32) + `"}`))
	}))
	t.Cleanup(badBlock.Close)
	if err := NewFollower(chain.New(), badBlock.URL).FollowOnce(context.Background()); err == nil {
		t.Fatal("expected bad block json")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
