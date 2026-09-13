package recordcontract

import (
	"errors"
	"reflect"
	"testing"
)

var expectedDefinitions = []Definition{
	{
		Family:    FamilyExecution,
		FileName:  "parametron.execution-record.json",
		Version:   CurrentVersion,
		Ownership: EngineProducedOwnership(),
	},
	{
		Family:    FamilyArtifact,
		FileName:  "parametron.artifact-record.json",
		Version:   CurrentVersion,
		Ownership: EngineProducedOwnership(),
	},
	{
		Family:    FamilyObservation,
		FileName:  "parametron.observation-record.json",
		Version:   CurrentVersion,
		Ownership: EngineProducedOwnership(),
	},
	{
		Family:    FamilyReference,
		FileName:  "parametron.reference-record.json",
		Version:   CurrentVersion,
		Ownership: EngineProducedOwnership(),
	},
	{
		Family:    FamilyFailure,
		FileName:  "parametron.failure-record.json",
		Version:   CurrentVersion,
		Ownership: EngineProducedOwnership(),
	},
	{
		Family:    FamilyVerification,
		FileName:  "parametron.verification-record.json",
		Version:   CurrentVersion,
		Ownership: EngineProducedOwnership(),
	},
}

func TestFamilyValidationAndNormalization(t *testing.T) {
	for _, def := range expectedDefinitions {
		t.Run(string(def.Family), func(t *testing.T) {
			if !KnownFamily(def.Family) {
				t.Fatalf("KnownFamily(%q) = false, want true", def.Family)
			}
			if err := ValidateFamily(def.Family); err != nil {
				t.Fatalf("ValidateFamily(%q) returned error: %v", def.Family, err)
			}
		})
	}

	for _, family := range []Family{"", " \t\n ", "unknown", "Execution"} {
		t.Run("reject "+string(family), func(t *testing.T) {
			if KnownFamily(family) {
				t.Fatalf("KnownFamily(%q) = true, want false", family)
			}
			if err := ValidateFamily(family); !errors.Is(err, ErrUnknownFamily) {
				t.Fatalf("ValidateFamily(%q) error = %v, want ErrUnknownFamily", family, err)
			}
		})
	}

	normalizationCases := []struct {
		name string
		in   Family
		want Family
	}{
		{name: "leading trailing whitespace", in: " \t execution\n", want: FamilyExecution},
		{name: "preserve case", in: " Execution ", want: "Execution"},
		{name: "preserve inner whitespace", in: "execu tion", want: "execu tion"},
	}

	for _, tc := range normalizationCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeFamily(tc.in); got != tc.want {
				t.Fatalf("NormalizeFamily(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestDefinitionsDeterministicOrderAndContents(t *testing.T) {
	got := Definitions()
	if !reflect.DeepEqual(got, expectedDefinitions) {
		t.Fatalf("Definitions() = %#v, want %#v", got, expectedDefinitions)
	}

	for _, def := range got {
		if !KnownFamily(def.Family) {
			t.Fatalf("definition family %q is not known", def.Family)
		}
		if def.Version != SupportedVersion() {
			t.Fatalf("definition %q version = %q, want %q", def.Family, def.Version, SupportedVersion())
		}
		if def.Ownership != EngineProducedOwnership() {
			t.Fatalf("definition %q ownership = %#v, want %#v", def.Family, def.Ownership, EngineProducedOwnership())
		}
	}
}

func TestDefinitionsCopySafety(t *testing.T) {
	got := Definitions()
	got[0] = Definition{
		Family:    "mutated",
		FileName:  "mutated.json",
		Version:   "99.99",
		Ownership: Ownership{Producer: OwnerPDMServer},
	}

	again := Definitions()
	if !reflect.DeepEqual(again, expectedDefinitions) {
		t.Fatalf("Definitions() after caller mutation = %#v, want %#v", again, expectedDefinitions)
	}
}

func TestLookupAndFileNameHelpers(t *testing.T) {
	for _, want := range expectedDefinitions {
		t.Run(string(want.Family), func(t *testing.T) {
			got, ok := Lookup(want.Family)
			if !ok {
				t.Fatalf("Lookup(%q) ok = false, want true", want.Family)
			}
			if got != want {
				t.Fatalf("Lookup(%q) = %#v, want %#v", want.Family, got, want)
			}

			got, ok = Lookup(Family(" \t" + string(want.Family) + "\n"))
			if !ok || got != want {
				t.Fatalf("Lookup with whitespace = %#v, %v; want %#v, true", got, ok, want)
			}

			fileName, ok := FileNameForFamily(want.Family)
			if !ok || fileName != want.FileName {
				t.Fatalf("FileNameForFamily(%q) = %q, %v; want %q, true", want.Family, fileName, ok, want.FileName)
			}

			if got := MustLookup(want.Family); got != want {
				t.Fatalf("MustLookup(%q) = %#v, want %#v", want.Family, got, want)
			}
		})
	}

	if _, ok := Lookup("Execution"); ok {
		t.Fatal("Lookup should not case-fold family values")
	}
	if _, ok := Lookup("unknown"); ok {
		t.Fatal("Lookup unknown ok = true, want false")
	}
	if fileName, ok := FileNameForFamily("unknown"); ok || fileName != "" {
		t.Fatalf("FileNameForFamily unknown = %q, %v; want empty, false", fileName, ok)
	}
	if !panics(func() { MustLookup("unknown") }) {
		t.Fatal("MustLookup unknown did not panic")
	}
}

func TestOwnershipDefaultsAndValidation(t *testing.T) {
	want := Ownership{
		Producer:            OwnerEngine,
		LocalOutputEmitter:  OwnerEngine,
		IngestionTarget:     OwnerPDMServer,
		DurableStorageOwner: OwnerPDMServer,
	}
	if got := EngineProducedOwnership(); got != want {
		t.Fatalf("EngineProducedOwnership() = %#v, want %#v", got, want)
	}
	if err := ValidateOwnership(want); err != nil {
		t.Fatalf("ValidateOwnership(default) returned error: %v", err)
	}

	for _, def := range Definitions() {
		if def.Ownership != want {
			t.Fatalf("definition %q ownership = %#v, want %#v", def.Family, def.Ownership, want)
		}
	}
}

func TestValidateOwnershipRejectsInvalidBoundaries(t *testing.T) {
	valid := EngineProducedOwnership()
	tests := []struct {
		name      string
		ownership Ownership
	}{
		{name: "producer not engine", ownership: withOwnership(valid, func(o *Ownership) { o.Producer = OwnerPDMServer })},
		{name: "local output emitter not engine", ownership: withOwnership(valid, func(o *Ownership) { o.LocalOutputEmitter = OwnerPDMServer })},
		{name: "ingestion target not pdm server", ownership: withOwnership(valid, func(o *Ownership) { o.IngestionTarget = OwnerEngine })},
		{name: "durable storage owner not pdm server", ownership: withOwnership(valid, func(o *Ownership) { o.DurableStorageOwner = OwnerEngine })},
		{name: "missing producer", ownership: withOwnership(valid, func(o *Ownership) { o.Producer = "" })},
		{name: "missing local output emitter", ownership: withOwnership(valid, func(o *Ownership) { o.LocalOutputEmitter = "" })},
		{name: "missing ingestion target", ownership: withOwnership(valid, func(o *Ownership) { o.IngestionTarget = "" })},
		{name: "missing durable storage owner", ownership: withOwnership(valid, func(o *Ownership) { o.DurableStorageOwner = "" })},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateOwnership(tc.ownership); !errors.Is(err, ErrInvalidOwnership) {
				t.Fatalf("ValidateOwnership(%#v) error = %v, want ErrInvalidOwnership", tc.ownership, err)
			}
		})
	}
}

