package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"parametron/internal/engine/executor"
	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordpackage"
	"parametron/internal/engine/verification"
)

// Issue #75 permanent end-to-end proof: captured target metadata moves through
// authoring, planning, the FreeCAD runtime manifest, controlled runtime
// observation, Engine verification, and normalized record emission at schema
// 1.0. Semantic metadata identity and native owner/property identity stay
// distinct and correlated.

const (
	metadataPartMutations = `{"metadata":[{"object":"Body01","property":"material","value":"S235JR","valueKind":"string"},{"object":"Chamfer01","property":"mass","value":12.5,"valueKind":"number"},{"object":"Chamfer01","property":"tagged","value":true,"valueKind":"boolean"}],"suppression":[{"object":"Body01","suppressed":true}],"deletion":[{"object":"Chamfer01"}]}`
	metadataAssembly      = `{"metadata":[{"object":"Assembly01","property":"assembly_count","value":2,"valueKind":"integer"}],"visibility":[{"object":"Assembly01","visible":false}]}`
	metadataTargetState   = `{"suppression":[{"destination":"part","object":"Body01"}],"visibility":[{"destination":"assembly","object":"Assembly01"}],"existence":[{"destination":"part","object":"Chamfer01"}]}`
)

func metadataContractTargets(reversed bool) []contractPackageTarget {
	if reversed {
		return []contractPackageTarget{
			{name: "Chamfer", kind: "part", action: "delete"},
			{name: "Assembly", kind: "assembly", action: "hide"},
			{name: "Bracket", kind: "part", action: "suppress"},
		}
	}
	return []contractPackageTarget{
		{name: "Bracket", kind: "part", action: "suppress"},
		{name: "Assembly", kind: "assembly", action: "hide"},
		{name: "Chamfer", kind: "part", action: "delete"},
	}
}

// writeMetadataContractPackageFixture extends the shared controlled-runtime
// fixture with captured metadata whose native owners differ from the semantic
// target names. Bracket owns Body01, Assembly owns Assembly01, and Chamfer owns
// Chamfer01. Only one metadata declaration uses an empty settings block; the
// rest omit the block, proving both forms remain compatible.
func writeMetadataContractPackageFixture(t *testing.T, reversed bool) string {
	t.Helper()

	projectDir := writeContractPackageFixture(t, metadataContractTargets(reversed))

	cadPath := filepath.Join(projectDir, "prm.cad.json")
	data, err := os.ReadFile(cadPath)
	if err != nil {
		t.Fatal(err)
	}
	var capture map[string]any
	if err := json.Unmarshal(data, &capture); err != nil {
		t.Fatal(err)
	}
	entities := capture["entities"].(map[string]any)
	components := entities["components"].([]any)
	nativeRefs := map[string]string{
		"cmp.bracket":  "Body01",
		"cmp.assembly": "Assembly01",
		"cmp.chamfer":  "Chamfer01",
	}
	for _, raw := range components {
		component := raw.(map[string]any)
		id, _ := component["id"].(string)
		nativeRef, ok := nativeRefs[id]
		if !ok {
			continue
		}
		identity, _ := component["identitySource"].(map[string]any)
		if identity == nil {
			t.Fatalf("component %q has no identitySource to receive a native owner", id)
		}
		identity["nativeRef"] = nativeRef
	}
	makeMetadata := func(id, ownerID, componentID, key, displayName, cadType, valueType string) map[string]any {
		return map[string]any{
			"id":             id,
			"ownerKind":      "component",
			"ownerId":        ownerID,
			"componentId":    componentID,
			"key":            key,
			"displayName":    displayName,
			"cadType":        cadType,
			"valueType":      valueType,
			"observable":     true,
			"writable":       true,
			"annotations":    map[string]any{"description": "", "comment": "", "purpose": ""},
			"stabilityClass": "stable",
		}
	}
	entities["metadata"] = []any{
		makeMetadata("meta.bracket.material", "cmp.bracket", "cmp.bracket", "material", "Material", "App::PropertyString", "string"),
		makeMetadata("meta.assembly.assemblyCount", "cmp.assembly", "cmp.assembly", "assembly_count", "Assembly Count", "App::PropertyInteger", "number"),
		makeMetadata("meta.chamfer.mass", "cmp.chamfer", "cmp.chamfer", "mass", "Mass", "App::PropertyFloat", "number"),
		makeMetadata("meta.chamfer.tagged", "cmp.chamfer", "cmp.chamfer", "tagged", "Tagged", "App::PropertyBool", "boolean"),
	}
	data, err = json.Marshal(capture)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cadPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	dslPath := filepath.Join(projectDir, "project.dsl")
	dslBytes, err := os.ReadFile(dslPath)
	if err != nil {
		t.Fatal(err)
	}
	dsl := string(dslBytes)
	chamferMetadata := "        metadata mass = 12.5\n        metadata tagged = true\n"
	if reversed {
		chamferMetadata = "        metadata tagged = true\n        metadata mass = 12.5\n"
	}
	for old, replacement := range map[string]string{
		"    target Bracket: action = suppress": "    target Bracket:\n        action = suppress\n        metadata material = \"S235JR\" { }\n",
		"    target Assembly: action = hide":    "    target Assembly:\n        action = hide\n        metadata assembly_count = 2\n",
		"    target Chamfer: action = delete":   "    target Chamfer:\n        action = delete\n" + chamferMetadata,
	} {
		if !strings.Contains(dsl, old) {
			t.Fatalf("fixture DSL missing %q", old)
		}
		dsl = strings.Replace(dsl, old, replacement, 1)
	}
	if err := os.WriteFile(dslPath, []byte(dsl), 0o644); err != nil {
		t.Fatal(err)
	}
	return projectDir
}

