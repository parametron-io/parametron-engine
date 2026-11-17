package runtimecap

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestIntegrityRequestEnvAppliesEngineValue(t *testing.T) {
	base := []string{"PATH=/bin", IntegrityRequestEnvVar + "=/inherited/stale.json", "OTHER=value"}

	got := integrityRequestEnv(base, "/engine/integrity/attempt.json")
	want := []string{"PATH=/bin", "OTHER=value", IntegrityRequestEnvVar + "=/engine/integrity/attempt.json"}
	if !slices.Equal(got, want) {
		t.Fatalf("integrityRequestEnv = %#v, want %#v", got, want)
	}
}

func TestIntegrityRequestEnvStripsInheritedValueWithoutEnginePath(t *testing.T) {
	base := []string{"PATH=/bin", IntegrityRequestEnvVar + "=/inherited/stale.json", "OTHER=value"}

	got := integrityRequestEnv(base, "")
	want := []string{"PATH=/bin", "OTHER=value"}
	if !slices.Equal(got, want) {
		t.Fatalf("integrityRequestEnv = %#v, want %#v", got, want)
	}
	prefix := IntegrityRequestEnvVar + "="
	for _, entry := range got {
		if strings.HasPrefix(entry, prefix) {
			t.Fatalf("inherited integrity request entry survived an empty Engine path: %q", entry)
		}
	}
}

func TestNormalizeRequestIntegrityRequestPath(t *testing.T) {
	normalized, err := NormalizeRequest(Request{
		RuntimeCommand:       "parametron-freecad",
		WorkingCopyDir:       "/work",
		ManifestPath:         "/work/manifest.json",
		ResultPath:           "/work/result.json",
		IntegrityRequestPath: "  /engine/integrity/attempt.json  ",
	})
	if err != nil {
		t.Fatalf("NormalizeRequest returned error: %v", err)
	}
	if normalized.IntegrityRequestPath != "/engine/integrity/attempt.json" {
		t.Fatalf("IntegrityRequestPath = %q, want the trimmed path", normalized.IntegrityRequestPath)
	}
	if _, err := NormalizeRequest(Request{
		RuntimeCommand:       "parametron-freecad",
		WorkingCopyDir:       "/work",
		ManifestPath:         "/work/manifest.json",
		ResultPath:           "/work/result.json",
		IntegrityRequestPath: "   ",
	}); err == nil {
		t.Fatal("NormalizeRequest accepted a whitespace-only integrity request path")
	}
}

func TestBuildCommandKeepsArgvIndependentOfIntegrityRequestPath(t *testing.T) {
	base := Request{
		RuntimeCommand:                "parametron-freecad",
		WorkingCopyDir:                "/work",
		ManifestPath:                  "/work/manifest.json",
		ResultPath:                    "/work/result.json",
		OutputDir:                     "/work/outputs",
		ObservationRequestPath:        "/work/prm.verification.json",
		ReferenceTraversalRequestPath: "/work/prm.reference-traversal-request.json",
	}
	withoutIntegrity, err := BuildCommand(base)
	if err != nil {
		t.Fatal(err)
	}
	withPath := base
	withPath.IntegrityRequestPath = "/engine/integrity/attempt.json"
	withIntegrity, err := BuildCommand(withPath)
	if err != nil {
		t.Fatal(err)
	}
	if withIntegrity.Path != withoutIntegrity.Path || !slices.Equal(withIntegrity.Args, withoutIntegrity.Args) {
		t.Fatalf("integrity request path changed argv:\nwithout: %#v\nwith:    %#v", withoutIntegrity, withIntegrity)
	}
	for _, arg := range withIntegrity.Args {
		if strings.Contains(arg, "/engine/integrity") {
			t.Fatalf("integrity request path leaked into argv: %#v", withIntegrity.Args)
		}
	}
}

func TestExternalCapabilityTransportsIntegrityRequestThroughEnvironment(t *testing.T) {
	dir := t.TempDir()
	fake := writeFakeRuntime(t, dir, "parametron-freecad", `
if [ -n "$PARAMETRON_RUNTIME_INTEGRITY_REQUEST" ]; then
    echo "integrity=$PARAMETRON_RUNTIME_INTEGRITY_REQUEST"
else
    echo "integrity=<unset>"
fi
printf '%s\n' "$@"
`)
	req := validExternalRequest(fake, dir)
	req.IntegrityRequestPath = filepath.Join(dir, "integrity", "attempt.json")
	t.Setenv(IntegrityRequestEnvVar, "/inherited/stale.json")

	result, err := (ExternalCapability{}).Invoke(context.Background(), req)
	if err != nil {
		t.Fatalf("Invoke returned error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(result.Stdout), "\n")
	if len(lines) == 0 || lines[0] != "integrity="+req.IntegrityRequestPath {
		t.Fatalf("runtime environment = %q, want the Engine-authored integrity request path", result.Stdout)
	}
	gotArgs := lines[1:]
	wantArgs := []string{
		"execute",
		"--working-copy", req.WorkingCopyDir,
		"--manifest", req.ManifestPath,
		"--result", req.ResultPath,
	}
	if !slices.Equal(gotArgs, wantArgs) {
		t.Fatalf("argv = %v, want unchanged aligned argv %v", gotArgs, wantArgs)
	}
}

func TestExternalCapabilityOmitsIntegrityRequestEnvironmentWhenAbsent(t *testing.T) {
	tests := []struct {
		name      string
		inherited string
	}{
		{name: "no inherited value", inherited: ""},
		{name: "inherited stale value", inherited: "/inherited/stale.json"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			fake := writeFakeRuntime(t, dir, "parametron-freecad", `
if [ -n "$PARAMETRON_RUNTIME_INTEGRITY_REQUEST" ]; then
    echo "integrity=$PARAMETRON_RUNTIME_INTEGRITY_REQUEST"
else
    echo "integrity=<unset>"
fi
`)
			req := validExternalRequest(fake, dir)
			t.Setenv(IntegrityRequestEnvVar, tc.inherited)

			result, err := (ExternalCapability{}).Invoke(context.Background(), req)
			if err != nil {
				t.Fatalf("Invoke returned error: %v", err)
			}
			if !strings.Contains(result.Stdout, "integrity=<unset>") {
				t.Fatalf("runtime observed an integrity request variable without an Engine request: %q", result.Stdout)
			}
			if tc.inherited != "" && strings.Contains(result.Stdout, tc.inherited) {
				t.Fatalf("runtime observed the inherited integrity request value: %q", result.Stdout)
			}
		})
	}
}
