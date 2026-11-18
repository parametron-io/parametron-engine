package recordcontract

import "testing"

func TestOutcomeScopedEvidenceRefAcceptsCanonicalAndSafeScopedForms(t *testing.T) {
	const canonical = "raw/fact/prm.fact-evidence.json"
	const relative = "fact/prm.fact-evidence.json"

	cases := map[string]bool{
		canonical: true,
		"raw/outcomes/abc123/fact/prm.fact-evidence.json":  true,
		"raw/outcomes/../fact/prm.fact-evidence.json":      false,
		"raw/outcomes/./fact/prm.fact-evidence.json":       false,
		"raw/outcomes//fact/prm.fact-evidence.json":        false,
		"raw/outcomes/a/b/fact/prm.fact-evidence.json":     false,
		"raw/outcomes/abc123/other/prm.fact-evidence.json": false,
		"/raw/outcomes/abc123/fact/prm.fact-evidence.json": false,
		"raw/fact/other.json":                              false,
	}
	for ref, want := range cases {
		if got := isOutcomeScopedEvidenceRef(ref, canonical, relative); got != want {
			t.Fatalf("isOutcomeScopedEvidenceRef(%q) = %t, want %t", ref, got, want)
		}
	}
}
