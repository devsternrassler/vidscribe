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
		{"secretstorage module not found", false, false, true},
		{"Failed to unlock keyring", false, false, true},
		{"No module named 'secretstorage'", false, false, true},
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
		if got := isSecretstorageError(tt.msg); got != tt.isSecre {
			t.Errorf("isSecretstorageError(%q) = %v, want %v", tt.msg, got, tt.isSecre)
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

func TestBuildBaseArgs(t *testing.T) {
	t.Run("no auth", func(t *testing.T) {
		args := buildBaseArgs(&Config{})
		if len(args) != 0 {
			t.Errorf("expected empty args, got %v", args)
		}
	})

	t.Run("browser cookies", func(t *testing.T) {
		args := buildBaseArgs(&Config{CookiesBrowser: "chrome"})
		if len(args) != 2 || args[0] != "--cookies-from-browser" || args[1] != "chrome" {
			t.Errorf("unexpected args: %v", args)
		}
	})

	t.Run("cookie file", func(t *testing.T) {
		args := buildBaseArgs(&Config{CookiesFile: "/tmp/cookies.txt"})
		if len(args) != 2 || args[0] != "--cookies" || args[1] != "/tmp/cookies.txt" {
			t.Errorf("unexpected args: %v", args)
		}
	})

	t.Run("browser takes precedence over file", func(t *testing.T) {
		args := buildBaseArgs(&Config{CookiesBrowser: "firefox", CookiesFile: "/tmp/c.txt"})
		if args[0] != "--cookies-from-browser" {
			t.Errorf("expected browser arg first, got %v", args)
		}
	})

	t.Run("js-runtime", func(t *testing.T) {
		args := buildBaseArgs(&Config{JSRuntime: "deno"})
		found := false
		for i, a := range args {
			if a == "--extractor-args" && i+1 < len(args) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected --extractor-args in %v", args)
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
