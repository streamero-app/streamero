package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSendFormatsPerKind(t *testing.T) {
	type got struct {
		path, ctype, auth, title string
		query                    string
		body                     []byte
	}
	reqs := make(chan got, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		reqs <- got{r.URL.Path, r.Header.Get("Content-Type"), r.Header.Get("Authorization"), r.Header.Get("Title"), r.URL.RawQuery, b}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	d := &Dispatcher{Server: func(context.Context) string { return "Kino" }}
	e := Event{Kind: ItemAdded, Title: "New in Movies", Message: "Dune (2021)", At: time.Unix(0, 0).UTC()}
	ctx := context.Background()

	if err := d.Send(ctx, Target{Kind: "discord", URL: srv.URL + "/hook"}, e); err != nil {
		t.Fatal(err)
	}
	r := <-reqs
	var discord struct {
		Username string
		Embeds   []struct{ Title, Description string }
	}
	_ = json.Unmarshal(r.body, &discord)
	if discord.Username != "Kino" || len(discord.Embeds) != 1 || discord.Embeds[0].Description != "Dune (2021)" {
		t.Fatalf("discord: %s", r.body)
	}

	if err := d.Send(ctx, Target{Kind: "ntfy", URL: srv.URL + "/streamero", Token: "tk"}, e); err != nil {
		t.Fatal(err)
	}
	r = <-reqs
	if r.path != "/streamero" || string(r.body) != "Dune (2021)" || r.title != "New in Movies" || r.auth != "Bearer tk" || !strings.HasPrefix(r.ctype, "text/plain") {
		t.Fatalf("ntfy: %+v", r)
	}

	if err := d.Send(ctx, Target{Kind: "gotify", URL: srv.URL + "/", Token: "app"}, e); err != nil {
		t.Fatal(err)
	}
	r = <-reqs
	if r.path != "/message" || r.query != "token=app" || !strings.Contains(string(r.body), `"priority":5`) {
		t.Fatalf("gotify: %+v %s", r, r.body)
	}

	if err := d.Send(ctx, Target{Kind: "generic", URL: srv.URL + "/g"}, e); err != nil {
		t.Fatal(err)
	}
	r = <-reqs
	var generic map[string]any
	_ = json.Unmarshal(r.body, &generic)
	if generic["event"] != ItemAdded || generic["server"] != "Kino" || generic["message"] != "Dune (2021)" {
		t.Fatalf("generic: %s", r.body)
	}
}

func TestSendReportsHTTPErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer srv.Close()
	err := (&Dispatcher{}).Send(context.Background(), Target{Kind: "generic", URL: srv.URL}, Event{Kind: Test})
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidate(t *testing.T) {
	for _, tc := range []struct {
		t  Target
		ok bool
	}{
		{Target{Kind: "discord", URL: "https://discord.com/api/webhooks/1/x"}, true},
		{Target{Kind: "ntfy", URL: "ftp://x"}, false},
		{Target{Kind: "slack", URL: "https://x"}, false},
		{Target{Kind: "gotify", URL: "https://push.example"}, false},
		{Target{Kind: "gotify", URL: "https://push.example", Token: "a"}, true},
		{Target{ID: uuid.New(), Kind: "gotify", URL: "https://push.example"}, true}, // keeps stored token
	} {
		if err := Validate(tc.t); (err == nil) != tc.ok {
			t.Errorf("%+v: err = %v", tc.t, err)
		}
	}
}
