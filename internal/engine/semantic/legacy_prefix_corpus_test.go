package semantic

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// legacyPrefixTokens are the operation-name-prefix authoring conventions this
// guard understands. "suppress_" and "unsuppress_" are the retired Task 1
// authoring bridge (see intent.go history); the remaining four are pseudo-
// prefix conventions that must never be introduced as a replacement.
var legacyPrefixTokens = []string{
	"unsuppress_",
	"suppress_",
	"unhide_",
	"hide_",
	"delete_",
	"keep_",
}

// retirementAllowedPrefixes are the only prefixes with any current
// intentional Task 1 retirement-regression use. hide_/unhide_/delete_/keep_
// have zero legitimate current uses and are rejected everywhere, in every
// function, with no allowlist.
var retirementAllowedPrefixes = map[string]bool{
	"suppress_":   true,
	"unsuppress_": true,
}

var legacyPrefixIdentifierPattern = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// classifyLegacyPrefixToken reports whether token is shaped like a legacy
// operation-name prefix authoring identifier (e.g. suppress_Pad), and if so
// which prefix it matched. Bare operation-kind strings such as "suppress" or
// "unsuppress" (no trailing name) never match, so canonical
// OperationKind: "suppress" / "unsuppress" values are never flagged.
func classifyLegacyPrefixToken(token string) (prefix string, ok bool) {
	for _, p := range legacyPrefixTokens {
		if strings.HasPrefix(token, p) && len(token) > len(p) {
			return p, true
		}
	}
	return "", false
}

type legacyPrefixHit struct {
	File   string
	Func   string
	Line   int
	Prefix string
	Token  string
}

type legacyPrefixAllowEntry struct {
	File string
	Func string
}

// legacyPrefixCorpusAllowlist is the explicit, exhaustive list of active
// test functions permitted to contain suppress_/unsuppress_-prefixed
// authoring-shaped tokens: the Task 1 permanent retirement regressions that
// deliberately exercise the retired legacy prefix bridge. Every entry here
// must correspond to a function that actually contains a matching token in
// the current corpus, or TestLegacyPrefixCorpus_NoIncidentalAuthoringFixtures
// fails as a stale allowlist entry.
//
// hide_/unhide_/delete_/keep_ are never allowlisted: there is no legitimate
// current use of those pseudo-prefixes anywhere in the corpus.
var legacyPrefixCorpusAllowlist = map[legacyPrefixAllowEntry]bool{
	{File: "internal/engine/semantic/semantic_test.go", Func: "TestInjectDSLIntent_UnmappedPrefixLikeParameterProducesNoMutationOrDiagnostic"}:      true,
	{File: "internal/engine/semantic/semantic_test.go", Func: "TestInjectDSLIntent_PrefixLikeParameterIgnoresFeatureComponentNameCollision"}:        true,
	{File: "internal/engine/semantic/semantic_test.go", Func: "TestInjectDSLIntent_PrefixLikeParametersNeverTriggerTargetResolution"}:                true,
	{File: "internal/engine/semantic/semantic_test.go", Func: "TestInjectDSLIntent_LegacySuppressionPrefixesAreOrdinaryParameters"}:                  true,
	{File: "internal/engine/semantic/semantic_test.go", Func: "TestInjectDSLIntent_LegacySuppressionPrefixNameMapsAsOrdinaryProperty"}:               true,
	{File: "internal/authoring/planner/planner_test.go", Func: "TestCreatePlanWithTablesAndSemanticModel_CaptureBackedManifestPayloadContainsExecutionIntentOnly"}: true,
}

// legacyPrefixCorpusExtensions bounds the guard to active source and fixture
// material: Go test/production source plus DSL and JSON fixture files. Docs
// (*.md) are never scanned here; documentation drift is Stage 3's concern.
var legacyPrefixCorpusExtensions = map[string]bool{
	".go":   true,
	".dsl":  true,
	".json": true,
}

func legacyPrefixLineAt(src []byte, offset int) int {
	return bytes.Count(src[:offset], []byte("\n")) + 1
}

