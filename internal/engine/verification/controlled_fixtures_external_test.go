package verification_test

import (
	"errors"
	"testing"

	"parametron/internal/engine/verification"
	"parametron/internal/engine/verification/verificationtest"
)

func TestVerify_ControlledFixtures(t *testing.T) {
	tests := []struct {
		name        string
		fixture     verificationtest.Fixture
		wantStatus  verification.Status
		wantClass   verification.FailureClass
		wantMessage string
		wantErr     error
	}{
		{
			name:        "pass",
			fixture:     verificationtest.PassFixture(),
			wantStatus:  verification.StatusPass,
			wantClass:   verification.FailureClassNone,
			wantMessage: "verification passed",
		},
		{
			name:        "metadata mismatch",
			fixture:     verificationtest.MetadataMismatchFixture(),
			wantStatus:  verification.StatusFail,
			wantClass:   verification.FailureClassMetadataMismatch,
			wantMessage: `observed metadata "working_copy_sha256" mismatch: want "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", got "0000000000000000000000000000000000000000000000000000000000000000"`,
			wantErr:     verification.ErrMetadataMismatch,
		},
		{
			name:        "reference mismatch",
			fixture:     verificationtest.ReferenceMismatchFixture(),
			wantStatus:  verification.StatusFail,
			wantClass:   verification.FailureClassReferenceMismatch,
			wantMessage: `observed reference "working_copy_path" mismatch: want "/tmp/out/_working/widget.FCStd", got "/tmp/out/_working/other-widget.FCStd"`,
			wantErr:     verification.ErrReferenceMismatch,
		},
		{
			name:        "required metadata missing",
			fixture:     verificationtest.MissingRequiredMetadataFixture(),
			wantStatus:  verification.StatusFail,
			wantClass:   verification.FailureClassRequiredObservationMissing,
			wantMessage: `required observed metadata "working_copy_sha256" is missing`,
			wantErr:     verification.ErrRequiredObservationMissing,
		},
		{
			name:        "required reference missing",
			fixture:     verificationtest.MissingRequiredReferenceFixture(),
			wantStatus:  verification.StatusFail,
			wantClass:   verification.FailureClassRequiredObservationMissing,
			wantMessage: `required observed reference "working_copy_path" is missing`,
			wantErr:     verification.ErrRequiredObservationMissing,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := verification.Verify(tt.fixture.Contract, tt.fixture.Observed)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("expected pass, got error %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("expected Verify to fail")
				}
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected error %v, got %v", tt.wantErr, err)
				}
			}

			if result.Status != tt.wantStatus {
				t.Fatalf("unexpected status: %+v", result)
			}
			if result.Failure != tt.wantClass {
				t.Fatalf("unexpected failure class: %+v", result)
			}
			if result.Message != tt.wantMessage {
				t.Fatalf("unexpected result message\nwant: %q\ngot:  %q", tt.wantMessage, result.Message)
			}
		})
	}
}
