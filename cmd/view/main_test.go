package main

import (
	"errors"
	"net/http"
	"os"
	"testing"
)

func TestRunListens(t *testing.T) {
	orig := listenAndServe
	t.Cleanup(func() { listenAndServe = orig })
	var got string
	listenAndServe = func(addr string, h http.Handler) error {
		got = addr
		if h == nil {
			t.Fatal("nil handler")
		}
		return nil
	}
	if err := run([]string{"-listen", "127.0.0.1:9"}); err != nil {
		t.Fatal(err)
	}
	if got != "127.0.0.1:9" {
		t.Fatalf("addr %s", got)
	}
}

func TestRunListenError(t *testing.T) {
	orig := listenAndServe
	t.Cleanup(func() { listenAndServe = orig })
	listenAndServe = func(string, http.Handler) error { return errors.New("down") }
	if err := run(nil); err == nil || err.Error() != "listen: down" {
		t.Fatalf("err %v", err)
	}
}

func TestRunBadFlag(t *testing.T) {
	if err := run([]string{"-unknown"}); err == nil {
		t.Fatal("expected flag error")
	}
}

func TestMainExitsOnError(t *testing.T) {
	origArgs := os.Args
	origExit := exitFunc
	origServe := listenAndServe
	t.Cleanup(func() {
		os.Args = origArgs
		exitFunc = origExit
		listenAndServe = origServe
	})
	listenAndServe = func(string, http.Handler) error { return errors.New("down") }
	os.Args = []string{"view"}
	exitFunc = func(code int) {
		if code != 1 {
			t.Fatalf("code %d", code)
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
