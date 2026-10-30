package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/slmthydesp/minichain/chain"
	"github.com/slmthydesp/minichain/node"
)

func main() {
	listen := flag.String("listen", "", "listen address")
	interval := flag.Duration("interval", time.Second, "block interval")
	peersFlag := flag.String("peers", "", "comma-separated peer URLs")
	poll := flag.Duration("poll", 200*time.Millisecond, "block poll interval")
	coinbaseFlag := flag.String("coinbase", "0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266", "block coinbase")
	flag.Parse()

	peers := splitPeers(*peersFlag)
	addr := *listen
	if addr == "" {
		addr = "127.0.0.1:8545"
		if len(peers) > 0 {
			addr = "127.0.0.1:8546"
		}
	}
	coinbase, err := chain.ParseAddress(*coinbaseFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "coinbase: %v\n", err)
		os.Exit(1)
	}
	c := chain.New()
	n := node.New(c, peers)
	srv := &http.Server{Addr: addr, Handler: n.Handler()}
	go func() {
		for {
			if _, err := c.Mine(coinbase); err != nil {
				fmt.Fprintf(os.Stderr, "mine: %v\n", err)
				os.Exit(1)
			}
			time.Sleep(*interval)
		}
	}()
	if len(peers) == 0 {
		log.Fatal(srv.ListenAndServe())
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "listen: %v\n", err)
			os.Exit(1)
		}
	}()
	for {
		if err := n.FollowOnce(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "sync: %v\n", err)
		}
		time.Sleep(*poll)
	}
}

func splitPeers(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
