package view

import (
	"strings"
	"testing"
)

func TestCanonicalNode(t *testing.T) {
	good := []struct{ in, want string }{
		{"127.0.0.1:8545", "127.0.0.1:8545"},
		{"  127.0.0.1:8545  ", "127.0.0.1:8545"},
		{"localhost:8545", "localhost:8545"},
		{"[::1]:8545", "[::1]:8545"},
		{"127.0.0.1:08545", "127.0.0.1:8545"},
		{"[fe80::1]:08545", "[fe80::1]:8545"},
		{"http://127.0.0.1:8546", "127.0.0.1:8546"},
		{"  HTTP://127.0.0.1:8546/  ", "127.0.0.1:8546"},
		{"https://[::1]:8545", "[::1]:8545"},
	}
	for _, tc := range good {
		got, err := CanonicalNode(tc.in)
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("%q: got %q want %q", tc.in, got, tc.want)
		}
	}
	bad := []string{
		"",
		"127.0.0.1",
		"http://127.0.0.1:8545/status",
		"https://127.0.0.1:8545?x=1",
		"ftp://127.0.0.1:8545",
		"127.0.0.1:8545/status",
		"127.0.0.1:0",
		"user@host:80",
		"127.0.0.1?:8545",
		"127.0.0.1#:8545",
		"127.0.0.1%:8545",
	}
	for _, in := range bad {
		_, err := CanonicalNode(in)
		if err == nil || err.Error() != "bad node" {
			t.Fatalf("%q: got %v", in, err)
		}
	}
}

func TestCanonicalAddress(t *testing.T) {
	good := []struct{ in, want string }{
		{"0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266", "0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266"},
		{"  0xF39FD6E51AAD88F6F4CE6AB8827279CFFFB92266  ", "0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266"},
		{"f39fd6e51aad88f6f4ce6ab8827279cfffb92266", "0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266"},
	}
	for _, tc := range good {
		got, err := CanonicalAddress(tc.in)
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("%q: got %q want %q", tc.in, got, tc.want)
		}
	}
	bad := []string{"", "0x", "0x1234", "zz", "0x" + strings.Repeat("g", 40), "0x" + strings.Repeat("ab", 21)}
	for _, in := range bad {
		_, err := CanonicalAddress(in)
		if err == nil || err.Error() != "bad address" {
			t.Fatalf("%q: got %v", in, err)
		}
	}
}
