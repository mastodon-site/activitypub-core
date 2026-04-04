package fetch

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"
)

// Regression: strict outbound dials must not connect after a second DNS lookup with a
// different answer than the validated set — here we only assert that resolving
// localhost to loopback is blocked after the single lookup used for the dial.
func TestStrictDial_rejectsLoopbackResolvedFromLocalhost(t *testing.T) {
	p := &Policy{}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err = p.dialContext(ctx, "tcp", net.JoinHostPort("localhost", port))
	if err == nil {
		t.Fatal("expected dial to fail: localhost resolves to loopback under strict policy")
	}
}

func TestLaxDial_allowsLocalhost(t *testing.T) {
	p := LaxPolicyForTests()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	c, err := p.dialContext(ctx, "tcp", net.JoinHostPort("localhost", port))
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
}
