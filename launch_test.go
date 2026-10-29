package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExistingStudio(t *testing.T) {
	s := &studio{token: "test", params: defaults()}
	server := httptest.NewServer(s)
	defer server.Close()
	s.host = strings.TrimPrefix(server.URL, "http://")
	if !existingStudio(s.host) {
		t.Fatal("running studio was not detected")
	}
	unrelated := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("different app")) }))
	defer unrelated.Close()
	if existingStudio(strings.TrimPrefix(unrelated.URL, "http://")) {
		t.Fatal("unrelated app treated as studio")
	}
}
