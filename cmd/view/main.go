// 命令 view 在本机提供看链页面，把浏览器的查询转到指定节点的 /status、/blocks 和 /account。
package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/slmthydesp/minichain/view"
)

var (
	exitFunc       = os.Exit
	listenAndServe = http.ListenAndServe
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		exitFunc(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("view", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	listen := fs.String("listen", "127.0.0.1:8080", "listen address")
	if err := fs.Parse(args); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "view: listen http://%s\n", *listen)
	if err := listenAndServe(*listen, view.Handler()); err != nil {
		return fmt.Errorf("listen: %v", err)
	}
	return nil
}
