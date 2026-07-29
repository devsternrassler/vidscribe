package netguard

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestValidatePublicURLRejectsPrivateTargets(t *testing.T) {
	for _, raw := range []string{
		"http://127.0.0.1/x", "http://[::1]/x", "http://169.254.169.254/latest",
		"http://localhost/x", "file:///etc/passwd", "https://user:pass@example.com/x",
	} {
		if err := ValidatePublicURL(context.Background(), raw); err == nil {
			t.Errorf("expected %s to be rejected", raw)
		}
	}
}

func TestPublicHTTPClientRejectsPrivateRedirect(t *testing.T) {
	client := PublicHTTPClient(time.Second)
	req := &http.Request{URL: &url.URL{Scheme: "http", Host: "127.0.0.1", Path: "/secret"}}
	if err := client.CheckRedirect(req, []*http.Request{{}}); err == nil {
		t.Fatal("expected private redirect to be rejected")
	}
}

func TestIsPublicIP(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "169.254.1.1", "::1", "fc00::1"} {
		if IsPublicIP(net.ParseIP(raw)) {
			t.Errorf("expected %s to be private", raw)
		}
	}
	for _, raw := range []string{"1.1.1.1", "8.8.8.8", "2606:4700:4700::1111"} {
		if !IsPublicIP(net.ParseIP(raw)) {
			t.Errorf("expected %s to be public", raw)
		}
	}
}
