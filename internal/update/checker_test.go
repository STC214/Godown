package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsNewer(t *testing.T) {
	tests := []struct {
		current, candidate string
		want               bool
	}{
		{"1.2.3", "v1.2.4", true},
		{"1.2.3", "1.3.0", true},
		{"1.2.3", "2.0.0", true},
		{"1.2.3", "1.2.3", false},
		{"1.2.3", "1.2.2", false},
		{"0.0.0-dev", "v0.1.0", true},
	}
	for _, test := range tests {
		got, err := IsNewer(test.current, test.candidate)
		if err != nil || got != test.want {
			t.Errorf("IsNewer(%q, %q) = %v, %v; want %v", test.current, test.candidate, got, err, test.want)
		}
	}
}

func TestCheckerLatestRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/releases/latest" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v1.4.0","name":"Release 1.4","html_url":"https://example.test/release","body":"notes"}`))
	}))
	defer server.Close()
	result, err := (Checker{CurrentVersion: "1.3.2", Owner: "owner", Repository: "repo", APIBaseURL: server.URL}).Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Newer || result.Version != "1.4.0" || result.URL != "https://example.test/release" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestCheckerRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(make([]byte, maxResponseBytes+1))
	}))
	defer server.Close()
	_, err := (Checker{CurrentVersion: "1.0.0", Owner: "owner", Repository: "repo", APIBaseURL: server.URL}).Check(context.Background())
	if err == nil {
		t.Fatal("expected oversized response error")
	}
}
