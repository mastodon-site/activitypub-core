package fetch

import (
	"context"
	"testing"
)

func TestPolicy_rejectsLoopbackHTTPS(t *testing.T) {
	p := &Policy{}
	ctx := context.Background()
	if err := p.CheckURL(ctx, "https://127.0.0.1/users/x"); err == nil {
		t.Fatal("expected error for loopback")
	}
}

func TestPolicy_rejectsHTTPByDefault(t *testing.T) {
	p := &Policy{}
	ctx := context.Background()
	if err := p.CheckURL(ctx, "http://192.0.2.1/inbox"); err == nil {
		t.Fatal("expected error for http when disallowed")
	}
}

func TestPolicy_rejectsRFC1918(t *testing.T) {
	p := &Policy{}
	ctx := context.Background()
	for _, u := range []string{
		"https://10.0.0.1/inbox",
		"https://172.16.0.1/inbox",
		"https://192.168.1.1/inbox",
	} {
		if err := p.CheckURL(ctx, u); err == nil {
			t.Fatalf("expected error for %s", u)
		}
	}
}

func TestPolicy_rejectsLinkLocalMetadataIPv4(t *testing.T) {
	p := &Policy{}
	ctx := context.Background()
	if err := p.CheckURL(ctx, "https://169.254.169.254/latest/meta-data"); err == nil {
		t.Fatal("expected error for link-local / metadata-style address")
	}
}

func TestPolicy_laxAllowsPrivateAndHTTP(t *testing.T) {
	p := LaxPolicyForTests()
	ctx := context.Background()
	if err := p.CheckURL(ctx, "http://127.0.0.1/inbox"); err != nil {
		t.Fatalf("lax should allow http+loopback for tests: %v", err)
	}
}
