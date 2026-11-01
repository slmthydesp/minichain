package view

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func hostOf(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u.Host
}

func TestBadNode(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("upstream %s", r.URL.RequestURI())
	}))
	t.Cleanup(upstream.Close)
	h := Handler()
	bad := []string{"", "127.0.0.1", "http://127.0.0.1:8545/status", "127.0.0.1:8545/status", "127.0.0.1:0"}
	for _, node := range bad {
		for _, path := range []string{"/api/status", "/api/blocks", "/api/account"} {
			q := url.Values{}
			q.Set("node", node)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path+"?"+q.Encode(), nil))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("%s %q: status %d", path, node, rec.Code)
			}
			if strings.TrimSpace(rec.Body.String()) != "bad node" {
				t.Fatalf("%s %q: body %q", path, node, rec.Body.String())
			}
		}
	}
}

func TestBadAfter(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("upstream %s", r.URL.RequestURI())
	}))
	t.Cleanup(upstream.Close)
	q := url.Values{}
	q.Set("node", hostOf(t, upstream.URL))
	q.Set("after", "12abc")
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/blocks?"+q.Encode(), nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
	if strings.TrimSpace(rec.Body.String()) != "bad after" {
		t.Fatalf("body %q", rec.Body.String())
	}
}

func TestNodeBeforeAfter(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("upstream %s", r.URL.RequestURI())
	}))
	t.Cleanup(upstream.Close)
	q := url.Values{}
	q.Set("node", "")
	q.Set("after", "12abc")
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/blocks?"+q.Encode(), nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
	if strings.TrimSpace(rec.Body.String()) != "bad node" {
		t.Fatalf("body %q", rec.Body.String())
	}
}

func TestStatusPassthrough(t *testing.T) {
	const body = `{"height":3,"stateRoot":"0xabc","head":"0xdef"}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status" {
			t.Errorf("path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(upstream.Close)
	q := url.Values{}
	q.Set("node", hostOf(t, upstream.URL))
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/status?"+q.Encode(), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("type %q", rec.Header().Get("Content-Type"))
	}
	if rec.Body.String() != body {
		t.Fatalf("body %q", rec.Body.String())
	}
}

func TestNodeURLForwardsToHost(t *testing.T) {
	var got string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RequestURI()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(upstream.Close)
	q := url.Values{}
	q.Set("node", "http://"+hostOf(t, upstream.URL)+"/")
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/status?"+q.Encode(), nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d", rec.Code)
	}
	if got != "/status" {
		t.Fatalf("uri %s", got)
	}
}

func TestBlocksQuery(t *testing.T) {
	cases := []struct {
		after   string
		present bool
		wantURI string
	}{
		{present: false, wantURI: "/blocks"},
		{present: true, after: "76", wantURI: "/blocks?after=76"},
		{present: true, after: "076", wantURI: "/blocks?after=76"},
		{present: true, after: "0", wantURI: "/blocks?after=0"},
	}
	for _, tc := range cases {
		var got string
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = r.URL.RequestURI()
			w.WriteHeader(http.StatusNoContent)
		}))
		t.Cleanup(upstream.Close)
		q := url.Values{}
		q.Set("node", hostOf(t, upstream.URL))
		if tc.present {
			q.Set("after", tc.after)
		}
		rec := httptest.NewRecorder()
		Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/blocks?"+q.Encode(), nil))
		if got != tc.wantURI {
			t.Fatalf("after %q: uri %s", tc.after, got)
		}
		if rec.Code != http.StatusNoContent {
			t.Fatalf("after %q: status %d", tc.after, rec.Code)
		}
	}
}

func TestUnreachable(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	node := hostOf(t, upstream.URL)
	upstream.Close()
	q := url.Values{}
	q.Set("node", node)
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/status?"+q.Encode(), nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status %d", rec.Code)
	}
	if strings.TrimSpace(rec.Body.String()) != "unreachable" {
		t.Fatalf("body %q", rec.Body.String())
	}
}

func TestRedirectNotFollowed(t *testing.T) {
	var hits int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path != "/status" {
			t.Errorf("path %s", r.URL.Path)
		}
		w.Header().Set("Location", "/gone")
		w.WriteHeader(http.StatusFound)
		_, _ = io.WriteString(w, "stay")
	}))
	t.Cleanup(upstream.Close)
	q := url.Values{}
	q.Set("node", hostOf(t, upstream.URL))
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/status?"+q.Encode(), nil))
	if hits != 1 {
		t.Fatalf("hits %d", hits)
	}
	if rec.Code != http.StatusFound {
		t.Fatalf("status %d", rec.Code)
	}
	if rec.Body.String() != "stay" {
		t.Fatalf("body %q", rec.Body.String())
	}
}

func TestAccountQuery(t *testing.T) {
	const addr = "0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266"
	var got string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"address":"`+addr+`","balance":1000000000000000000,"nonce":0}`)
	}))
	t.Cleanup(upstream.Close)
	q := url.Values{}
	q.Set("node", hostOf(t, upstream.URL))
	q.Set("address", "0xF39FD6E51AAD88F6F4CE6AB8827279CFFFB92266")
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/account?"+q.Encode(), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if got != "/account?address="+addr {
		t.Fatalf("uri %s", got)
	}
	if rec.Body.String() != `{"address":"`+addr+`","balance":1000000000000000000,"nonce":0}` {
		t.Fatalf("body %q", rec.Body.String())
	}
}

func TestBadAddress(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("upstream %s", r.URL.RequestURI())
	}))
	t.Cleanup(upstream.Close)
	q := url.Values{}
	q.Set("node", hostOf(t, upstream.URL))
	q.Set("address", "zz")
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/account?"+q.Encode(), nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
	if strings.TrimSpace(rec.Body.String()) != "bad address" {
		t.Fatalf("body %q", rec.Body.String())
	}
}

func TestNodeBeforeAddress(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("upstream %s", r.URL.RequestURI())
	}))
	t.Cleanup(upstream.Close)
	q := url.Values{}
	q.Set("node", "")
	q.Set("address", "zz")
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/account?"+q.Encode(), nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
	if strings.TrimSpace(rec.Body.String()) != "bad node" {
		t.Fatalf("body %q", rec.Body.String())
	}
}

func TestNotFound(t *testing.T) {
	h := Handler()
	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodGet, "/missing"},
		{http.MethodPost, "/api/status"},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s: status %d", tc.method, tc.path, rec.Code)
		}
	}
}
