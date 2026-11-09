// 命令 node 启动一个进程：定时出块，并在配置了 --peers 时轮询对方的链。
// 没写 --listen 时，单独运行听 8545；写了 --peers 则听 8546，方便本机再开第二个进程。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/slmthydesp/minichain/chain"
	"github.com/slmthydesp/minichain/node"
)

var (
	exitFunc  = os.Exit
	fatalFunc = log.Fatal
	unixNow   = func() uint64 { return uint64(time.Now().Unix()) }
	mineAt    = func(c *chain.Chain, coinbase chain.Address, timestamp uint64) (chain.Block, error) {
		return c.Mine(coinbase, timestamp)
	}
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		exitFunc(1)
	}
}

func run(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("node", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	listen := fs.String("listen", "", "listen address")
	interval := fs.Duration("interval", 5*time.Second, "block interval")
	peersFlag := fs.String("peers", "", "comma-separated peer URLs")
	poll := fs.Duration("poll", 200*time.Millisecond, "block poll interval")
	coinbaseFlag := fs.String("coinbase", "0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266", "block coinbase")
	if err := fs.Parse(args); err != nil {
		return err
	}

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
		return fmt.Errorf("coinbase: %v", err)
	}
	c := chain.New()
	n := node.New(c, peers)
	printStartup(addr, coinbase.Hex(), *interval, *poll, peers)
	srv := &http.Server{Addr: addr, Handler: n.Handler()}
	go func() {
		for {
			if ctx.Err() != nil {
				return
			}
			head := c.Head()
			now := unixNow()
			if now <= head.Time {
				now = head.Time + 1
			}
			if _, err := mineAt(c, coinbase, now); err != nil {
				fmt.Fprintf(os.Stderr, "mine: %v\n", err)
				exitFunc(1)
				return
			}
			if !sleepCtx(ctx, *interval) {
				return
			}
		}
	}()
	// 没有 peer 时，监听失败直接退出。有 peer 时，同步失败只打印，出块继续。
	if len(peers) == 0 {
		fatalFunc(srv.ListenAndServe())
		return nil
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "listen: %v\n", err)
			exitFunc(1)
		}
	}()
	// 连不上或重新连上时各打一条。同一种状态的后续轮询不再重复打。
	wasDown := false
	for {
		if ctx.Err() != nil {
			_ = srv.Shutdown(context.Background())
			return nil
		}
		err := n.FollowOnce(ctx)
		if err != nil {
			if !wasDown {
				fmt.Fprintf(os.Stderr, "sync: %v\n", err)
				wasDown = true
			}
		} else if wasDown {
			fmt.Fprintf(os.Stderr, "sync: connected %s\n", strings.Join(peers, ", "))
			wasDown = false
		}
		if !sleepCtx(ctx, *poll) {
			_ = srv.Shutdown(context.Background())
			return nil
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
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
