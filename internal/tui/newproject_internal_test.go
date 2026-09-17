package tui

import "testing"

func TestIsCloneURL(t *testing.T) {
	for in, want := range map[string]bool{
		"https://github.com/owner/repo": true,
		"http://example.com/repo.git":   true,
		"ssh://git@host/owner/repo.git": true,
		"git@github.com:owner/repo.git": true,
		"~/dev/github/repo":             false,
		"/home/me/dev/repo":             false,
		"dev/repo":                      false,
	} {
		if got := isCloneURL(in); got != want {
			t.Errorf("isCloneURL(%q) = %v, want %v", in, got, want)
		}
	}
}

// ä and ö share their first byte in UTF-8, so a byte-wise prefix of the two
// ends inside a character.
func TestCommonPrefixKeepsWholeCharacters(t *testing.T) {
	if got := commonPrefix([]string{"/p/äa", "/p/öb"}); got != "/p/" {
		t.Errorf("commonPrefix = %q, want /p/", got)
	}
}

func TestSameRepoIgnoresTheGitSuffixAndATrailingSlash(t *testing.T) {
	want := sameRepo("https://github.com/o/widget")
	for _, u := range []string{"https://github.com/o/widget.git", "https://github.com/o/widget/"} {
		if got := sameRepo(u); got != want {
			t.Errorf("sameRepo(%q) = %q, want %q", u, got, want)
		}
	}
}

func TestRepoName(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/owner/repo":  "repo",
		"https://github.com/owner/repo/": "repo",
		"git@github.com:owner/repo.git":  "repo",
		"git@host:repo.git":              "repo",
	} {
		if got := repoName(in); got != want {
			t.Errorf("repoName(%q) = %q, want %q", in, got, want)
		}
	}
}
