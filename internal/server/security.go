package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// The guards in this file exist because of one fact: a server bound to
// localhost is reachable from every web page you visit. It is not on a network
// anyone else can route to, but the browser you are reading this in can post to
// it, and so can any tab you have open. Since this server moves files to the
// Trash, "only localhost" is not a security boundary on its own.
//
// Four checks stack up, and a request has to pass all of them:
//
//  1. A token in a custom header. Custom headers force a CORS preflight, and the
//     preflight is refused, so a cross-origin page cannot send one at all.
//  2. An Origin allow-list, because a same-origin page is the only legitimate
//     caller.
//  3. A Host check, because DNS rebinding points an attacker's domain at
//     127.0.0.1 and would otherwise satisfy an Origin check.
//  4. A path allow-list, so even a valid caller cannot reach outside the roots
//     that were configured for scanning.

// tokenHeader is deliberately not a CORS-safelisted header. Sending it
// cross-origin requires a successful preflight, which this server never grants.
const tokenHeader = "X-Disk-Report-Token"

// Token authenticates the page against the server that served it.
type Token struct {
	value string
}

// NewToken mints a fresh token. One per server start: a token that outlives the
// process is a token that can leak from a stale file.
func NewToken() (*Token, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	return &Token{value: hex.EncodeToString(raw)}, nil
}

// Value is the token as sent to the page.
func (t *Token) Value() string { return t.value }

// Matches compares in constant time.
func (t *Token) Matches(candidate string) bool {
	return subtle.ConstantTimeCompare([]byte(t.value), []byte(candidate)) == 1
}

// WriteFile stores the token for tooling that needs it (curl, tests). Mode 0600:
// on a shared machine this is the key to the trash endpoint.
func (t *Token) WriteFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(t.value+"\n"), 0o600)
}

// guard rejects anything that is not the page this server served.
type guard struct {
	token   *Token
	origins map[string]bool
	hosts   map[string]bool
}

func newGuard(token *Token, port int) *guard {
	origins := map[string]bool{}
	hosts := map[string]bool{}
	for _, host := range []string{"localhost", "127.0.0.1"} {
		authority := fmt.Sprintf("%s:%d", host, port)
		origins["http://"+authority] = true
		hosts[authority] = true
	}
	return &guard{token: token, origins: origins, hosts: hosts}
}

// checkHost runs on every request, safe or not. A request whose Host header
// names something other than localhost reached us through a name that resolves
// here — which is what DNS rebinding looks like.
func (g *guard) checkHost(r *http.Request) error {
	if !g.hosts[r.Host] {
		return fmt.Errorf("unexpected Host %q", r.Host)
	}
	return nil
}

// checkMutating runs on everything that changes state.
func (g *guard) checkMutating(r *http.Request) error {
	if err := g.checkHost(r); err != nil {
		return err
	}

	// An absent Origin is not treated as trustworthy. Browsers send it on every
	// cross-origin request and on same-origin POSTs; a caller that omits it is
	// either not a browser or is trying not to be identified.
	origin := r.Header.Get("Origin")
	if origin == "" {
		return fmt.Errorf("missing Origin")
	}
	if !g.origins[origin] {
		return fmt.Errorf("origin %q is not allowed", origin)
	}

	if !g.token.Matches(r.Header.Get(tokenHeader)) {
		return fmt.Errorf("bad or missing %s", tokenHeader)
	}
	return nil
}

// PathGuard decides which paths the action endpoints may touch.
type PathGuard struct {
	roots []string
}

// NewPathGuard builds the allow-list from the configured scan roots. Anything
// the scanner was never asked to look at is not something the app may move.
func NewPathGuard(roots []string) *PathGuard {
	resolved := make([]string, 0, len(roots))
	for _, root := range roots {
		clean := filepath.Clean(root)
		// Resolve the root too: on macOS /tmp is a symlink to /private/tmp, and
		// comparing a resolved path against an unresolved root never matches.
		if real, err := filepath.EvalSymlinks(clean); err == nil {
			clean = real
		}
		resolved = append(resolved, clean)
	}
	return &PathGuard{roots: resolved}
}

// MaxBatch caps a single request. A bug in the page should not be able to ask
// for the whole disk in one call.
const MaxBatch = 500

// Check reports whether a path may be acted on, returning the resolved path.
//
// Symlinks are resolved before the prefix test, so a link inside a root that
// points outside it cannot be used to reach the rest of the filesystem.
func (p *PathGuard) Check(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("not an absolute path: %s", path)
	}

	resolved := resolvePath(filepath.Clean(path))

	for _, root := range p.roots {
		if resolved == root {
			return "", fmt.Errorf("refusing to act on a scan root: %s", path)
		}
		if strings.HasPrefix(resolved, root+string(filepath.Separator)) {
			return resolved, nil
		}
	}

	return "", fmt.Errorf("outside every configured root: %s", path)
}

// resolvePath resolves symlinks as far as the filesystem allows.
//
// EvalSymlinks fails outright on a path that does not exist, and "does not
// exist" is the normal case here: the file was in the last scan and has since
// been deleted. Falling back to the lexical path is not good enough, because on
// macOS the roots resolve through /private and the two forms would never share
// a prefix — a vanished file would look like it was outside every root.
//
// So the deepest existing ancestor is resolved and the rest re-appended. The
// directory chain is still followed, which is what the symlink check needs.
func resolvePath(path string) string {
	remainder := ""
	current := path

	for {
		if real, err := filepath.EvalSymlinks(current); err == nil {
			return filepath.Join(real, remainder)
		}

		parent := filepath.Dir(current)
		if parent == current {
			return path
		}
		remainder = filepath.Join(filepath.Base(current), remainder)
		current = parent
	}
}

// CheckAll validates a batch, failing as a whole rather than part way through.
// A caller that asked for something disallowed gets nothing done, not some of it.
func (p *PathGuard) CheckAll(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("no paths given")
	}
	if len(paths) > MaxBatch {
		return nil, fmt.Errorf("%d paths exceeds the %d limit", len(paths), MaxBatch)
	}

	resolved := make([]string, 0, len(paths))
	for _, path := range paths {
		checked, err := p.Check(path)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, checked)
	}
	return resolved, nil
}