func TestSupportedVersion(t *testing.T) {
	if SupportedVersion() != "1.0" {
		t.Fatalf("SupportedVersion() = %q, want 1.0", SupportedVersion())
	}
	if CurrentVersion != "1.0" {
		t.Fatalf("CurrentVersion = %q, want 1.0", CurrentVersion)
	}
	if err := ValidateVersion("1.0"); err != nil {
		t.Fatalf("ValidateVersion(1.0) returned error: %v", err)
	}
	if got := ClassifyVersion("1.0"); got != VersionStatusSupported {
		t.Fatalf("ClassifyVersion(1.0) = %q, want %q", got, VersionStatusSupported)
	}

	info := InspectVersion("1.0")
	if info.Raw != "1.0" ||
		info.Status != VersionStatusSupported ||
		!info.Parsed ||
		info.Major != 1 ||
		info.Minor != 0 ||
		info.SupportedMajor != 1 ||
		info.SupportedMinor != 0 {
		t.Fatalf("InspectVersion(1.0) = %#v", info)
	}
}

func TestVersionClassificationAndValidation(t *testing.T) {
	tests := []struct {
		version string
		want    VersionStatus
	}{
		{version: "1.0", want: VersionStatusSupported},
		{version: "", want: VersionStatusMissing},
		{version: " ", want: VersionStatusMalformed},
		{version: "1", want: VersionStatusMalformed},
		{version: "1.", want: VersionStatusMalformed},
		{version: ".0", want: VersionStatusMalformed},
		{version: "1.0.0", want: VersionStatusMalformed},
		{version: "01.0", want: VersionStatusMalformed},
		{version: "1.01", want: VersionStatusMalformed},
		{version: "v1.0", want: VersionStatusMalformed},
		{version: "1.a", want: VersionStatusMalformed},
		{version: "a.0", want: VersionStatusMalformed},
		{version: "-1.0", want: VersionStatusMalformed},
		{version: "1.-1", want: VersionStatusMalformed},
		{version: " 1.0", want: VersionStatusMalformed},
		{version: "1.0 ", want: VersionStatusMalformed},
		{version: "1 .0", want: VersionStatusMalformed},
		{version: "1. 0", want: VersionStatusMalformed},
		{version: "1.1", want: VersionStatusUnsupportedSameMajor},
		{version: "1.99", want: VersionStatusUnsupportedSameMajor},
		{version: "0.1", want: VersionStatusUnsupportedMajor},
		{version: "2.0", want: VersionStatusUnsupportedMajor},
		{version: "99.0", want: VersionStatusUnsupportedMajor},
	}

	for _, tc := range tests {
		t.Run(tc.version, func(t *testing.T) {
			if got := ClassifyVersion(tc.version); got != tc.want {
				t.Fatalf("ClassifyVersion(%q) = %q, want %q", tc.version, got, tc.want)
			}

			err := ValidateVersion(tc.version)
			if tc.want == VersionStatusSupported {
				if err != nil {
					t.Fatalf("ValidateVersion(%q) returned error: %v", tc.version, err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidVersion) {
				t.Fatalf("ValidateVersion(%q) error = %v, want ErrInvalidVersion", tc.version, err)
			}
		})
	}
}

func TestInspectVersionDetails(t *testing.T) {
	tests := []struct {
		version string
		want    VersionInfo
	}{
		{
			version: "1.0",
			want: VersionInfo{
				Raw:            "1.0",
				Status:         VersionStatusSupported,
				Major:          1,
				Minor:          0,
				SupportedMajor: 1,
				SupportedMinor: 0,
				Parsed:         true,
			},
		},
		{
			version: "1.1",
			want: VersionInfo{
				Raw:            "1.1",
				Status:         VersionStatusUnsupportedSameMajor,
				Major:          1,
				Minor:          1,
				SupportedMajor: 1,
				SupportedMinor: 0,
				Parsed:         true,
			},
		},
		{
			version: "2.0",
			want: VersionInfo{
				Raw:            "2.0",
				Status:         VersionStatusUnsupportedMajor,
				Major:          2,
				Minor:          0,
				SupportedMajor: 1,
				SupportedMinor: 0,
				Parsed:         true,
			},
		},
		{
			version: "",
			want: VersionInfo{
				Raw:            "",
				Status:         VersionStatusMissing,
				SupportedMajor: 1,
				SupportedMinor: 0,
			},
		},
		{
			version: " 1.0",
			want: VersionInfo{
				Raw:            " 1.0",
				Status:         VersionStatusMalformed,
				SupportedMajor: 1,
				SupportedMinor: 0,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.version, func(t *testing.T) {
			if got := InspectVersion(tc.version); got != tc.want {
				t.Fatalf("InspectVersion(%q) = %#v, want %#v", tc.version, got, tc.want)
			}
			if tc.want.Status != VersionStatusSupported && InspectVersion(tc.version).Status == VersionStatusSupported {
				t.Fatalf("InspectVersion(%q) reported supported for invalid or unsupported version", tc.version)
			}
		})
	}
}

func TestValidationErrorsAreInspectable(t *testing.T) {
	if err := ValidateFamily("unknown"); !errors.Is(err, ErrUnknownFamily) {
		t.Fatalf("ValidateFamily error = %v, want ErrUnknownFamily", err)
	}
	if err := ValidateVersion("2.0"); !errors.Is(err, ErrInvalidVersion) {
		t.Fatalf("ValidateVersion error = %v, want ErrInvalidVersion", err)
	}
	if err := ValidateOwnership(Ownership{}); !errors.Is(err, ErrInvalidOwnership) {
		t.Fatalf("ValidateOwnership error = %v, want ErrInvalidOwnership", err)
	}
}

func withOwnership(base Ownership, mutate func(*Ownership)) Ownership {
	mutate(&base)
	return base
}

func panics(fn func()) (didPanic bool) {
	defer func() {
		if recover() != nil {
			didPanic = true
		}
	}()
	fn()
	return false
}
