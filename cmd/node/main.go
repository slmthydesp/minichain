// 命令 node 启动一个进程：定时出块，并在配置了 --peers 时轮询对方的链。
// 没写 --listen 时，单独运行听 8545；写了 --peers 则听 8546，方便本机再开第二个进程。
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
	interval := flag.Duration("interval", 5*time.Second, "block interval")
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
	printStartup(addr, coinbase.Hex(), *interval, *poll, peers)
	srv := &http.Server{Addr: addr, Handler: n.Handler()}
	go func() {
		for {
			head := c.Head()
			now := uint64(time.Now().Unix())
			if now <= head.Time {
				now = head.Time + 1
			}
			if _, err := c.Mine(coinbase, now); err != nil {
				fmt.Fprintf(os.Stderr, "mine: %v\n", err)
				os.Exit(1)
			}
			time.Sleep(*interval)
		}
	}()
	// 没有 peer 时，监听失败直接退出。有 peer 时，同步失败只打印，出块继续。
	if len(peers) == 0 {
		log.Fatal(srv.ListenAndServe())
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "listen: %v\n", err)
			os.Exit(1)
		}
	}()
	// 连不上或重新连上时各打一条。同一种状态的后续轮询不再重复打。
	wasDown := false
	for {
		err := n.FollowOnce(context.Background())
		if err != nil {
			if !wasDown {
				fmt.Fprintf(os.Stderr, "sync: %v\n", err)
				wasDown = true
			}
		} else if wasDown {
			fmt.Fprintf(os.Stderr, "sync: connected %s\n", strings.Join(peers, ", "))
			wasDown = false
		}
		time.Sleep(*poll)
	}
}

// printStartup 在出块和同步开始前打印这次进程实际使用的参数。
func printStartup(addr, coinbase string, interval, poll time.Duration, peers []string) {
	fmt.Fprintf(os.Stderr, "node: listen %s\n", addr)
	fmt.Fprintf(os.Stderr, "node: coinbase %s\n", coinbase)
	fmt.Fprintf(os.Stderr, "node: interval %s\n", interval)
	fmt.Fprintf(os.Stderr, "node: chain %d\n", chain.ChainID)
	if len(peers) == 0 {
		fmt.Fprintf(os.Stderr, "node: peers none\n")
		return
	}
	fmt.Fprintf(os.Stderr, "node: peers %s\n", strings.Join(peers, ", "))
	fmt.Fprintf(os.Stderr, "node: poll %s\n", poll)
}

// splitPeers 按逗号拆开 peer 地址，丢掉空白项。
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
