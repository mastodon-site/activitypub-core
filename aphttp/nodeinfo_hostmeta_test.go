package aphttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mastodon-site/activitypub-core/internal/config"
	"github.com/mastodon-site/activitypub-core/queue"
)

func TestContract_hostMeta_XML_lrddTemplate(t *testing.T) {
	cfg := &config.Config{
		PublicBaseURL:  "https://node.test",
		LocalUsernames: []string{"a"},
		LocalUsername:  "a",
	}
	h, err := New(cfg, Deps{Queue: queueNoop{}})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/.well-known/host-meta", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `application/jrd+json`) || !strings.Contains(body, `rel="lrdd"`) {
		t.Fatalf("body %q", body)
	}
	if !strings.Contains(body, `https://node.test/.well-known/webfinger?resource={uri}`) {
		t.Fatalf("template missing: %q", body)
	}
}

func TestContract_hostMeta_JSON(t *testing.T) {
	cfg := &config.Config{
		PublicBaseURL:  "https://node.test",
		LocalUsernames: []string{"a"},
		LocalUsername:  "a",
	}
	h, err := New(cfg, Deps{Queue: queueNoop{}})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/.well-known/host-meta", nil)
	req.Header.Set("Accept", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	var doc struct {
		Links []struct {
			Rel      string `json:"rel"`
			Template string `json:"template"`
		} `json:"links"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Links) != 1 || doc.Links[0].Rel != "lrdd" {
		t.Fatalf("links %+v", doc.Links)
	}
	want := "https://node.test/.well-known/webfinger?resource={uri}"
	if doc.Links[0].Template != want {
		t.Fatalf("template %q", doc.Links[0].Template)
	}
}

func TestContract_nodeInfoDiscovery_and20(t *testing.T) {
	cfg := &config.Config{
		PublicBaseURL:       "https://node.test",
		LocalUsernames:      []string{"a"},
		LocalUsername:       "a",
		InstanceName:        "Node Test",
		InstanceDescription: "Hello",
		SoftwareVersion:     "1.2.3",
		OpenRegistrations:   true,
	}
	h, err := New(cfg, Deps{Queue: queueNoop{}})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/nodeinfo", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("discovery status %d", rr.Code)
	}
	var disc struct {
		Links []struct {
			Rel  string `json:"rel"`
			Href string `json:"href"`
		} `json:"links"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &disc); err != nil {
		t.Fatal(err)
	}
	if len(disc.Links) != 1 || disc.Links[0].Href != "https://node.test/nodeinfo/2.0" {
		t.Fatalf("discovery %+v", disc)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/nodeinfo/2.0", nil)
	rr2 := httptest.NewRecorder()
	mux.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("nodeinfo 20 status %d", rr2.Code)
	}
	var ni map[string]json.RawMessage
	if err := json.Unmarshal(rr2.Body.Bytes(), &ni); err != nil {
		t.Fatal(err)
	}
	if string(ni["version"]) != `"2.0"` {
		t.Fatalf("version %s", ni["version"])
	}
	var sw map[string]string
	if err := json.Unmarshal(ni["software"], &sw); err != nil {
		t.Fatal(err)
	}
	if sw["name"] != "activitypub-core" || sw["version"] != "1.2.3" {
		t.Fatalf("software %+v", sw)
	}
	var meta map[string]string
	if err := json.Unmarshal(ni["metadata"], &meta); err != nil {
		t.Fatal(err)
	}
	if meta["nodeName"] != "Node Test" || meta["nodeDescription"] != "Hello" {
		t.Fatalf("metadata %+v", meta)
	}
}

type queueNoop struct{}

func (queueNoop) Enqueue(ctx context.Context, job queue.Job) error         { return nil }
func (queueNoop) Dequeue(ctx context.Context) (*queue.Lease, error)        { return nil, nil }
func (queueNoop) Ack(ctx context.Context, id64 int64) error                { return nil }
func (queueNoop) Nack(ctx context.Context, id64 int64, requeue bool) error { return nil }
