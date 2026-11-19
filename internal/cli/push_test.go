package cli

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPushCookies(t *testing.T) {
	var gotBody, gotAuth, gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody, gotAuth, gotMethod, gotPath = string(b), r.Header.Get("Authorization"), r.Method, r.URL.Path
		if gotAuth != "Bearer s3cret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if err := PushCookies(context.Background(), nil, srv.URL+"/", "s3cret", "session_id=abc"); err != nil {
		t.Fatalf("push: %v", err)
	}
	if gotMethod != http.MethodPut || gotPath != "/patreon/token" || gotBody != "session_id=abc" {
		t.Errorf("request = %s %s %q", gotMethod, gotPath, gotBody)
	}

	err := PushCookies(context.Background(), nil, srv.URL, "wrong", "session_id=abc")
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("wrong secret: err = %v, want a 401 error", err)
	}

	for name, args := range map[string][3]string{
		"no scheme": {"example.com", "s", "h"},
		"no secret": {srv.URL, "", "h"},
		"no cookie": {srv.URL, "s", " "},
	} {
		if err := PushCookies(context.Background(), nil, args[0], args[1], args[2]); err == nil {
			t.Errorf("%s: err = nil, want an error", name)
		}
	}
}
