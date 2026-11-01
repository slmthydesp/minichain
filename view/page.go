package view

import (
	_ "embed"
	"net/http"
)

//go:embed static/index.html
var indexHTML []byte

// handleIndex 返回嵌入的静态页面。页面自己填写要看的节点地址。
func handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(indexHTML)
}
