package client

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHistory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/history" || r.URL.Query().Get("since") != "4" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"source":"pwplay@h/1","lastSeq":5,"records":[{"seq":5,"url":"http://fh/abc","listened":12.5,"duration":200}]}`))
	}))
	defer srv.Close()
	h, err := New(srv.URL).History(4)
	if err != nil {
		t.Fatal(err)
	}
	if h.Source != "pwplay@h/1" || h.LastSeq != 5 || len(h.Records) != 1 || h.Records[0].Listened != 12.5 {
		t.Fatalf("%+v", h)
	}
	if _, err := New(srv.URL).History(0); !errors.Is(err, ErrNoHistory) {
		t.Fatalf("404 = %v, want ErrNoHistory", err)
	}
}
