package ssrfguard

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestIsAllowedAddr(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "127.0.0.2", "0.0.0.0", "::1", "::",
		"10.0.0.1", "10.255.255.255", "172.16.0.1", "172.31.255.255",
		"192.168.0.1", "192.168.255.255",
		"169.254.169.254", // AWS EC2 instance metadata
		"169.254.1.1",
		"100.64.0.1", "100.127.255.255", // CGNAT — hostile
		"fe80::1", "fc00::1", "fd12:3456::1",
		"::ffff:127.0.0.1", "::ffff:10.0.0.1",
	}
	for _, s := range blocked {
		if IsAllowedAddr(net.ParseIP(s)) {
			t.Errorf("IsAllowedAddr(%q) = true, want false (blocked)", s)
		}
	}
	allowed := []string{
		"8.8.8.8", "1.1.1.1", "93.184.216.34",
		"2606:4700::6810:3c04", "104.21.60.4", "142.250.72.14",
	}
	for _, s := range allowed {
		if !IsAllowedAddr(net.ParseIP(s)) {
			t.Errorf("IsAllowedAddr(%q) = false, want true (public)", s)
		}
	}
}

func TestValidateURLRejectsBadURLs(t *testing.T) {
	for _, u := range []string{"not-a-url", "ftp://example.com/x", "", "http://"} {
		if _, err := ValidateURL(context.Background(), u); err == nil {
			t.Errorf("ValidateURL(%q): expected error", u)
		}
	}
}

type staticResolver struct{ ips []net.IP }

func (r *staticResolver) LookupIPAddr(_ context.Context, _ string) ([]net.IPAddr, error) {
	out := make([]net.IPAddr, 0, len(r.ips))
	for _, ip := range r.ips {
		out = append(out, net.IPAddr{IP: ip})
	}
	return out, nil
}

func TestPinRejectsHostileIPs(t *testing.T) {
	cases := []struct {
		name string
		ip   string
	}{
		{"metadata", "169.254.169.254"},
		{"loopback", "127.0.0.1"},
		{"private", "10.0.0.5"},
		{"linklocal", "169.254.1.1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := &staticResolver{ips: []net.IP{net.ParseIP(c.ip)}}
			if pin, err := Pin(res, "attacker.example.com"); err == nil {
				t.Fatalf("Pin should reject %s, got pin %v", c.ip, pin)
			}
		})
	}
}

// TestPinAllowsPublic verifies a public host is permitted and the returned
// dial actually connects to the pinned (public) IP.
func TestPinAllowsPublicAndConnects(t *testing.T) {
	res := &staticResolver{ips: []net.IP{net.ParseIP("93.184.216.34")}}
	pin, err := Pin(res, "example.com")
	if err != nil {
		t.Fatalf("Pin should allow public IP: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := pin.DialContext(ctx, "tcp", "example.com:80")
	if err != nil {
		t.Skipf("network dial to public IP unavailable in test env: %v", err)
	}
	conn.Close()
	if got := pin.IPs[0].String(); got != "93.184.216.34" {
		t.Errorf("pinned IP = %s, want 93.184.216.34", got)
	}
}
