package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestQueryNextNonceErrors(t *testing.T) {
	if _, err := queryNextNonce("http://127.0.0.1:1", "0xabc"); err == nil {
		t.Fatal("expected dial error")
	}
	statusSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusBadRequest)
	}))
	t.Cleanup(statusSrv.Close)
	if _, err := queryNextNonce(statusSrv.URL, "0xabc"); err == nil {
		t.Fatal("expected status error")
	}
	badJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("{"))
	}))
	t.Cleanup(badJSON.Close)
	if _, err := queryNextNonce(badJSON.URL, "0xabc"); err == nil {
		t.Fatal("expected json error")
	}

	orig := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = orig })
	http.DefaultTransport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(errReader{}),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})
	if _, err := queryNextNonce("http://node", "0xabc"); err == nil {
		t.Fatal("expected body error")
	}
}

func TestRunSendPaths(t *testing.T) {
	if err := run([]string{"-unknown"}); err == nil {
		t.Fatal("expected flag error")
	}
	if err := run(nil); err == nil || !strings.Contains(err.Error(), "url is required") {
		t.Fatalf("err %v", err)
	}
	if err := run([]string{"-url", "http://node", "-key", "zz"}); err == nil {
		t.Fatal("expected bad key")
	}
	if err := run([]string{"-url", "http://node", "-key", "aa"}); err == nil {
		t.Fatal("expected short key")
	}
	if err := run([]string{"-url", "http://127.0.0.1:1", "-nonce", "nope"}); err == nil {
		t.Fatal("expected bad nonce")
	}
	const to = "0x0000000000000000000000000000000000000001"
	if err := run([]string{"-url", "http://node", "-to", "zz", "-nonce", "0"}); err == nil {
		t.Fatal("expected bad to")
	}
	if err := run([]string{"-url", "http://node", "-nonce", "0", "-data", "zz"}); err == nil {
		t.Fatal("expected bad data")
	}
	closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	closed.Close()
	if err := run([]string{"-url", closed.URL}); err == nil || !strings.Contains(err.Error(), "nonce:") {
		t.Fatalf("nonce query %v", err)
	}
	if err := run([]string{"-url", closed.URL, "-nonce", "1", "-to", to}); err == nil {
		t.Fatal("expected post error")
	}

	var sawTo string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/account" {
			_, _ = w.Write([]byte(`{"nextNonce":3}`))
			return
		}
		body, _ := io.ReadAll(r.Body)
		sawTo = string(body)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	if err := run([]string{"-url", srv.URL, "-to", to, "-value", "1"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sawTo, `"nonce":3`) {
		t.Fatalf("body %s", sawTo)
	}
	if err := run([]string{"-url", srv.URL, "-to", to, "-nonce", "1", "-data", "0x61"}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"-url", srv.URL, "-nonce", "1", "-data", "0X60"}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"-url", srv.URL, "-nonce", "1"}); err != nil {
		t.Fatal(err)
	}

	rejected := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", http.StatusBadRequest)
	}))
	t.Cleanup(rejected.Close)
	if err := run([]string{"-url", rejected.URL, "-to", to, "-nonce", "0"}); err == nil {
		t.Fatal("expected status error")
	}

	origPost := postFunc
	t.Cleanup(func() { postFunc = origPost })
	postFunc = func(string, string, io.Reader) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(errReader{})}, nil
	}
	if err := run([]string{"-url", "http://node", "-to", to, "-nonce", "0"}); err == nil || !strings.Contains(err.Error(), "body:") {
		t.Fatalf("err %v", err)
	}
}

func TestMainExitsOnError(t *testing.T) {
	origArgs := os.Args
	origExit := exitFunc
	t.Cleanup(func() {
		os.Args = origArgs
		exitFunc = origExit
	})
	os.Args = []string{"send"}
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

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read") }

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
