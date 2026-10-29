package node

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	tx, err := chain.Sign(key, to, value, nonce)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{
		"to":    tx.To.Hex(),
		"value": tx.Value,
		"nonce": tx.Nonce,
		"sig":   "0x" + hex.EncodeToString(tx.Sig),
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

func TestFollowerCatchesMinerAndExitsWhenMinerStops(t *testing.T) {
	minerChain := chain.New()
	miner := NewMiner(minerChain)
	srv := httptest.NewServer(miner.Handler())
	t.Cleanup(srv.Close)

	resp := postTx(t, srv.URL, signedBody(t, 0, 20))
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if _, err := minerChain.Mine(); err != nil {
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
	if _, err := minerChain.Mine(); err != nil {
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
