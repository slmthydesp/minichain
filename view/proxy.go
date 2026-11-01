package view

import (
	"io"
	"net/http"
	"strconv"
	"time"
)

var upstreamClient = &http.Client{
	Timeout: 3 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// Handler 只接受 GET：页面、链头、区块列表和账户。其他方法一律 404。
func Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", handleIndex)
	mux.HandleFunc("GET /api/status", handleStatus)
	mux.HandleFunc("GET /api/blocks", handleBlocks)
	mux.HandleFunc("GET /api/account", handleAccount)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	node := r.URL.Query().Get("node")
	if _, err := CanonicalNode(node); err != nil {
		http.Error(w, "bad node", http.StatusBadRequest)
		return
	}
	forward(w, node, "/status")
}

func handleBlocks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	node := q.Get("node")
	if _, err := CanonicalNode(node); err != nil {
		http.Error(w, "bad node", http.StatusBadRequest)
		return
	}
	path := "/blocks"
	if _, ok := q["after"]; ok {
		n, err := strconv.ParseUint(q.Get("after"), 10, 64)
		if err != nil {
			http.Error(w, "bad after", http.StatusBadRequest)
			return
		}
		path = "/blocks?after=" + strconv.FormatUint(n, 10)
	}
	forward(w, node, path)
}

func handleAccount(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	node := q.Get("node")
	if _, err := CanonicalNode(node); err != nil {
		http.Error(w, "bad node", http.StatusBadRequest)
		return
	}
	addr, err := CanonicalAddress(q.Get("address"))
	if err != nil {
		http.Error(w, "bad address", http.StatusBadRequest)
		return
	}
	forward(w, node, "/account?address="+addr)
}

// forward 把请求转到 http://节点 的对应路径，并原样返回状态码和正文。
// 不跟随重定向，这样页面看到的是上游的 3xx，而不是被带到别的地址。
func forward(w http.ResponseWriter, node, path string) {
	canonical, err := CanonicalNode(node)
	if err != nil {
		http.Error(w, "bad node", http.StatusBadRequest)
		return
	}
	req, err := http.NewRequest(http.MethodGet, "http://"+canonical+path, nil)
	if err != nil {
		http.Error(w, "bad node", http.StatusBadRequest)
		return
	}
	resp, err := upstreamClient.Do(req)
	if err != nil {
		http.Error(w, "unreachable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "unreachable", http.StatusBadGateway)
		return
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
}
