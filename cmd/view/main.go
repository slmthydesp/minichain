// 命令 view 在本机提供看链页面，把浏览器的查询转到指定节点的 /status、/blocks 和 /account。
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"

	"github.com/slmthydesp/minichain/view"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8080", "listen address")
	flag.Parse()
	fmt.Fprintf(os.Stderr, "view: listen http://%s\n", *listen)
	if err := http.ListenAndServe(*listen, view.Handler()); err != nil {
		fmt.Fprintf(os.Stderr, "listen: %v\n", err)
		os.Exit(1)
	}
}
