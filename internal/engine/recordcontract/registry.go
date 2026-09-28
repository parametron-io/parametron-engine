package recordcontract

import "fmt"

// Definition describes a registered Engine-produced record contract family.
type Definition struct {
	Family    Family
	FileName  string
	Version   string
	Ownership Ownership
}

var registryDefinitions = [...]Definition{
	{
		Family:    FamilyExecution,
		FileName:  "prm.execution-record.json",
		Version:   CurrentVersion,
		Ownership: EngineProducedOwnership(),
	},
	{
		Family:    FamilyArtifact,
		FileName:  "prm.artifact-record.json",
		Version:   CurrentVersion,
		Ownership: EngineProducedOwnership(),
	},
	{
		Family:    FamilyObservation,
		FileName:  "prm.observation-record.json",
		Version:   CurrentVersion,
		Ownership: EngineProducedOwnership(),
	},
	{
		Family:    FamilyReference,
		FileName:  "prm.reference-record.json",
		Version:   CurrentVersion,
		Ownership: EngineProducedOwnership(),
	},
	{
		Family:    FamilyFailure,
		FileName:  "prm.failure-record.json",
		Version:   CurrentVersion,
		Ownership: EngineProducedOwnership(),
	},
	{
		Family:    FamilyVerification,
		FileName:  "prm.verification-record.json",
		Version:   CurrentVersion,
		Ownership: EngineProducedOwnership(),
	},
}

// Definitions returns all registered contract definitions in deterministic logical order.
func Definitions() []Definition {
	out := make([]Definition, len(registryDefinitions))
	copy(out, registryDefinitions[:])
	return out
}

// Lookup returns the definition for family after trimming whitespace.
func Lookup(family Family) (Definition, bool) {
	normalized := NormalizeFamily(family)
	for _, def := range registryDefinitions {
		if def.Family == normalized {
			return def, true
		}
	}
	return Definition{}, false
}

// MustLookup returns the definition for family or panics when family is unknown.
func MustLookup(family Family) Definition {
	def, ok := Lookup(family)
	if !ok {
		panic(fmt.Sprintf("recordcontract: unknown family %q", family))
	}
	return def
}

// FileNameForFamily returns the candidate contract filename for family.
func FileNameForFamily(family Family) (string, bool) {
	def, ok := Lookup(family)
	if !ok {
		return "", false
	}
	return def.FileName, true
}
