package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/slmthydesp/minichain/chain"
	"github.com/slmthydesp/minichain/node"
)

func TestSplitPeers(t *testing.T) {
	if splitPeers("  ") != nil {
		t.Fatal("blank peers")
	}
	got := splitPeers(" , http://a , , http://b ")
	if len(got) != 2 || got[0] != "http://a" || got[1] != "http://b" {
		t.Fatalf("peers %v", got)
	}
}

func TestRunRejectsFlagAndCoinbase(t *testing.T) {
	if err := run(context.Background(), []string{"-nope"}); err == nil {
		t.Fatal("expected flag error")
	}
	if err := run(context.Background(), []string{"-coinbase", "zz"}); err == nil {
		t.Fatal("expected coinbase error")
	}
}

func TestRunMinerListenFailure(t *testing.T) {
	origFatal := fatalFunc
	t.Cleanup(func() { fatalFunc = origFatal })
	fatalFunc = func(v ...any) {}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := run(ctx, []string{"-listen", "127.0.0.1:99999", "-interval", "1h"}); err != nil {
		t.Fatal(err)
	}
}

func TestRunFollowerSyncAndListenError(t *testing.T) {
	miner := httptest.NewServer(node.NewMiner(chain.New()).Handler())
	t.Cleanup(miner.Close)
	var hits atomic.Int32
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		miner.Config.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(peer.Close)

	origExit := exitFunc
	t.Cleanup(func() { exitFunc = origExit })
	exited := make(chan int, 1)
	exitFunc = func(code int) { exited <- code }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{
			"-listen", "127.0.0.1:99999",
			"-peers", " , " + peer.URL + " , ",
			"-poll", "5ms",
			"-interval", "1h",
		})
	}()
	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for hits.Load() < 2 && time.Now().Before(deadline) {
			time.Sleep(2 * time.Millisecond)
		}
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("run did not return")
	}
	select {
	case code := <-exited:
		if code != 1 {
			t.Fatalf("exit %d", code)
		}
	default:
		t.Fatal("listen error did not exit")
	}
}

func TestRunDefaultPeerPortAndMineFailure(t *testing.T) {
	origNow, origMine, origExit, origFatal := unixNow, mineAt, exitFunc, fatalFunc
	t.Cleanup(func() {
		unixNow, mineAt, exitFunc, fatalFunc = origNow, origMine, origExit, origFatal
	})
	unixNow = func() uint64 { return 0 }
	mineAt = func(*chain.Chain, chain.Address, uint64) (chain.Block, error) {
		return chain.Block{}, errors.New("boom")
	}
	exitFunc = func(int) {}
	fatalFunc = func(...any) {}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{"-peers", "http://127.0.0.1:1", "-poll", "1h", "-interval", "1h"})
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("run did not return")
	}
}

func TestMainExitsOnError(t *testing.T) {
	origArgs := os.Args
	origExit := exitFunc
	t.Cleanup(func() {
		os.Args = origArgs
		exitFunc = origExit
	})
	os.Args = []string{"node", "-nope"}
	exitFunc = func(code int) {
		if code != 1 {
			t.Errorf("code %d", code)
		}
		panic("exit")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("expected exit")
		}
	}()
	main()
}