// legacyPrefixCorpusGuardSourceFile is this file's own absolute path,
// resolved once via runtime.Caller. The corpus walk excludes it: this file
// legitimately contains suppress_/unsuppress_/hide_/unhide_/delete_/keep_
// literals as its own allowlist entries and synthetic self-test fixtures,
// which are not authoring-fixture dependencies and must not require
// production allowlist entries of their own.
var legacyPrefixCorpusGuardSourceFile = func() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	return filepath.Clean(file)
}()

// scanGoSourceForLegacyPrefixHits parses src as Go source and attributes
// every legacy-prefix-shaped identifier token to its enclosing top-level
// function, including that function's doc comment (so a regression test's
// explanatory comment referencing suppress_/unsuppress_ is attributed to the
// function it documents, not the preceding declaration). Tokens outside any
// function (imports, package-level decls) are attributed to the empty
// function name, which never matches an allowlist entry.
func scanGoSourceForLegacyPrefixHits(path string, src []byte) ([]legacyPrefixHit, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	type funcRange struct {
		name       string
		start, end int
	}
	var ranges []funcRange
	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		startPos := fd.Pos()
		if fd.Doc != nil {
			startPos = fd.Doc.Pos()
		}
		ranges = append(ranges, funcRange{
			name:  fd.Name.Name,
			start: fset.Position(startPos).Offset,
			end:   fset.Position(fd.End()).Offset,
		})
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].start < ranges[j].start })

	funcAt := func(offset int) string {
		for _, r := range ranges {
			if offset >= r.start && offset < r.end {
				return r.name
			}
		}
		return ""
	}

	var hits []legacyPrefixHit
	for _, loc := range legacyPrefixIdentifierPattern.FindAllIndex(src, -1) {
		tok := string(src[loc[0]:loc[1]])
		prefix, ok := classifyLegacyPrefixToken(tok)
		if !ok {
			continue
		}
		hits = append(hits, legacyPrefixHit{
			File:   path,
			Func:   funcAt(loc[0]),
			Line:   legacyPrefixLineAt(src, loc[0]),
			Prefix: prefix,
			Token:  tok,
		})
	}
	return hits, nil
}

// scanPlainSourceForLegacyPrefixHits scans non-Go fixture material (DSL,
// JSON) for legacy-prefix-shaped tokens. There is no function structure to
// attribute hits to, so Func is always empty, which never matches an
// allowlist entry: no fixture is ever allowlisted.
func scanPlainSourceForLegacyPrefixHits(path string, src []byte) []legacyPrefixHit {
	var hits []legacyPrefixHit
	for _, loc := range legacyPrefixIdentifierPattern.FindAllIndex(src, -1) {
		tok := string(src[loc[0]:loc[1]])
		prefix, ok := classifyLegacyPrefixToken(tok)
		if !ok {
			continue
		}
		hits = append(hits, legacyPrefixHit{
			File:   path,
			Line:   legacyPrefixLineAt(src, loc[0]),
			Prefix: prefix,
			Token:  tok,
		})
	}
	return hits
}

// legacyPrefixRepoRoot locates the repository root from this test file's own
// location (walking up to the nearest go.mod), rather than trusting the
// process working directory, so the scan is hermetic under `go test` from
// any invocation directory.
func legacyPrefixRepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("unable to determine current test file path via runtime.Caller")
	}
	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("unable to locate repository root: no go.mod found above legacy_prefix_corpus_test.go")
		}
		dir = parent
	}
}

// walkLegacyPrefixCorpus deterministically scans internal/** and testdata/**
// (the active Engine test/fixture corpus relevant to Task 2) and returns all
// legacy-prefix-shaped hits, sorted by file, then line, then token.
func walkLegacyPrefixCorpus(t *testing.T, root string) []legacyPrefixHit {
	t.Helper()
	var hits []legacyPrefixHit
	for _, dir := range []string{"internal", "testdata"} {
		start := filepath.Join(root, dir)
		if _, err := os.Stat(start); err != nil {
			continue
		}
		err := filepath.WalkDir(start, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			if !legacyPrefixCorpusExtensions[filepath.Ext(path)] {
				return nil
			}
			if filepath.Clean(path) == legacyPrefixCorpusGuardSourceFile {
				return nil
			}
			src, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			if filepath.Ext(path) == ".go" {
				fileHits, parseErr := scanGoSourceForLegacyPrefixHits(path, src)
				if parseErr != nil {
					return parseErr
				}
				hits = append(hits, fileHits...)
				return nil
			}
			hits = append(hits, scanPlainSourceForLegacyPrefixHits(path, src)...)
			return nil
		})
		if err != nil {
			t.Fatalf("failed walking %s for legacy-prefix corpus scan: %v", start, err)
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].File != hits[j].File {
			return hits[i].File < hits[j].File
		}
		if hits[i].Line != hits[j].Line {
			return hits[i].Line < hits[j].Line
		}
		return hits[i].Token < hits[j].Token
	})
	return hits
}