func metadataScriptedTargetState(t *testing.T) *string {
	t.Helper()
	return scripted(t, scriptedTargetState{
		Suppression: []scriptedBooleanEvidence{observedBoolean("part", "Body01", true)},
		Visibility:  []scriptedBooleanEvidence{observedBoolean("assembly", "Assembly01", false)},
		Existence:   []scriptedExistenceEvidence{existenceWithStatus("part", "Chamfer01", "absent")},
	})
}

func metadataTargetStateFacts() map[string]string {
	return map[string]string{
		"part/Body01/suppression":        `{"status":"observed","value":true}`,
		"assembly/Assembly01/visibility": `{"status":"observed","value":false}`,
		"part/Chamfer01/existence":       `{"status":"absent"}`,
	}
}

func encodedObservationRequest(t *testing.T, run contractPackageRun) map[string]any {
	t.Helper()
	inv := run.invocations[0]
	request, _ := decodeJSONFile(t, inv.flag(t, "--observation-request"))
	return request
}

func expectedMetadataByID(t *testing.T, request map[string]any) map[string]map[string]any {
	t.Helper()
	expected, _ := request["expected"].(map[string]any)
	items, _ := expected["metadata"].([]any)
	out := map[string]map[string]any{}
	for _, item := range items {
		entry := item.(map[string]any)
		id, _ := entry["id"].(string)
		if id == "" {
			id = "working_copy_sha256"
		}
		out[id] = entry
	}
	return out
}

func assertMetadataObservationRequest(t *testing.T, run contractPackageRun) {
	t.Helper()
	entries := expectedMetadataByID(t, encodedObservationRequest(t, run))
	workingCopy, ok := entries["working_copy_sha256"]
	if !ok || workingCopy["key"] != "working_copy_sha256" {
		t.Fatalf("working-copy metadata expectation missing: %#v", entries)
	}
	want := map[string]struct {
		key       string
		ownerID   string
		value     any
		valueKind string
	}{
		"meta.bracket.material":       {key: "material", ownerID: "Body01", value: "S235JR", valueKind: "string"},
		"meta.assembly.assemblyCount": {key: "assembly_count", ownerID: "Assembly01", value: float64(2), valueKind: "integer"},
		"meta.chamfer.mass":           {key: "mass", ownerID: "Chamfer01", value: 12.5, valueKind: "number"},
		"meta.chamfer.tagged":         {key: "tagged", ownerID: "Chamfer01", value: true, valueKind: "boolean"},
	}
	for id, wantEntry := range want {
		entry, ok := entries[id]
		if !ok {
			t.Fatalf("metadata expectation %q missing: %#v", id, entries)
		}
		if entry["key"] != wantEntry.key || entry["ownerId"] != wantEntry.ownerID || entry["valueKind"] != wantEntry.valueKind {
			t.Fatalf("metadata expectation %q: %#v", id, entry)
		}
		if entry["value"] != wantEntry.value {
			t.Fatalf("metadata expectation %q value: got %#v want %#v", id, entry["value"], wantEntry.value)
		}
	}
}

