package projectinput

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"parametron/internal/engine/resourcebinding"
)

func sha256Hex(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func TestAttachSourceIdentitiesCoversEveryDeclaredResource(t *testing.T) {
	root := writeDeclaredProject(t)
	resolved, err := ResolveProject(root)
	if err != nil {
		t.Fatal(err)
	}
	captured, err := CaptureResources(resolved)
	if err != nil {
		t.Fatal(err)
	}

	inventory, err := AttachSourceIdentities(resolved, captured)
	if err != nil {
		t.Fatalf("AttachSourceIdentities returned error: %v", err)
	}
	bindings := inventory.Bindings()
	if len(bindings) != 4 {
		t.Fatalf("attached inventory has %d resources, want all 4 declared resources", len(bindings))
	}

	wantDigests := map[string]string{
		"alias":      sha256Hex("primary bytes"),
		"primary":    sha256Hex("primary bytes"),
		"standalone": sha256Hex("standalone bytes"),
		"unused":     sha256Hex("unused bytes"),
	}
	wantCoordinates := map[string]string{
		"alias":      "models/part.FCStd",
		"primary":    "models/part.FCStd",
		"standalone": "standalone.FCStd",
		"unused":     "other/part.FCStd",
	}
	for _, binding := range bindings {
		if got, want := binding.SourceDigest, wantDigests[binding.LogicalID]; got != want {
			t.Fatalf("resource %q source digest = %q, want %q", binding.LogicalID, got, want)
		}
		if got, want := binding.SourceCoordinate, wantCoordinates[binding.LogicalID]; got != want {
			t.Fatalf("resource %q source coordinate changed: got %q, want %q", binding.LogicalID, got, want)
		}
		if binding.MaterializedCoordinate != "" {
			t.Fatalf("resource %q gained a materialized coordinate before execution", binding.LogicalID)
		}
		if err := resourcebinding.ValidateCoordinate(binding.SourceCoordinate); err != nil {
			t.Fatalf("resource %q has an invalid source coordinate: %v", binding.LogicalID, err)
		}
	}
	// The unused and standalone declarations have no product or reference edge.
	for _, logicalID := range []string{"unused", "standalone"} {
		bound, err := inventory.Lookup(resolved.ProjectID, ResourceKindModel, logicalID)
		if err != nil {
			t.Fatalf("zero-edge resource %q is not addressable: %v", logicalID, err)
		}
		if bound.SourceDigest != wantDigests[logicalID] {
			t.Fatalf("zero-edge resource %q lost its source identity: %#v", logicalID, bound)
		}
	}
}

func TestAttachSourceIdentitiesIsRelocationInvariant(t *testing.T) {
	first, err := ResolveProject(writeDeclaredProject(t))
	if err != nil {
		t.Fatal(err)
	}
	second, err := ResolveProject(writeDeclaredProject(t))
	if err != nil {
		t.Fatal(err)
	}

	firstInventory, err := AttachSourceIdentities(first, mustCapture(t, first))
	if err != nil {
		t.Fatal(err)
	}
	secondInventory, err := AttachSourceIdentities(second, mustCapture(t, second))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(firstInventory.Bindings(), secondInventory.Bindings()) {
		t.Fatalf("equivalent bytes at different roots changed declared resource identity:\nfirst:  %#v\nsecond: %#v",
			firstInventory.Bindings(), secondInventory.Bindings())
	}
}

func TestAttachSourceIdentitiesValidatesBidirectionalCoverage(t *testing.T) {
	resolved, err := ResolveProject(writeDeclaredProject(t))
	if err != nil {
		t.Fatal(err)
	}
	captured := mustCapture(t, resolved)

	t.Run("missing captured source identity", func(t *testing.T) {
		partial := *captured
		partial.Models = captured.Models[:len(captured.Models)-1]
		_, err := AttachSourceIdentities(resolved, &partial)
		if err == nil || !strings.Contains(err.Error(), "no captured source identity") {
			t.Fatalf("AttachSourceIdentities = %v, want a missing captured identity error", err)
		}
	})

	t.Run("captured identity is not declared", func(t *testing.T) {
		enriched := *captured
		enriched.Models = append(append([]CapturedResource(nil), captured.Models...), CapturedResource{
			LogicalID: "undeclared", Signature: sha256Hex("undeclared"),
		})
		_, err := AttachSourceIdentities(resolved, &enriched)
		if err == nil || !strings.Contains(err.Error(), "not a declared resource") {
			t.Fatalf("AttachSourceIdentities = %v, want an undeclared resource error", err)
		}
	})

	t.Run("duplicate captured identity", func(t *testing.T) {
		enriched := *captured
		enriched.Models = append(append([]CapturedResource(nil), captured.Models...), captured.Models[0])
		_, err := AttachSourceIdentities(resolved, &enriched)
		if err == nil || !strings.Contains(err.Error(), "duplicate logical ID") {
			t.Fatalf("AttachSourceIdentities = %v, want a duplicate logical ID error", err)
		}
	})

	t.Run("captured identity without logical ID", func(t *testing.T) {
		enriched := *captured
		enriched.Models = append(append([]CapturedResource(nil), captured.Models...), CapturedResource{Signature: sha256Hex("x")})
		_, err := AttachSourceIdentities(resolved, &enriched)
		if err == nil || !strings.Contains(err.Error(), "requires a logical ID") {
			t.Fatalf("AttachSourceIdentities = %v, want a logical ID error", err)
		}
	})
}

func TestAttachSourceIdentitiesWithoutCapturedResourcesFailsClosed(t *testing.T) {
	resolved, err := ResolveProject(writeDeclaredProject(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AttachSourceIdentities(resolved, nil); err == nil {
		t.Fatal("AttachSourceIdentities accepted a declared inventory without captured identities")
	}
}

func TestAttachSourceIdentitiesWithoutProjectIsEmpty(t *testing.T) {
	inventory, err := AttachSourceIdentities(nil, nil)
	if err != nil {
		t.Fatalf("AttachSourceIdentities(nil, nil) returned error: %v", err)
	}
	if len(inventory.Bindings()) != 0 {
		t.Fatalf("AttachSourceIdentities(nil, nil) = %#v, want an empty inventory", inventory)
	}
}

func mustCapture(t *testing.T, resolved *Resolved) *CapturedResources {
	t.Helper()
	captured, err := CaptureResources(resolved)
	if err != nil {
		t.Fatal(err)
	}
	return captured
}

func TestAttachSourceIdentitiesDoesNotMutateCapturedResources(t *testing.T) {
	resolved, err := ResolveProject(writeDeclaredProject(t))
	if err != nil {
		t.Fatal(err)
	}
	captured := mustCapture(t, resolved)
	before := CloneCapturedResources(captured)
	if _, err := AttachSourceIdentities(resolved, captured); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, captured) {
		t.Fatalf("AttachSourceIdentities mutated captured resources:\nbefore: %#v\nafter:  %#v", before, captured)
	}
	path := filepath.Join(resolved.ProjectRoot, "models", "part.FCStd")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("source bytes moved during capture: %v", err)
	}
}
