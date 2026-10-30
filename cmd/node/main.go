package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/slmthydesp/minichain/chain"
	"github.com/slmthydesp/minichain/node"
)

func main() {
	listen := flag.String("listen", "", "listen address")
	interval := flag.Duration("interval", time.Second, "block interval")
	peer := flag.String("peer", "", "block producer URL")
	poll := flag.Duration("poll", 200*time.Millisecond, "block poll interval")
	flag.Parse()

	addr := *listen
	if addr == "" {
		addr = "127.0.0.1:8545"
		if *peer != "" {
			addr = "127.0.0.1:8546"
		}
	}
	c := chain.New()
	if *peer == "" {
		n := node.NewMiner(c)
		go func() {
			for {
				if _, err := c.Mine(chain.Address{}); err != nil {
					fmt.Fprintf(os.Stderr, "mine: %v\n", err)
					os.Exit(1)
				}
				time.Sleep(*interval)
			}
		}()
		log.Fatal((&http.Server{Addr: addr, Handler: n.Handler()}).ListenAndServe())
	}

	n := node.NewFollower(c, *peer)
	srv := &http.Server{Addr: addr, Handler: n.Handler()}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "listen: %v\n", err)
			os.Exit(1)
		}
	}()
	for {
		if err := n.FollowOnce(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "sync: %v\n", err)
			os.Exit(1)
		}
		time.Sleep(*poll)
	}
}
