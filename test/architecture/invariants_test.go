package architecture

// The claims about this module's shape, rather than its behaviour.

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Three packages, three questions, and no edges between them.
//
// observe delivers events and owns no question about them; trace and metrics
// each own one and neither needs the other. A program wanting both composes
// them with observe.Fanout, which is what keeps this a set of parts rather
// than a framework: an edge here would mean a caller who wanted a span tree
// had also acquired an aggregate.
var mayImport = map[string][]string{
	"observe": {},
	"trace":   {},
	"metrics": {},
}

func TestPackagesDependOnNothingHereButThemselves(t *testing.T) {
	for pkg, allowed := range mayImport {
		permitted := map[string]bool{}
		for _, held := range allowed {
			permitted[held] = true
		}
		for _, source := range sourcesIn(t, pkg) {
			for _, line := range strings.Split(readSource(t, source), "\n") {
				held, found := ownImport(line)
				if !found || held == pkg || permitted[held] {
					continue
				}
				t.Errorf("%s imports %s, which it may not", display(t, source), held)
			}
		}
	}
}

// ownImport is an import of this module, as a package path within it. Only an
// import line counts: a doc comment naming a sibling package is prose.
func ownImport(line string) (string, bool) {
	if !imported(line) {
		return "", false
	}
	const prefix = `"github.com/mbauer83/effect-golang-observe/`
	at := strings.Index(line, prefix)
	if at < 0 {
		return "", false
	}
	rest := line[at+len(prefix):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return "", false
	}
	return rest[:end], true
}

// imported says the line is an import and not prose that happens to name a
// package. An import line is a quoted path and nothing else, optionally behind
// an alias or the import keyword.
func imported(line string) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.HasSuffix(trimmed, `"`) {
		return false
	}
	trimmed = strings.TrimPrefix(trimmed, "import ")
	if strings.HasPrefix(trimmed, `"`) {
		return true
	}
	alias, rest, split := strings.Cut(trimmed, " ")
	return split && strings.HasPrefix(rest, `"`) && !strings.Contains(alias, `"`)
}

// TestNothingHereCarriesAThirdPartyDependency is the claim that makes this
// module free to install.
//
// Telemetry is where dependencies get in: an exporter brings a client, a client
// brings a protobuf runtime, and a program that only wanted to see its own
// spans has acquired all of it. Everything here reads the runtime's own events
// and the standard library, so installing it costs a program nothing it did
// not already have -- and an exporter for somebody's ecosystem is a module of
// its own, which is what the runtime's own reference says it should be.
func TestNothingHereCarriesAThirdPartyDependency(t *testing.T) {
	walk := func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		relative := display(t, path)
		for _, line := range strings.Split(readSource(t, path), "\n") {
			if third, found := thirdPartyImport(line); found {
				t.Errorf("%s imports %q, and this module carries no dependencies",
					relative, third)
			}
		}
		return nil
	}
	if err := filepath.WalkDir(moduleRoot(t), walk); err != nil {
		t.Fatal(err)
	}
}

// thirdPartyImport reports an import that is neither the standard library nor
// this project. A standard-library path has no dot before its first slash.
func thirdPartyImport(line string) (string, bool) {
	if !imported(line) {
		return "", false
	}
	trimmed := strings.TrimSpace(line)
	start := strings.Index(trimmed, `"`)
	path := strings.Trim(trimmed[start:], `"`)
	host, _, hasSlash := strings.Cut(path, "/")
	if !hasSlash || !strings.Contains(host, ".") {
		return "", false
	}
	if strings.HasPrefix(path, "github.com/mbauer83/") {
		return "", false
	}
	return path, true
}

// The module root is for project metadata and documentation. Every package is
// a directory, for the reason the runtime's is: a root full of source files is
// not a layout.
func TestModuleRootHoldsNoSource(t *testing.T) {
	entries, err := os.ReadDir(moduleRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
			t.Errorf("%s is in the module root", entry.Name())
		}
	}
}

// A file that grows past these has stopped being about one thing. The soft
// limit is a review prompt and the hard one is a failure, so the check is a
// test rather than something a growing file can do by drifting past a review.
const (
	soft = 250
	hard = 350
)

func TestSourceFilesStayWithinTheirLineLimits(t *testing.T) {
	walk := func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		lines := strings.Count(readSource(t, path), "\n")
		relative := display(t, path)
		switch {
		case lines > hard:
			t.Errorf("%s has %d lines, past the hard limit", relative, lines)
		case lines > soft:
			t.Errorf("%s has %d lines, past the soft limit; split it by domain role",
				relative, lines)
		}
		return nil
	}
	if err := filepath.WalkDir(moduleRoot(t), walk); err != nil {
		t.Fatal(err)
	}
}

// A document nobody can reach is a document nobody reads.
func TestEveryPublicDocumentIsLinkedFromTheReadme(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join(moduleRoot(t), "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	walk := func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".md") {
			return err
		}
		relative := filepath.ToSlash(display(t, path))
		if !strings.Contains(string(readme), relative) {
			t.Errorf("%s is not linked from README.md", relative)
		}
		return nil
	}
	if err := filepath.WalkDir(filepath.Join(moduleRoot(t), "docs"), walk); err != nil {
		t.Fatal(err)
	}
}
