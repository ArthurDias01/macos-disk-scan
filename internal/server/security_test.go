package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testGuard(t *testing.T) (*guard, *Token) {
	t.Helper()
	token, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	return newGuard(token, 7777), token
}

func mutatingRequest(origin, host, token string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/actions/trash", nil)
	r.Host = host
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if token != "" {
		r.Header.Set(tokenHeader, token)
	}
	return r
}

func TestAValidSameOriginRequestPasses(t *testing.T) {
	g, token := testGuard(t)

	for _, host := range []string{"localhost:7777", "127.0.0.1:7777"} {
		r := mutatingRequest("http://"+host, host, token.Value())
		if err := g.checkMutating(r); err != nil {
			t.Errorf("%s rejected: %v", host, err)
		}
	}
}

// Every web page you visit can post to localhost. These are the shapes that
// attempt would take.
func TestCrossOriginAndForgedRequestsAreRefused(t *testing.T) {
	g, token := testGuard(t)

	cases := []struct {
		name    string
		request *http.Request
	}{
		{"another origin", mutatingRequest("https://evil.example", "localhost:7777", token.Value())},
		{"no origin at all", mutatingRequest("", "localhost:7777", token.Value())},
		{"no token", mutatingRequest("http://localhost:7777", "localhost:7777", "")},
		{"wrong token", mutatingRequest("http://localhost:7777", "localhost:7777", "00")},
		// DNS rebinding: the attacker's name resolves to 127.0.0.1, so the
		// request really does arrive here — but Host still names their domain.
		{"rebound host", mutatingRequest("http://evil.example", "evil.example", token.Value())},
		// A right-looking origin on the wrong port is a different server.
		{"wrong port", mutatingRequest("http://localhost:1234", "localhost:7777", token.Value())},
	}

	for _, c := range cases {
		if err := g.checkMutating(c.request); err == nil {
			t.Errorf("%s was accepted", c.name)
		}
	}
}

func TestHostCheckAppliesToPlainGets(t *testing.T) {
	g, _ := testGuard(t)

	ok := httptest.NewRequest(http.MethodGet, "/", nil)
	ok.Host = "localhost:7777"
	if err := g.checkHost(ok); err != nil {
		t.Errorf("same-host GET rejected: %v", err)
	}

	rebound := httptest.NewRequest(http.MethodGet, "/", nil)
	rebound.Host = "evil.example"
	if err := g.checkHost(rebound); err == nil {
		t.Error("a rebound host reached the page")
	}
}

func TestTokensAreDistinctPerServer(t *testing.T) {
	first, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if first.Value() == second.Value() {
		t.Error("two servers minted the same token")
	}
	if first.Matches(second.Value()) {
		t.Error("tokens compare equal across servers")
	}
}

func TestTokenFileIsNotWorldReadable(t *testing.T) {
	token, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "state", "server-token")
	if err := token.WriteFile(path); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// It is the key to an endpoint that moves files to the Trash.
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 600", info.Mode().Perm())
	}
}

// ---------------------------------------------------------------------------
// Path allow-list
// ---------------------------------------------------------------------------

func TestPathsInsideARootAreAllowed(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "media", "clip.mov")
	if err := os.MkdirAll(filepath.Dir(nested), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nested, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	guard := NewPathGuard([]string{root})
	resolved, err := guard.Check(nested)
	if err != nil {
		t.Fatalf("rejected a path inside the root: %v", err)
	}
	if !strings.HasSuffix(resolved, "clip.mov") {
		t.Errorf("resolved = %s", resolved)
	}
}

func TestPathsOutsideEveryRootAreRefused(t *testing.T) {
	guard := NewPathGuard([]string{t.TempDir()})

	for _, path := range []string{"/etc/passwd", "/", "relative/path", "~/Documents"} {
		if _, err := guard.Check(path); err == nil {
			t.Errorf("%s was allowed", path)
		}
	}
}

// Trashing a root would take everything the report was built from with it.
func TestARootItselfIsRefused(t *testing.T) {
	root := t.TempDir()
	guard := NewPathGuard([]string{root})

	if _, err := guard.Check(root); err == nil {
		t.Error("the root itself was allowed")
	}
	// Including by a route that cleans back to it.
	if _, err := guard.Check(filepath.Join(root, "media", "..")); err == nil {
		t.Error("a path that resolves to the root was allowed")
	}
}

// A symlink inside a root pointing out of it is the obvious way to reach the
// rest of the filesystem through an endpoint that only checks prefixes.
func TestSymlinksCannotEscapeTheRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	target := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "shortcut")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if _, err := NewPathGuard([]string{root}).Check(link); err == nil {
		t.Error("a symlink out of the root was allowed")
	}
}

func TestTraversalIsRefused(t *testing.T) {
	root := t.TempDir()
	guard := NewPathGuard([]string{root})

	if _, err := guard.Check(filepath.Join(root, "..", "elsewhere")); err == nil {
		t.Error("a traversal out of the root was allowed")
	}
}

// A path that was deleted between the scan and the click still has to be
// judged, and judged the same way.
func TestAVanishedPathIsStillBoundedByTheRoots(t *testing.T) {
	root := t.TempDir()
	guard := NewPathGuard([]string{root})

	if _, err := guard.Check(filepath.Join(root, "gone.mov")); err != nil {
		t.Errorf("a missing path inside the root was rejected: %v", err)
	}
	if _, err := guard.Check("/nowhere/gone.mov"); err == nil {
		t.Error("a missing path outside the roots was allowed")
	}
}

func TestBatchesAreBoundedAndAllOrNothing(t *testing.T) {
	root := t.TempDir()
	guard := NewPathGuard([]string{root})

	if _, err := guard.CheckAll(nil); err == nil {
		t.Error("an empty batch was accepted")
	}

	huge := make([]string, MaxBatch+1)
	for i := range huge {
		huge[i] = filepath.Join(root, "f")
	}
	if _, err := guard.CheckAll(huge); err == nil {
		t.Error("an oversized batch was accepted")
	}

	// One bad path fails the whole batch: a caller that asked for something
	// disallowed gets nothing done, not some of it.
	mixed := []string{filepath.Join(root, "ok"), "/etc/passwd"}
	if _, err := guard.CheckAll(mixed); err == nil {
		t.Error("a batch containing a disallowed path was accepted")
	}
}