// evaluateLegacyPrefixHits classifies scanned hits against the allowlist. A
// hit is accepted only if its prefix is one of the two retirement-regression
// prefixes ("suppress_"/"unsuppress_") AND its (File, Func) pair is
// explicitly allowlisted; hide_/unhide_/delete_/keep_ hits are never
// accepted regardless of location. Every allowlist entry must be matched by
// at least one hit, or it is reported as stale.
func evaluateLegacyPrefixHits(hits []legacyPrefixHit, allow map[legacyPrefixAllowEntry]bool) (unexpected []legacyPrefixHit, stale []legacyPrefixAllowEntry) {
	matched := make(map[legacyPrefixAllowEntry]bool, len(allow))
	for _, hit := range hits {
		if retirementAllowedPrefixes[hit.Prefix] {
			entry := legacyPrefixAllowEntry{File: hit.File, Func: hit.Func}
			if allow[entry] {
				matched[entry] = true
				continue
			}
		}
		unexpected = append(unexpected, hit)
	}
	for entry := range allow {
		if !matched[entry] {
			stale = append(stale, entry)
		}
	}
	sort.Slice(unexpected, func(i, j int) bool {
		if unexpected[i].File != unexpected[j].File {
			return unexpected[i].File < unexpected[j].File
		}
		if unexpected[i].Line != unexpected[j].Line {
			return unexpected[i].Line < unexpected[j].Line
		}
		return unexpected[i].Token < unexpected[j].Token
	})
	sort.Slice(stale, func(i, j int) bool {
		if stale[i].File != stale[j].File {
			return stale[i].File < stale[j].File
		}
		return stale[i].Func < stale[j].Func
	})
	return unexpected, stale
}

// TestLegacyPrefixCorpus_NoIncidentalAuthoringFixtures is the permanent
// Task 2 corpus guard. It scans the active internal/** and testdata/**
// corpus for suppress_/unsuppress_/hide_/unhide_/delete_/keep_-prefixed
// authoring-shaped identifiers and fails if any occurrence is not explicitly
// allowlisted as an intentional Task 1 retirement regression. Bare
// OperationKind "suppress"/"unsuppress" strings are never flagged (they lack
// the trailing underscore/name that makes a token prefix-shaped).
//
// This test fails permanently if TestInjectDSLIntent_StructuredIntentOrderingIsDeterministic
// or TestCaptureBackedManifest_RejectsOrStripsSemanticFieldInjection (the two
// Stage 1-cleaned tests) ever regain a suppress_/unsuppress_ declaration,
// because neither is on the allowlist.
func TestLegacyPrefixCorpus_NoIncidentalAuthoringFixtures(t *testing.T) {
	root := legacyPrefixRepoRoot(t)
	hits := walkLegacyPrefixCorpus(t, root)
	for i := range hits {
		rel, err := filepath.Rel(root, hits[i].File)
		if err != nil {
			t.Fatalf("failed to relativize %s against repo root %s: %v", hits[i].File, root, err)
		}
		hits[i].File = filepath.ToSlash(rel)
	}

	unexpected, stale := evaluateLegacyPrefixHits(hits, legacyPrefixCorpusAllowlist)

	if len(unexpected) > 0 {
		var b strings.Builder
		b.WriteString("unexpected legacy-prefix authoring fixtures found (not on the intentional Task 1 retirement allowlist):\n")
		for _, hit := range unexpected {
			fmt.Fprintf(&b, "  %s:%d func=%q token=%q prefix=%q\n", hit.File, hit.Line, hit.Func, hit.Token, hit.Prefix)
		}
		t.Fatal(b.String())
	}

	if len(stale) > 0 {
		var b strings.Builder
		b.WriteString("legacy-prefix corpus allowlist entries matched no suppress_/unsuppress_ token in the corpus (stale allowlist entry - update legacyPrefixCorpusAllowlist):\n")
		for _, entry := range stale {
			fmt.Fprintf(&b, "  %s func=%q\n", entry.File, entry.Func)
		}
		t.Fatal(b.String())
	}
}