func assertMetadataObservedEvidence(t *testing.T, outcome *executor.CADRuntimeOutcome) {
	t.Helper()
	if outcome.Observed == nil {
		t.Fatal("Engine did not parse observed evidence")
	}
	byID := map[string]struct {
		key       string
		ownerID   string
		valueKind string
		raw       string
	}{}
	for _, entry := range outcome.Observed.Observation.Metadata {
		byID[entry.ID] = struct {
			key       string
			ownerID   string
			valueKind string
			raw       string
		}{key: entry.Key, ownerID: entry.OwnerID, valueKind: entry.ValueKind, raw: string(entry.Value.Raw())}
	}
	want := map[string]struct {
		key       string
		ownerID   string
		valueKind string
		raw       string
	}{
		"meta.bracket.material":       {key: "material", ownerID: "Body01", valueKind: "string", raw: `"S235JR"`},
		"meta.assembly.assemblyCount": {key: "assembly_count", ownerID: "Assembly01", valueKind: "integer", raw: "2"},
		"meta.chamfer.mass":           {key: "mass", ownerID: "Chamfer01", valueKind: "number", raw: "12.5"},
		"meta.chamfer.tagged":         {key: "tagged", ownerID: "Chamfer01", valueKind: "boolean", raw: "true"},
	}
	for id, wantEntry := range want {
		got, ok := byID[id]
		if !ok {
			t.Fatalf("observed metadata %q missing: %#v", id, byID)
		}
		if got != wantEntry {
			t.Fatalf("observed metadata %q: got %#v want %#v", id, got, wantEntry)
		}
	}

	rawBytes, err := os.ReadFile(outcome.ObservedPath)
	if err != nil {
		t.Fatalf("read raw observed evidence: %v", err)
	}
	var raw struct {
		Observation struct {
			Metadata []map[string]any `json:"metadata"`
		} `json:"observation"`
	}
	if err := json.Unmarshal(rawBytes, &raw); err != nil {
		t.Fatalf("decode raw observed evidence: %v", err)
	}
	rawByID := map[string]map[string]any{}
	for _, entry := range raw.Observation.Metadata {
		id, _ := entry["id"].(string)
		rawByID[id] = entry
	}
	for id, wantEntry := range want {
		entry, ok := rawByID[id]
		if !ok {
			t.Fatalf("raw observed metadata %q missing: %s", id, rawBytes)
		}
		if entry["ownerId"] != wantEntry.ownerID || entry["key"] != wantEntry.key || entry["valueKind"] != wantEntry.valueKind {
			t.Fatalf("raw observed metadata %q identity: %#v", id, entry)
		}
	}
}

func metadataObservationFacts(record recordcontract.ObservationRecord) map[string]recordcontract.ObservationFact {
	facts := map[string]recordcontract.ObservationFact{}
	for _, fact := range record.Observation.Facts {
		if fact.Kind == recordcontract.ObservationKindMetadata {
			facts[fact.Subject.ID] = fact
		}
	}
	return facts
}

func assertMetadataRecordPackage(t *testing.T, run contractPackageRun) {
	t.Helper()
	packageRoot := recordPackageRoot(run.result.RunRoot)
	files := readRecordPackageFiles(t, packageRoot)
	var record recordcontract.ObservationRecord
	decodePackageRecord(t, files, recordpackage.MustRecordContractPath("observation"), &record)
	if record.Version != recordcontract.CurrentVersion || record.Family != recordcontract.FamilyObservation {
		t.Fatalf("observation record version/family = %q/%q", record.Version, record.Family)
	}
	facts := metadataObservationFacts(record)
	want := map[string]struct {
		ownerID   string
		name      string
		valueKind string
		raw       string
	}{
		"meta.bracket.material":       {ownerID: "Body01", name: "material", valueKind: "string", raw: `"S235JR"`},
		"meta.assembly.assemblyCount": {ownerID: "Assembly01", name: "assembly_count", valueKind: "integer", raw: "2"},
		"meta.chamfer.mass":           {ownerID: "Chamfer01", name: "mass", valueKind: "number", raw: "12.5"},
		"meta.chamfer.tagged":         {ownerID: "Chamfer01", name: "tagged", valueKind: "boolean", raw: "true"},
	}
	for id, wantEntry := range want {
		fact, ok := facts[id]
		if !ok {
			t.Fatalf("normalized metadata fact %q missing: %#v", id, facts)
		}
		if fact.Subject.OwnerID != wantEntry.ownerID || fact.Subject.Name != wantEntry.name || fact.Key != wantEntry.name {
			t.Fatalf("normalized metadata fact %q identity: %#v", id, fact)
		}
		if fact.Value.Kind != wantEntry.valueKind || fact.Value.Raw != wantEntry.raw {
			t.Fatalf("normalized metadata fact %q value: %#v", id, fact.Value)
		}
		if fact.Subject.ID == fact.Subject.OwnerID {
			t.Fatalf("normalized metadata fact %q collapsed semantic and native identity: %#v", id, fact)
		}
	}
}

