package pipeline

import "testing"

func TestIsHTTPErrors(t *testing.T) {
	tests := []struct {
		msg     string
		is403   bool
		is429   bool
		isSecre bool
	}{
		{"ERROR: unable to download webpage: HTTP Error 403: Forbidden", true, false, false},
		{"ERROR: HTTP Error 429: Too Many Requests", false, true, false},
		{"403 Forbidden (access denied)", true, false, false},
		{"429 Too Many Requests", false, true, false},
		// Linux keyring
		{"secretstorage module not found", false, false, true},
		{"Failed to unlock keyring", false, false, true},
		{"No module named 'secretstorage'", false, false, true},
		// macOS keyring
		{"Keychain access denied", false, false, true},
		{"OSStatus error -25300", false, false, true},
		{"cannot be found in the keychain", false, false, true},
		// Windows keyring
		{"CryptUnprotectData failed", false, false, true},
		{"No module named 'win32crypt'", false, false, true},
		// unrelated
		{"network timeout", false, false, false},
		{"", false, false, false},
	}

	for _, tt := range tests {
		if got := isHTTP403(tt.msg); got != tt.is403 {
			t.Errorf("isHTTP403(%q) = %v, want %v", tt.msg, got, tt.is403)
		}
		if got := isHTTP429(tt.msg); got != tt.is429 {
			t.Errorf("isHTTP429(%q) = %v, want %v", tt.msg, got, tt.is429)
		}
		if got := isKeyringError(tt.msg); got != tt.isSecre {
			t.Errorf("isKeyringError(%q) = %v, want %v", tt.msg, got, tt.isSecre)
		}
	}
}

func TestFirstLine(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"line1\nline2\nline3", "line1"},
		{"single line", "single line"},
		{"  spaces  ", "spaces"},
		{"", ""},
		{"\n\n", ""},
	}

	for _, tt := range tests {
		got := firstLine(tt.input)
		if got != tt.want {
			t.Errorf("firstLine(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// hasArg reports whether flag appears in args.
func hasArg(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func TestBuildBaseArgs(t *testing.T) {
	t.Run("no auth, explicit empty js-runtime", func(t *testing.T) {
		// Pass a non-existent runtime so auto-detection is suppressed.
		args := buildBaseArgs(&Config{JSRuntime: "node:/nonexistent/node"})
		// Only js-runtime flags expected — no cookie flags.
		if hasArg(args, "--cookies-from-browser") || hasArg(args, "--cookies") {
			t.Errorf("unexpected cookie args: %v", args)
		}
	})

	t.Run("browser cookies", func(t *testing.T) {
		args := buildBaseArgs(&Config{CookiesBrowser: "chrome", JSRuntime: "node:/nonexistent/node"})
		if !hasArg(args, "--cookies-from-browser") {
			t.Errorf("expected --cookies-from-browser in %v", args)
		}
		if hasArg(args, "--cookies") {
			t.Errorf("unexpected --cookies in %v", args)
		}
	})

	t.Run("cookie file", func(t *testing.T) {
		args := buildBaseArgs(&Config{CookiesFile: "/tmp/cookies.txt", JSRuntime: "node:/nonexistent/node"})
		if !hasArg(args, "--cookies") {
			t.Errorf("expected --cookies in %v", args)
		}
	})

	t.Run("browser takes precedence over file", func(t *testing.T) {
		args := buildBaseArgs(&Config{CookiesBrowser: "firefox", CookiesFile: "/tmp/c.txt", JSRuntime: "node:/nonexistent/node"})
		if args[0] != "--cookies-from-browser" {
			t.Errorf("expected browser arg first, got %v", args)
		}
	})

	t.Run("explicit js-runtime sets --js-runtimes and --remote-components", func(t *testing.T) {
		args := buildBaseArgs(&Config{JSRuntime: "deno:/usr/bin/deno"})
		if !hasArg(args, "--js-runtimes") {
			t.Errorf("expected --js-runtimes in %v", args)
		}
		if !hasArg(args, "--remote-components") {
			t.Errorf("expected --remote-components in %v", args)
		}
	})

	t.Run("auto-detected node sets --js-runtimes", func(t *testing.T) {
		// Auto-detection runs when JSRuntime is empty; result depends on environment.
		// We only assert that if --js-runtimes is present, --remote-components follows.
		args := buildBaseArgs(&Config{})
		if hasArg(args, "--js-runtimes") && !hasArg(args, "--remote-components") {
			t.Errorf("--js-runtimes without --remote-components in %v", args)
		}
	})
}

func TestRemoveBrowserCookies(t *testing.T) {
	input := []string{"--no-playlist", "--cookies-from-browser", "chrome", "--extract-audio"}
	got := removeBrowserCookies(input)
	want := []string{"--no-playlist", "--extract-audio"}

	if len(got) != len(want) {
		t.Fatalf("removeBrowserCookies = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("removeBrowserCookies[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
