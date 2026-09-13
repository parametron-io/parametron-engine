package artifact

import "testing"

func TestExportOutputContract_SupportedFormatsNormalize(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "step", want: ExportOutputTypeSTEP},
		{input: " csv ", want: ExportOutputTypeCSV},
		{input: "PDF", want: ExportOutputTypePDF},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			if got := NormalizeExportOutputType(tc.input); got != tc.want {
				t.Fatalf("NormalizeExportOutputType(%q) = %q, want %q", tc.input, got, tc.want)
			}
			if !IsSupportedExportOutputType(tc.input) {
				t.Fatalf("expected %q to be supported", tc.input)
			}
			if _, ok := ExportOutputSpecFor(tc.input); !ok {
				t.Fatalf("expected a contract specification for %q", tc.input)
			}
		})
	}
}

func TestExportOutputContract_UnsupportedFormatsRejected(t *testing.T) {
	for _, outputType := range []string{"", "   ", "stl", "dxf", "svg", "json"} {
		t.Run(outputType, func(t *testing.T) {
			if IsSupportedExportOutputType(outputType) {
				t.Fatalf("expected %q to be unsupported", outputType)
			}
			if _, ok := ExportOutputSpecFor(outputType); ok {
				t.Fatalf("expected no contract specification for %q", outputType)
			}
		})
	}
}
