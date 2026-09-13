package recordcontract

import "testing"

func TestJSONSchemasRequiredForPhase2Exit(t *testing.T) {
	if JSONSchemasRequiredForPhase2Exit() {
		t.Fatal("JSONSchemasRequiredForPhase2Exit() = true, want false")
	}
}