func assertMetadataVerification(t *testing.T, outcome *executor.CADRuntimeOutcome) {
	t.Helper()
	if outcome.VerificationResult == nil {
		t.Fatal("Engine verification result missing")
	}
	result := outcome.VerificationResult
	if result.Status != verification.StatusPass {
		t.Fatalf("verification status = %q: %+v", result.Status, result)
	}
	for _, category := range []struct {
		name  string
		entry verification.CategoryResult
	}{
		{"metadata", result.Categories.Metadata},
		{"parameters", result.Categories.Parameters},
		{"targetState", result.Categories.TargetState},
	} {
		if !category.entry.Enabled || category.entry.Status != verification.CategoryStatusPass {
			t.Fatalf("%s category = %+v", category.name, category.entry)
		}
	}
	if result.Categories.Metadata.Message != "verified 5 metadata entries" {
		t.Fatalf("metadata category message = %q", result.Categories.Metadata.Message)
	}
}

func TestContractPackage_TargetScopedMetadataEndToEnd(t *testing.T) {
	contractPackageEnv(t)
	dir := writeMetadataContractPackageFixture(t, false)
	run := runContractPackage(t, dir, filepath.Join(t.TempDir(), "out"), "success", metadataScriptedTargetState(t))
	if len(run.invocations) != 1 {
		t.Fatalf("runtime calls=%d, err=%v", len(run.invocations), run.err)
	}
	attempt := assertContractRequest(t, dir, run, run.invocations[0], contractRequestExpectation{
		partMutations:      metadataPartMutations,
		assemblyMutations:  metadataAssembly,
		targetStateRequest: metadataTargetState,
	})
	assertContractSuccessPackage(t, run, attempt, *metadataScriptedTargetState(t), metadataTargetStateFacts())

	outcome := singleCADRuntimeOutcome(t, run.result.Execution)
	assertMetadataObservationRequest(t, run)
	assertMetadataObservedEvidence(t, outcome)
	assertMetadataVerification(t, outcome)
	assertMetadataRecordPackage(t, run)
}

func TestContractPackage_MetadataProjectionIsAuthoringOrderIndependent(t *testing.T) {
	contractPackageEnv(t)
	first := runContractPackage(t, writeMetadataContractPackageFixture(t, false), filepath.Join(t.TempDir(), "out"), "success", metadataScriptedTargetState(t))
	second := runContractPackage(t, writeMetadataContractPackageFixture(t, true), filepath.Join(t.TempDir(), "out"), "success", metadataScriptedTargetState(t))
	if first.err != nil || second.err != nil {
		t.Fatalf("runs failed: %v / %v", first.err, second.err)
	}

	firstPayload, _ := decodeJSONFile(t, filepath.Join(first.result.RunRoot, "products", "Box", "prm.export-manifest.json"))
	secondPayload, _ := decodeJSONFile(t, filepath.Join(second.result.RunRoot, "products", "Box", "prm.export-manifest.json"))
	for _, section := range []string{"partMutations", "assemblyMutations"} {
		if compactJSON(t, firstPayload[section]) != compactJSON(t, secondPayload[section]) {
			t.Fatalf("%s depends on authoring order:\nfirst:  %s\nsecond: %s", section, compactJSON(t, firstPayload[section]), compactJSON(t, secondPayload[section]))
		}
	}
	firstRequest := encodedObservationRequest(t, first)
	secondRequest := encodedObservationRequest(t, second)
	firstExpected := compactJSON(t, firstRequest["expected"].(map[string]any)["metadata"])
	secondExpected := compactJSON(t, secondRequest["expected"].(map[string]any)["metadata"])
	if firstExpected != secondExpected {
		t.Fatalf("expected metadata depends on authoring order:\nfirst:  %s\nsecond: %s", firstExpected, secondExpected)
	}
	firstContext := compactJSON(t, firstRequest["observationContext"].(map[string]any)["targetState"])
	secondContext := compactJSON(t, secondRequest["observationContext"].(map[string]any)["targetState"])
	if firstContext != secondContext {
		t.Fatalf("target-state request depends on authoring order:\nfirst:  %s\nsecond: %s", firstContext, secondContext)
	}
}