// TestClassifyLegacyPrefixToken proves the token classifier's boundary
// behavior in isolation: prefix-shaped tokens are matched, bare canonical
// OperationKind strings and unrelated identifiers are not.
func TestClassifyLegacyPrefixToken(t *testing.T) {
	tests := []struct {
		token      string
		wantPrefix string
		wantOK     bool
	}{
		{"suppress_Foo", "suppress_", true},
		{"suppress_Pad", "suppress_", true},
		{"suppress_keyway", "suppress_", true},
		{"unsuppress_Bar", "unsuppress_", true},
		{"unsuppress_Pad", "unsuppress_", true},
		{"hide_Foo", "hide_", true},
		{"unhide_Foo", "unhide_", true},
		{"delete_Foo", "delete_", true},
		{"keep_Foo", "keep_", true},
		{"suppress", "", false},
		{"unsuppress", "", false},
		{"suppress_", "", false},
		{"unsuppress_", "", false},
		{"hide", "", false},
		{"unhide", "", false},
		{"delete", "", false},
		{"keep", "", false},
		{"Suppression", "", false},
		{"hidden_Foo", "", false},
		{"OperationKind", "", false},
		{"suppressed_Foo", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.token, func(t *testing.T) {
			prefix, ok := classifyLegacyPrefixToken(tt.token)
			if ok != tt.wantOK || prefix != tt.wantPrefix {
				t.Fatalf("classifyLegacyPrefixToken(%q) = (%q, %v), want (%q, %v)", tt.token, prefix, ok, tt.wantPrefix, tt.wantOK)
			}
		})
	}
}

// TestScanGoSourceForLegacyPrefixHits_AttributesToEnclosingFunction proves
// the AST-based scanner attributes hits (including hits inside a function's
// doc comment) to the correct enclosing top-level function, and that bare
// OperationKind-shaped string literals never produce a hit. Synthetic
// in-memory source is used so no fixture file needs to exist on disk.
func TestScanGoSourceForLegacyPrefixHits_AttributesToEnclosingFunction(t *testing.T) {
	src := []byte(`package example

// TestRetirementRegression proves suppress_/unsuppress_ retirement.
func TestRetirementRegression(t *testing.T) {
	_ = "suppress_Keyway"
	_ = "unsuppress_Pad"
}

func TestOperationKindOnly(t *testing.T) {
	kind := "suppress"
	other := "unsuppress"
	_ = kind
	_ = other
}

func TestPseudoPrefixes(t *testing.T) {
	_ = "hide_Foo"
	_ = "unhide_Foo"
	_ = "delete_Foo"
	_ = "keep_Foo"
}
`)

	hits, err := scanGoSourceForLegacyPrefixHits("synthetic_test.go", src)
	if err != nil {
		t.Fatalf("scanGoSourceForLegacyPrefixHits returned error: %v", err)
	}

	byFunc := make(map[string]map[string]bool)
	for _, hit := range hits {
		if byFunc[hit.Func] == nil {
			byFunc[hit.Func] = make(map[string]bool)
		}
		byFunc[hit.Func][hit.Token] = true
	}

	if !byFunc["TestRetirementRegression"]["suppress_Keyway"] || !byFunc["TestRetirementRegression"]["unsuppress_Pad"] {
		t.Fatalf("expected suppress_Keyway and unsuppress_Pad attributed to TestRetirementRegression, got %#v", byFunc["TestRetirementRegression"])
	}
	// The doc comment above TestRetirementRegression also mentions
	// suppress_/unsuppress_; it must attribute to that function, not leak
	// into TestOperationKindOnly (the previous declaration in the file).
	if byFunc["TestOperationKindOnly"] != nil {
		t.Fatalf("expected no legacy-prefix hits attributed to TestOperationKindOnly, got %#v", byFunc["TestOperationKindOnly"])
	}
	if !byFunc["TestPseudoPrefixes"]["hide_Foo"] || !byFunc["TestPseudoPrefixes"]["unhide_Foo"] ||
		!byFunc["TestPseudoPrefixes"]["delete_Foo"] || !byFunc["TestPseudoPrefixes"]["keep_Foo"] {
		t.Fatalf("expected all four pseudo-prefix tokens attributed to TestPseudoPrefixes, got %#v", byFunc["TestPseudoPrefixes"])
	}
}

