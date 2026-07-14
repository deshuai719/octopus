package siteorigin

import "testing"

func TestNormalize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"lowercase and strip path", "HTTPS://Example.COM:443/login?a=1#x", "https://example.com"},
		{"keep non-default port", "http://Example.com:3000/a/", "http://example.com:3000"},
		{"remove default http port", "http://example.com:80", "http://example.com"},
		{"punycode", "https://例子.测试/path", "https://xn--fsqu00a.xn--0zwm56d"},
		{"trailing host dot", "https://Example.com./", "https://example.com"},
		{"ipv6", "https://[2001:4860:4860::8888]:8443/a", "https://[2001:4860:4860::8888]:8443"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Normalize(test.raw)
			if err != nil {
				t.Fatalf("Normalize() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("Normalize() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestNormalizeRejectsUnsafeURLs(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"file:///tmp/a", "https://user:pass@example.com", "https://", "https://example.com:70000"} {
		if _, err := Normalize(raw); err == nil {
			t.Fatalf("Normalize(%q) unexpectedly succeeded", raw)
		}
	}
}
