package recordcontract

import "strings"

// isOutcomeScopedEvidenceRef reports whether ref is the canonical raw evidence
// path or its outcome-scoped form under raw/outcomes/<outcomeKey>/.
//
// The normalized contract owns the accepted package-path shape, but package
// layout naming remains owned by recordpackage. A single safe outcome segment is
// required so raw evidence paths cannot escape their outcome scope.
func isOutcomeScopedEvidenceRef(ref, canonical, relative string) bool {
	if ref == canonical {
		return true
	}
	const prefix = "raw/outcomes/"
	suffix := "/" + relative
	if !strings.HasPrefix(ref, prefix) || !strings.HasSuffix(ref, suffix) {
		return false
	}
	outcomeKey := strings.TrimSuffix(strings.TrimPrefix(ref, prefix), suffix)
	if outcomeKey == "" || outcomeKey == "." || outcomeKey == ".." {
		return false
	}
	return !strings.ContainsAny(outcomeKey, "/\\\x00")
}
