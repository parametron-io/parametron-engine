package semanticmap

import (
	"errors"
	"reflect"
	"testing"
)

func TestInspectSchemaVersionModelsExactAndFutureVersionStates(t *testing.T) {
	cases := []struct {
		version    string
		wantStatus VersionStatus
		wantParsed bool
		wantMajor  int
		wantMinor  int
		wantSupMaj int
		wantSupMin int
	}{
		{version: "", wantStatus: VersionStatusMissing, wantSupMaj: 1, wantSupMin: 0},
		{version: "1", wantStatus: VersionStatusMalformed, wantSupMaj: 1, wantSupMin: 0},
		{version: "01.0", wantStatus: VersionStatusMalformed, wantSupMaj: 1, wantSupMin: 0},
		{version: "1.01", wantStatus: VersionStatusMalformed, wantSupMaj: 1, wantSupMin: 0},
		{version: " 1.0", wantStatus: VersionStatusMalformed, wantSupMaj: 1, wantSupMin: 0},
		{version: "1.0 ", wantStatus: VersionStatusMalformed, wantSupMaj: 1, wantSupMin: 0},
		{version: "1.0", wantStatus: VersionStatusSupported, wantParsed: true, wantMajor: 1, wantMinor: 0, wantSupMaj: 1, wantSupMin: 0},
		{version: "1.1", wantStatus: VersionStatusUnsupportedSameMajor, wantParsed: true, wantMajor: 1, wantMinor: 1, wantSupMaj: 1, wantSupMin: 0},
		{version: "2.0", wantStatus: VersionStatusUnsupportedMajor, wantParsed: true, wantMajor: 2, wantMinor: 0, wantSupMaj: 1, wantSupMin: 0},
	}

	for _, tc := range cases {
		t.Run(tc.version, func(t *testing.T) {
			got := InspectSchemaVersion(tc.version)
			if got.Raw != tc.version {
				t.Fatalf("InspectSchemaVersion(%q).Raw = %q", tc.version, got.Raw)
			}
			if got.Status != tc.wantStatus {
				t.Fatalf("InspectSchemaVersion(%q).Status = %q, want %q", tc.version, got.Status, tc.wantStatus)
			}
			if got.Parsed != tc.wantParsed {
				t.Fatalf("InspectSchemaVersion(%q).Parsed = %v, want %v", tc.version, got.Parsed, tc.wantParsed)
			}
			if got.Major != tc.wantMajor || got.Minor != tc.wantMinor {
				t.Fatalf("InspectSchemaVersion(%q) parsed version = %d.%d, want %d.%d", tc.version, got.Major, got.Minor, tc.wantMajor, tc.wantMinor)
			}
			if got.SupportedMajor != tc.wantSupMaj || got.SupportedMinor != tc.wantSupMin {
				t.Fatalf("InspectSchemaVersion(%q) supported version = %d.%d, want %d.%d", tc.version, got.SupportedMajor, got.SupportedMinor, tc.wantSupMaj, tc.wantSupMin)
			}
		})
	}
}

func TestClassifySchemaVersionDistinguishesStrictStatuses(t *testing.T) {
	cases := []struct {
		version string
		want    VersionStatus
	}{
		{version: "", want: VersionStatusMissing},
		{version: "1", want: VersionStatusMalformed},
		{version: "01.0", want: VersionStatusMalformed},
		{version: "1.01", want: VersionStatusMalformed},
		{version: " 1.0", want: VersionStatusMalformed},
		{version: "1.0 ", want: VersionStatusMalformed},
		{version: "1.0", want: VersionStatusSupported},
		{version: "1.1", want: VersionStatusUnsupportedSameMajor},
		{version: "2.0", want: VersionStatusUnsupportedMajor},
	}

	for _, tc := range cases {
		if got := ClassifySchemaVersion(tc.version); got != tc.want {
			t.Fatalf("ClassifySchemaVersion(%q) = %q, want %q", tc.version, got, tc.want)
		}
	}
}

func TestValidateSchemaVersionAcceptsOnlyExactV1Strictly(t *testing.T) {
	if err := ValidateSchemaVersion("1.0"); err != nil {
		t.Fatalf("ValidateSchemaVersion returned error: %v", err)
	}

	cases := []struct {
		version string
		want    string
	}{
		{version: "", want: "schemaVersion is required"},
		{version: "1", want: `schemaVersion "1" is malformed`},
		{version: "01.0", want: `schemaVersion "01.0" is malformed`},
		{version: "1.01", want: `schemaVersion "1.01" is malformed`},
		{version: " 1.0", want: `schemaVersion " 1.0" is malformed`},
		{version: "1.0 ", want: `schemaVersion "1.0 " is malformed`},
		{version: "1.1", want: `schemaVersion "1.1" is not supported; only "1.0" is supported`},
		{version: "2.0", want: `schemaVersion "2.0" is not supported; only "1.0" is supported`},
	}

	for _, tc := range cases {
		err := ValidateSchemaVersion(tc.version)
		assertFixtureError(t, err, ErrValidation, tc.want)
	}
}

func TestValidateSchemaVersionIsDeterministicForRepeatedInvalidInput(t *testing.T) {
	first := ValidateSchemaVersion("1.1")
	second := ValidateSchemaVersion("1.1")
	if first == nil || second == nil {
		t.Fatal("expected repeated validation failures")
	}
	if !errors.Is(first, ErrValidation) || !errors.Is(second, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v and %v", first, second)
	}

	var firstErr *ValidationError
	if !errors.As(first, &firstErr) {
		t.Fatalf("expected first ValidationError, got %T", first)
	}
	var secondErr *ValidationError
	if !errors.As(second, &secondErr) {
		t.Fatalf("expected second ValidationError, got %T", second)
	}

	want := []string{`schemaVersion "1.1" is not supported; only "1.0" is supported`}
	if !reflect.DeepEqual(firstErr.Messages(), want) {
		t.Fatalf("unexpected first validation messages\nwant: %#v\ngot:  %#v", want, firstErr.Messages())
	}
	if !reflect.DeepEqual(secondErr.Messages(), want) {
		t.Fatalf("unexpected second validation messages\nwant: %#v\ngot:  %#v", want, secondErr.Messages())
	}
	if first.Error() != second.Error() {
		t.Fatalf("expected deterministic error text\nfirst:  %q\nsecond: %q", first.Error(), second.Error())
	}
}