// TestEvaluateLegacyPrefixHits is the preferred synthetic positive/negative
// proof for the guard's accept/reject decision logic, independent of real
// files: intentional allowlisted suppress_/unsuppress_ hits are accepted;
// the same prefixes in a non-allowlisted function or file are rejected; and
// hide_/unhide_/delete_/keep_ are rejected unconditionally, even inside an
// otherwise-allowlisted function.
func TestEvaluateLegacyPrefixHits(t *testing.T) {
	allow := map[legacyPrefixAllowEntry]bool{
		{File: "internal/pkg/example_test.go", Func: "TestRetirementRegression"}: true,
	}

	hits := []legacyPrefixHit{
		{File: "internal/pkg/example_test.go", Func: "TestRetirementRegression", Prefix: "suppress_", Token: "suppress_Keyway", Line: 10},
		{File: "internal/pkg/example_test.go", Func: "TestRetirementRegression", Prefix: "unsuppress_", Token: "unsuppress_Pad", Line: 11},
		{File: "internal/pkg/example_test.go", Func: "TestUnrelated", Prefix: "suppress_", Token: "suppress_Foo", Line: 20},
		{File: "internal/pkg/other_test.go", Func: "TestOther", Prefix: "unsuppress_", Token: "unsuppress_Bar", Line: 5},
		{File: "internal/pkg/example_test.go", Func: "TestRetirementRegression", Prefix: "hide_", Token: "hide_Foo", Line: 12},
		{File: "internal/pkg/example_test.go", Func: "TestRetirementRegression", Prefix: "unhide_", Token: "unhide_Foo", Line: 13},
		{File: "internal/pkg/example_test.go", Func: "TestRetirementRegression", Prefix: "delete_", Token: "delete_Foo", Line: 14},
		{File: "internal/pkg/example_test.go", Func: "TestRetirementRegression", Prefix: "keep_", Token: "keep_Foo", Line: 15},
		{File: "examples/fixture/box.dsl", Func: "", Prefix: "suppress_", Token: "suppress_Fixture", Line: 3},
	}

	unexpected, stale := evaluateLegacyPrefixHits(hits, allow)

	wantUnexpectedTokens := []string{
		"delete_Foo", "hide_Foo", "keep_Foo", "suppress_Fixture", "suppress_Foo", "unhide_Foo", "unsuppress_Bar",
	}
	gotTokens := make([]string, 0, len(unexpected))
	for _, hit := range unexpected {
		gotTokens = append(gotTokens, hit.Token)
	}
	sort.Strings(gotTokens)
	if !reflect.DeepEqual(gotTokens, wantUnexpectedTokens) {
		t.Fatalf("unexpected-hit set mismatch\nwant: %#v\ngot:  %#v", wantUnexpectedTokens, gotTokens)
	}
	if len(stale) != 0 {
		t.Fatalf("expected no stale allowlist entries, got %#v", stale)
	}
}

// TestEvaluateLegacyPrefixHits_DetectsStaleAllowlistEntry proves the guard
// also catches the opposite drift: an allowlist entry whose corresponding
// intentional regression fixture was removed or renamed without updating
// legacyPrefixCorpusAllowlist.
func TestEvaluateLegacyPrefixHits_DetectsStaleAllowlistEntry(t *testing.T) {
	allow := map[legacyPrefixAllowEntry]bool{
		{File: "internal/pkg/example_test.go", Func: "TestRetirementRegression"}: true,
	}

	unexpected, stale := evaluateLegacyPrefixHits(nil, allow)
	if len(unexpected) != 0 {
		t.Fatalf("expected no unexpected hits, got %#v", unexpected)
	}
	if len(stale) != 1 || stale[0].Func != "TestRetirementRegression" {
		t.Fatalf("expected exactly one stale allowlist entry for TestRetirementRegression, got %#v", stale)
	}
}
