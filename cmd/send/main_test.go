package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestQueryNextNonce(t *testing.T) {
	const addr = "0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266"
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"address":"` + addr + `","balance":1,"nonce":0,"nextNonce":4}`))
	}))
	t.Cleanup(srv.Close)
	nonce, err := queryNextNonce(srv.URL+"/", addr)
	if err != nil {
		t.Fatal(err)
	}
	if nonce != 4 {
		t.Fatalf("nonce = %d", nonce)
	}
	if got != "/account?address="+addr {
		t.Fatalf("uri %s", got)
	}
}
