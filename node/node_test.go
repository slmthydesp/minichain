package node

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/slmthydesp/minichain/chain"
	"github.com/slmthydesp/minichain/sig"
)

func signedBody(t *testing.T, nonce, value uint64) []byte {
	t.Helper()
	key, err := sig.ParseKey(sig.DemoKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	var to chain.Address
	to[19] = 1
	tx, err := chain.Sign(key, to, value, nonce, 21000, 1, chain.ChainID)
	if err != nil {
		t.Fatal(err)
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
		t.Fatal(err)
	}
	return body
}

func postTx(t *testing.T, url string, body []byte) *http.Response {
	t.Helper()
	resp, err := http.Post(url+"/tx", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestFollowerCatchesMinerAndErrorsWhenMinerStops(t *testing.T) {
	minerChain := chain.New()
	miner := NewMiner(minerChain)
	srv := httptest.NewServer(miner.Handler())
	t.Cleanup(srv.Close)

	resp := postTx(t, srv.URL, signedBody(t, 0, 20))
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if _, err := minerChain.Mine(chain.Address{}); err != nil {
		t.Fatal(err)
	}

	followerChain := chain.New()
	follower := NewFollower(followerChain, srv.URL)
	if err := follower.FollowOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if followerChain.Head().Height != minerChain.Head().Height {
		t.Fatalf("height follower=%d miner=%d", followerChain.Head().Height, minerChain.Head().Height)
	}
	if followerChain.Head().StateRoot != minerChain.Head().StateRoot {
		t.Fatal("state roots differ")
	}

	srv.Close()
	if err := follower.FollowOnce(context.Background()); err == nil {
		t.Fatal("expected error after miner stopped")
	}
}

func TestFollowerAcceptsTransactionAndForwardsIt(t *testing.T) {
	minerChain := chain.New()
	miner := NewMiner(minerChain)
	minerSrv := httptest.NewServer(miner.Handler())
	t.Cleanup(minerSrv.Close)

	followerChain := chain.New()
	follower := NewFollower(followerChain, minerSrv.URL)
	followerSrv := httptest.NewServer(follower.Handler())
	t.Cleanup(followerSrv.Close)

	resp := postTx(t, followerSrv.URL, signedBody(t, 0, 7))
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if len(minerChain.Pool().Pending()) != 1 || len(followerChain.Pool().Pending()) != 1 {
		t.Fatalf("pools miner=%d follower=%d", len(minerChain.Pool().Pending()), len(followerChain.Pool().Pending()))
	}
	if _, err := minerChain.Mine(chain.Address{}); err != nil {
		t.Fatal(err)
	}
	if err := follower.FollowOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if followerChain.Head().StateRoot != minerChain.Head().StateRoot {
		t.Fatal("state roots differ")
	}
	if len(minerChain.Pool().Pending()) != 0 || len(followerChain.Pool().Pending()) != 0 {
		t.Fatal("transaction remained in a pool")
	}
}

func TestStatusReportsLocalHead(t *testing.T) {
	c := chain.New()
	srv := httptest.NewServer(NewMiner(c).Handler())
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL + "/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got struct {
		Height    uint64 `json:"height"`
		StateRoot string `json:"stateRoot"`
		Head      string `json:"head"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	head := c.Head()
	if got.Height != head.Height || got.StateRoot != head.StateRoot.Hex() || got.Head != head.Hash().Hex() {
		t.Fatalf("status = %+v", got)
	}
}

func TestBlocksRejectsTrailingAfter(t *testing.T) {
	srv := httptest.NewServer(NewMiner(chain.New()).Handler())
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL + "/blocks?after=12abc")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestFollowerRejectsLocallyWithoutForwarding(t *testing.T) {
	minerChain := chain.New()
	minerSrv := httptest.NewServer(NewMiner(minerChain).Handler())
	t.Cleanup(minerSrv.Close)
	followerChain := chain.New()
	followerSrv := httptest.NewServer(NewFollower(followerChain, minerSrv.URL).Handler())
	t.Cleanup(followerSrv.Close)

	var payload map[string]any
	if err := json.Unmarshal(signedBody(t, 0, 7), &payload); err != nil {
		t.Fatal(err)
	}
	payload["sig"] = "0x" + strings.Repeat("00", 65)
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	resp := postTx(t, followerSrv.URL, body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if len(minerChain.Pool().Pending()) != 0 || len(followerChain.Pool().Pending()) != 0 {
		t.Fatal("rejected transaction was kept or forwarded")
	}
}

func TestFollowerDropsWhenMinerRejects(t *testing.T) {
	minerChain := chain.New()
	minerSrv := httptest.NewServer(NewMiner(minerChain).Handler())
	t.Cleanup(minerSrv.Close)
	body := signedBody(t, 0, 5)
	resp := postTx(t, minerSrv.URL, body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("miner status = %d", resp.StatusCode)
	}
	if _, err := minerChain.Mine(chain.Address{}); err != nil {
		t.Fatal(err)
	}

	followerChain := chain.New()
	followerSrv := httptest.NewServer(NewFollower(followerChain, minerSrv.URL).Handler())
	t.Cleanup(followerSrv.Close)
	resp = postTx(t, followerSrv.URL, body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if len(followerChain.Pool().Pending()) != 1 {
		t.Fatal("follower dropped a locally valid transaction")
	}
	follower := NewFollower(followerChain, minerSrv.URL)
	if err := follower.FollowOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(followerChain.Pool().Pending()) != 0 {
		t.Fatal("included transaction remained pending after sync")
	}
}

func TestFollowerDropsWhenMinerUnreachable(t *testing.T) {
	minerSrv := httptest.NewServer(http.NotFoundHandler())
	url := minerSrv.URL
	minerSrv.Close()
	followerChain := chain.New()
	followerSrv := httptest.NewServer(NewFollower(followerChain, url).Handler())
	t.Cleanup(followerSrv.Close)
	resp := postTx(t, followerSrv.URL, signedBody(t, 0, 1))
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if len(followerChain.Pool().Pending()) != 1 {
		t.Fatal("follower dropped a transaction it could not forward")
	}
}

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFollowerReportsBodyReadError(t *testing.T) {
	followerChain := chain.New()
	follower := NewFollower(followerChain, "http://miner.invalid")
	follower.client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Body:       io.NopCloser(errReader{errors.New("read failed")}),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})}
	srv := httptest.NewServer(follower.Handler())
	t.Cleanup(srv.Close)
	resp := postTx(t, srv.URL, signedBody(t, 0, 1))
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d body = %s", resp.StatusCode, body)
	}
	if len(followerChain.Pool().Pending()) != 1 {
		t.Fatal("follower dropped a transaction after a failed forward read")
	}
}

func TestFollowerSwitchesToLongerFork(t *testing.T) {
	var coinA, coinB chain.Address
	coinA[19] = 1
	coinB[19] = 2

	chainA := chain.New()
	srvA := httptest.NewServer(NewMiner(chainA).Handler())
	t.Cleanup(srvA.Close)
	if _, err := chainA.Mine(coinA); err != nil {
		t.Fatal(err)
	}

	chainB := chain.New()
	srvB := httptest.NewServer(NewMiner(chainB).Handler())
	t.Cleanup(srvB.Close)
	if _, err := chainB.Mine(coinB); err != nil {
		t.Fatal(err)
	}
	if _, err := chainB.Mine(coinB); err != nil {
		t.Fatal(err)
	}

	followerChain := chain.New()
	follower := NewFollower(followerChain, srvA.URL)
	if err := follower.FollowOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if followerChain.Head().Hash() != chainA.Head().Hash() {
		t.Fatal("follower did not adopt miner A")
	}

	follower = NewFollower(followerChain, srvB.URL)
	if err := follower.FollowOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if followerChain.Head().Hash() != chainB.Head().Hash() || followerChain.Head().StateRoot != chainB.Head().StateRoot {
		t.Fatal("follower did not adopt the longer fork")
	}
	if !followerChain.Has(chainA.Head().Hash()) {
		t.Fatal("shorter fork was discarded")
	}
}
