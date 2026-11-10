package cadruntime

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/resourcebinding"
)

func occurrenceFixture(t *testing.T, count int) (OccurrenceMappingInput, OccurrenceRuntimeEvidence) {
	t.Helper()
	inventory, err := resourcebinding.New([]resourcebinding.Binding{
		{ProjectID: "p", Kind: "model", LogicalID: "assembly", SourceCoordinate: "assembly.FCStd", MaterializedCoordinate: "source/assembly.FCStd"},
		{ProjectID: "p", Kind: "model", LogicalID: "component", SourceCoordinate: "part.FCStd", MaterializedCoordinate: "resources/part.FCStd"},
		{ProjectID: "p", Kind: "model", LogicalID: "alias", SourceCoordinate: "part.FCStd", MaterializedCoordinate: "resources/alias.FCStd"},
	})
	if err != nil {
		t.Fatal(err)
	}
	parent := recordcontract.OccurrenceEntity{Resource: recordcontract.ReferenceResource{ProjectID: "p", Kind: "model", LogicalID: "assembly"}, NativeKind: "object", Name: "Assembly"}
	request := &recordcontract.OccurrenceRequest{SchemaVersion: "1.0", Requirement: "optional", Scope: recordcontract.OccurrenceScope{Parent: parent, Context: []recordcontract.OccurrenceContextFrame{}, Mechanism: recordcontract.OccurrenceDirectMemberMechanism}}
	evidence := OccurrenceRuntimeEvidence{SchemaVersion: "1.1", Kind: "native_occurrence_evidence", FreeCADVersion: "1.1.4", Scope: request.Scope, Parent: &OccurrenceNativeEntity{Entity: parent, Document: "source/assembly.FCStd", NativeType: "Assembly::AssemblyObject"}, Completeness: "complete", InspectionCompleted: true, Group: []string{}, Members: []OccurrenceRuntimeMember{}, Diagnostics: []string{}}
	for n := 0; n < count; n++ {
		zero, valid := 0, true
		name := fmt.Sprintf("Link%d", n)
		target := parent
		target.Resource.LogicalID = "component"
		target.Name = "Body"
		evidence.Group = append(evidence.Group, name)
		evidence.Members = append(evidence.Members, OccurrenceRuntimeMember{Name: name, Parent: parent, NativeType: "App::Link", ElementCount: &zero, MembershipCorroborated: true, Classification: "occurrence", Resolution: "resolved", NativeStateValid: &valid, Target: &OccurrenceNativeEntity{Entity: target, Document: "resources/part.FCStd", NativeType: "PartDesign::Body"}})
	}
	return OccurrenceMappingInput{Request: request, Resources: inventory, ExpectedSourceDocument: "source/assembly.FCStd"}, evidence
}

func occurrenceBytes(t *testing.T, value any) []byte {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func mapOccurrence(t *testing.T, in OccurrenceMappingInput, e OccurrenceRuntimeEvidence) *recordcontract.OccurrenceSummary {
	t.Helper()
	in.Content = occurrenceBytes(t, e)
	s, err := MapOccurrenceEvidence(in)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestOccurrenceSupportedTargetsAndDistinctQuantity(t *testing.T) {
	for _, typ := range []string{"PartDesign::Body", "Part::Feature", "App::Part"} {
		for count := 0; count <= 3; count++ {
			t.Run(fmt.Sprintf("%s/%d", typ, count), func(t *testing.T) {
				in, e := occurrenceFixture(t, count)
				for i := range e.Members {
					e.Members[i].Target.NativeType = typ
				}
				s := mapOccurrence(t, in, e)
				if s.SchemaVersion != "1.0" || s.Completeness != "complete" || len(s.Occurrences) != count || s.Quantities == nil {
					t.Fatalf("summary: %+v", s)
				}
				if count == 0 {
					if len(s.Quantities) != 0 {
						t.Fatal(s.Quantities)
					}
					return
				}
				if len(s.Quantities) != 1 || s.Quantities[0].Quantity != count || s.Quantities[0].Target != e.Members[0].Target.Entity {
					t.Fatal(s.Quantities)
				}
				for i, o := range s.Occurrences {
					if o.Target != e.Members[i].Target.Entity || o.Member != e.Members[i].Name {
						t.Fatal("native target or member substituted")
					}
				}
			})
		}
	}
}

func TestOccurrenceRejectsUnprovenNativeEvidence(t *testing.T) {
	cases := map[string]func(*OccurrenceRuntimeEvidence){
		"outside group":              func(e *OccurrenceRuntimeEvidence) { e.Group = []string{"Other"} },
		"owner type":                 func(e *OccurrenceRuntimeEvidence) { e.Parent.NativeType = "App::Part" },
		"parent mismatch":            func(e *OccurrenceRuntimeEvidence) { e.Members[0].Parent.Name = "Other" },
		"uncorroborated":             func(e *OccurrenceRuntimeEvidence) { e.Members[0].MembershipCorroborated = false },
		"generic property":           func(e *OccurrenceRuntimeEvidence) { e.Members[0].NativeType = "App::PropertyXLink" },
		"array":                      func(e *OccurrenceRuntimeEvidence) { *e.Members[0].ElementCount = 2 },
		"link element":               func(e *OccurrenceRuntimeEvidence) { e.Members[0].NativeType = "App::LinkElement" },
		"unresolved":                 func(e *OccurrenceRuntimeEvidence) { e.Members[0].Resolution = "unresolved" },
		"invalid state":              func(e *OccurrenceRuntimeEvidence) { *e.Members[0].NativeStateValid = false },
		"unknown target":             func(e *OccurrenceRuntimeEvidence) { e.Members[0].Target.NativeType = "App::FeaturePython" },
		"assembly target":            func(e *OccurrenceRuntimeEvidence) { e.Members[0].Target.NativeType = "Assembly::AssemblyObject" },
		"missing target":             func(e *OccurrenceRuntimeEvidence) { e.Members[0].Target = nil },
		"missing count":              func(e *OccurrenceRuntimeEvidence) { e.Members[0].ElementCount = nil },
		"missing state":              func(e *OccurrenceRuntimeEvidence) { e.Members[0].NativeStateValid = nil },
		"version 1.1.1":              func(e *OccurrenceRuntimeEvidence) { e.FreeCADVersion = "1.1.1" },
		"unsupported mechanism":      func(e *OccurrenceRuntimeEvidence) { e.Scope.Mechanism = "recursive" },
		"coverage gap":               func(e *OccurrenceRuntimeEvidence) { e.Group = append(e.Group, "Unknown") },
		"inspection incomplete":      func(e *OccurrenceRuntimeEvidence) { e.InspectionCompleted = false },
		"complete diagnostic":        func(e *OccurrenceRuntimeEvidence) { e.Diagnostics = []string{"unhandled"} },
		"missing root":               func(e *OccurrenceRuntimeEvidence) { e.Parent = nil },
		"undeclared target":          func(e *OccurrenceRuntimeEvidence) { e.Members[0].Target.Entity.Resource.LogicalID = "unknown" },
		"absolute target coordinate": func(e *OccurrenceRuntimeEvidence) { e.Members[0].Target.Document = "/tmp/part.FCStd" },
		"traversing coordinate":      func(e *OccurrenceRuntimeEvidence) { e.Members[0].Target.Document = "../part.FCStd" },
		"wrong resource join":        func(e *OccurrenceRuntimeEvidence) { e.Members[0].Target.Document = "resources/alias.FCStd" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			in, e := occurrenceFixture(t, 1)
			mutate(&e)
			in.Content = occurrenceBytes(t, e)
			if _, err := MapOccurrenceEvidence(in); err == nil {
				t.Fatal("accepted unproven evidence")
			}
		})
	}
}

func TestOccurrenceAuxiliaryRequiresVerifiedEmptyContents(t *testing.T) {
	for count := 0; count <= 2; count++ {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			in, e := occurrenceFixture(t, count)
			valid := true
			joint := OccurrenceRuntimeMember{Name: "Joints", Parent: e.Scope.Parent, NativeType: "Assembly::JointGroup", MembershipCorroborated: true, Classification: "auxiliary", Resolution: "resolved", NativeStateValid: &valid, Contents: &OccurrenceRuntimeMemberContents{InspectionCompleted: true, Group: []string{}}}
			e.Group = append(e.Group, joint.Name)
			e.Members = append(e.Members, joint)
			s := mapOccurrence(t, in, e)
			if len(s.Occurrences) != count || s.Quantities == nil {
				t.Fatal(s)
			}
		})
	}
	cases := map[string]func(*OccurrenceRuntimeMember){
		"missing contents":       func(m *OccurrenceRuntimeMember) { m.Contents = nil },
		"unfinished inspection":  func(m *OccurrenceRuntimeMember) { m.Contents.InspectionCompleted = false },
		"missing inventory":      func(m *OccurrenceRuntimeMember) { m.Contents.Group = nil },
		"nonempty":               func(m *OccurrenceRuntimeMember) { m.Contents.Group = []string{"Joint"} },
		"unknown type":           func(m *OccurrenceRuntimeMember) { m.NativeType = "App::FeaturePython" },
		"unknown classification": func(m *OccurrenceRuntimeMember) { m.Classification = "ignore" },
		"incorrect parent":       func(m *OccurrenceRuntimeMember) { m.Parent.Name = "Other" },
		"missing ownership":      func(m *OccurrenceRuntimeMember) { m.MembershipCorroborated = false },
		"invalid state":          func(m *OccurrenceRuntimeMember) { *m.NativeStateValid = false },
		"link count":             func(m *OccurrenceRuntimeMember) { zero := 0; m.ElementCount = &zero },
		"resolved target": func(m *OccurrenceRuntimeMember) {
			m.Target = &OccurrenceNativeEntity{Entity: m.Parent, Document: "source/assembly.FCStd", NativeType: "App::Part"}
		},
		"diagnostic": func(m *OccurrenceRuntimeMember) { m.Diagnostic = "failed inspection" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			in, e := occurrenceFixture(t, 0)
			valid := true
			m := OccurrenceRuntimeMember{Name: "Joints", Parent: e.Scope.Parent, NativeType: "Assembly::JointGroup", MembershipCorroborated: true, Classification: "auxiliary", Resolution: "resolved", NativeStateValid: &valid, Contents: &OccurrenceRuntimeMemberContents{InspectionCompleted: true, Group: []string{}}}
			mutate(&m)
			e.Group = []string{m.Name}
			e.Members = []OccurrenceRuntimeMember{m}
			in.Content = occurrenceBytes(t, e)
			if _, err := MapOccurrenceEvidence(in); !errors.Is(err, recordcontract.ErrInvalidOccurrence) {
				t.Fatalf("error: %v", err)
			}
		})
	}
}

func TestOccurrenceAvailabilityAndRequiredCompleteness(t *testing.T) {
	for _, state := range []string{"complete", "incomplete", "unsupported", "unavailable", "omitted"} {
		for _, required := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/required=%t", state, required), func(t *testing.T) {
				in, e := occurrenceFixture(t, 0)
				if required {
					in.Request.Requirement = "complete"
				}
				e.Completeness = state
				if state != "complete" {
					e.Diagnostics = []string{"not available"}
					e.InspectionCompleted = false
				}
				if state == "unavailable" {
					e.Parent = nil
				}
				if state != "omitted" {
					in.Content = occurrenceBytes(t, e)
				}
				s, err := MapOccurrenceEvidence(in)
				if required && state != "complete" {
					if !errors.Is(err, recordcontract.ErrOccurrenceCompletenessRequired) {
						t.Fatalf("required sentinel: %v", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				want := state
				if state == "omitted" {
					want = "unavailable"
				}
				if s.Completeness != want || (s.Quantities != nil) != (state == "complete") {
					t.Fatal(s)
				}
			})
		}
	}
}

func TestOccurrenceDuplicateConflictAndSemanticDeterminism(t *testing.T) {
	in, e := occurrenceFixture(t, 3)
	e.Members[0].Target.Entity.SemanticID = "semantic-a"
	e.Members[1].Target.Entity.SemanticID = "semantic-b"
	first := mapOccurrence(t, in, e)
	raw := occurrenceBytes(t, e)
	e.Members = append(e.Members, e.Members[0])
	slices.Reverse(e.Members)
	slices.Reverse(e.Group)
	bindings := in.Resources.Bindings()
	slices.Reverse(bindings)
	var err error
	in.Resources, err = resourcebinding.New(bindings)
	if err != nil {
		t.Fatal(err)
	}
	second := mapOccurrence(t, in, e)
	if !reflect.DeepEqual(first.Occurrences, second.Occurrences) || !reflect.DeepEqual(first.Quantities, second.Quantities) || first.Quantities[0].Quantity != 3 {
		t.Fatal("order or duplicates changed semantic output")
	}
	if first.Evidence.DigestSHA256 != fmt.Sprintf("%x", sha256.Sum256(raw)) || first.Evidence.DigestSHA256 == second.Evidence.DigestSHA256 {
		t.Fatal("raw provenance canonicalized")
	}
	for name, change := range map[string]func(*OccurrenceRuntimeMember){
		"target": func(m *OccurrenceRuntimeMember) {
			target := *m.Target
			target.Entity.Name = "Other"
			m.Target = &target
		},
		"parent":      func(m *OccurrenceRuntimeMember) { m.Parent.Name = "Other" },
		"native type": func(m *OccurrenceRuntimeMember) { m.NativeType = "App::LinkElement" },
		"state":       func(m *OccurrenceRuntimeMember) { v := false; m.NativeStateValid = &v },
		"membership":  func(m *OccurrenceRuntimeMember) { m.MembershipCorroborated = false },
		"semantic annotation": func(m *OccurrenceRuntimeMember) {
			target := *m.Target
			target.Entity.SemanticID = "conflict"
			m.Target = &target
		},
	} {
		t.Run(name, func(t *testing.T) {
			for _, reverse := range []bool{false, true} {
				in, e := occurrenceFixture(t, 1)
				conflict := e.Members[0]
				change(&conflict)
				e.Members = append(e.Members, conflict)
				if reverse {
					slices.Reverse(e.Members)
				}
				in.Content = occurrenceBytes(t, e)
				if _, err := MapOccurrenceEvidence(in); !errors.Is(err, recordcontract.ErrInvalidOccurrence) {
					t.Fatalf("conflict accepted: %v", err)
				}
			}
		})
	}
}

func TestOccurrenceQuantityResourceAndEntitySeparation(t *testing.T) {
	in, e := occurrenceFixture(t, 3)
	e.Members[1].Target.Entity.Name = "Other"
	e.Members[2].Target.Entity.Resource.LogicalID = "alias"
	e.Members[2].Target.Document = "resources/alias.FCStd"
	s := mapOccurrence(t, in, e)
	if len(s.Quantities) != 3 {
		t.Fatal(s.Quantities)
	}
	for _, q := range s.Quantities {
		if q.Quantity != 1 {
			t.Fatal(q)
		}
	}
}

func TestOccurrenceStrictJSONAndVersionIsolation(t *testing.T) {
	for _, version := range []string{"1.0", "1.1"} {
		t.Run(version, func(t *testing.T) {
			in, e := occurrenceFixture(t, 1)
			e.SchemaVersion = version
			base := string(occurrenceBytes(t, e))
			in.Content = []byte(base)
			if _, err := MapOccurrenceEvidence(in); err != nil {
				t.Fatal(err)
			}
			for name, raw := range map[string]string{
				"unsupported version": strings.Replace(base, `"schemaVersion":"`+version+`"`, `"schemaVersion":"9.0"`, 1),
				"unknown field":       strings.Replace(base, `"kind":`, `"unexpected":true,"kind":`, 1),
				"duplicate":           strings.Replace(base, `"kind":`, `"kind":"native_occurrence_evidence","kind":`, 1),
				"case duplicate":      strings.Replace(base, `"kind":`, `"Kind":"native_occurrence_evidence","kind":`, 1),
				"trailing":            base + ` {}`,
				"wrong type":          strings.Replace(base, `"inspectionCompleted":true`, `"inspectionCompleted":"true"`, 1),
				"null inventory":      strings.Replace(base, `"group":["Link0"]`, `"group":null`, 1),
				"missing version":     strings.Replace(base, `"schemaVersion":"`+version+`",`, "", 1),
				"null target":         strings.Replace(base, `"nativeStateValid":true`, `"nativeStateValid":null`, 1),
			} {
				t.Run(name, func(t *testing.T) {
					in.Content = []byte(raw)
					if _, err := MapOccurrenceEvidence(in); !errors.Is(err, recordcontract.ErrInvalidOccurrence) {
						t.Fatalf("accepted invalid JSON: %v", err)
					}
				})
			}
			if version == "1.0" {
				for _, contents := range []string{`null`, `{"inspectionCompleted":true,"group":[]}`} {
					for _, field := range []string{"contents", "Contents"} {
						raw := strings.Replace(base, `"classification":`, fmt.Sprintf(`%q:%s,"classification":`, field, contents), 1)
						if _, err := DecodeOccurrenceEvidence([]byte(raw)); !errors.Is(err, recordcontract.ErrInvalidOccurrence) {
							t.Fatalf("legacy shape relaxed: %v", err)
						}
					}
				}
			}
		})
	}
	in, _ := occurrenceFixture(t, 0)
	base := string(occurrenceBytes(t, in.Request))
	if _, err := DecodeOccurrenceRequest([]byte(base)); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{base + `null`, strings.Replace(base, `"schemaVersion":"1.0"`, `"schemaVersion":"1.1"`, 1), strings.Replace(base, `"requirement":"optional"`, `"requirement":null`, 1), strings.Replace(base, `"context":[]`, `"context":null`, 1), strings.Replace(base, `"requirement":`, `"Requirement":"complete","requirement":`, 1), strings.Replace(base, `"requirement":`, `"unknown":true,"requirement":`, 1)} {
		if _, err := DecodeOccurrenceRequest([]byte(raw)); !errors.Is(err, recordcontract.ErrInvalidOccurrence) {
			t.Fatalf("invalid request accepted: %s: %v", raw, err)
		}
	}
}

func TestOccurrenceAuxiliaryJSONRequiresExplicitInspectionInventory(t *testing.T) {
	in, e := occurrenceFixture(t, 0)
	valid := true
	e.Group = []string{"Joints"}
	e.Members = []OccurrenceRuntimeMember{{Name: "Joints", Parent: e.Scope.Parent, NativeType: "Assembly::JointGroup", MembershipCorroborated: true, Classification: "auxiliary", Resolution: "resolved", NativeStateValid: &valid, Contents: &OccurrenceRuntimeMemberContents{InspectionCompleted: true, Group: []string{}}}}
	base := string(occurrenceBytes(t, e))
	for _, contents := range []string{`null`, `{}`, `{"group":[]}`, `{"inspectionCompleted":false,"group":[]}`, `{"inspectionCompleted":null,"group":[]}`, `{"inspectionCompleted":true}`, `{"inspectionCompleted":true,"group":null}`, `{"inspectionCompleted":true,"group":["Joint"]}`, `{"inspectionCompleted":true,"group":{}}`, `{"inspectionCompleted":true,"group":"empty"}`} {
		t.Run(contents, func(t *testing.T) {
			in.Content = []byte(strings.Replace(base, `{"inspectionCompleted":true,"group":[]}`, contents, 1))
			if _, err := MapOccurrenceEvidence(in); !errors.Is(err, recordcontract.ErrInvalidOccurrence) {
				t.Fatalf("unproven auxiliary inventory accepted: %v", err)
			}
		})
	}
	e.Group = append(e.Group, "Unknown")
	in.Content = occurrenceBytes(t, e)
	if _, err := MapOccurrenceEvidence(in); !errors.Is(err, recordcontract.ErrInvalidOccurrence) {
		t.Fatalf("auxiliary concealed unclassified member: %v", err)
	}
}

func TestOccurrenceNoncompleteMembersCannotProduceQuantity(t *testing.T) {
	for _, state := range []string{"incomplete", "unsupported"} {
		t.Run(state, func(t *testing.T) {
			in, e := occurrenceFixture(t, 2)
			m := &e.Members[1]
			m.Target = nil
			m.Diagnostic = "unproven member"
			m.Resolution = "unresolved"
			m.Classification = "unresolved"
			if state == "unsupported" {
				m.Classification = "unsupported"
				m.Resolution = "unsupported"
				m.NativeType = "App::LinkElement"
			}
			e.Completeness = state
			e.Diagnostics = []string{"inventory not supported completely"}
			s := mapOccurrence(t, in, e)
			if len(s.Occurrences) != 1 || s.Quantities != nil || s.Completeness != state {
				t.Fatal("noncomplete scope fabricated rollup")
			}
			e.Completeness = "complete"
			e.Diagnostics = []string{}
			in.Content = occurrenceBytes(t, e)
			if _, err := MapOccurrenceEvidence(in); !errors.Is(err, recordcontract.ErrInvalidOccurrence) {
				t.Fatalf("unhandled member became complete: %v", err)
			}
		})
	}
}

func TestOccurrenceScopeContextCannotAuthorizeRecursiveQuantity(t *testing.T) {
	in, e := occurrenceFixture(t, 0)
	outer := e.Scope.Parent
	outer.Name = "OuterAssembly"
	in.Request.Scope.Context = []recordcontract.OccurrenceContextFrame{{Parent: outer, Member: "Instance"}}
	e.Scope = in.Request.Scope
	e.Completeness = "unsupported"
	e.InspectionCompleted = false
	e.Diagnostics = []string{"recursive expansion unproven"}
	s := mapOccurrence(t, in, e)
	if s.Completeness != "unsupported" || s.Quantities != nil {
		t.Fatal(s)
	}
	e.Completeness = "complete"
	e.InspectionCompleted = true
	e.Diagnostics = []string{}
	in.Content = occurrenceBytes(t, e)
	if _, err := MapOccurrenceEvidence(in); !errors.Is(err, recordcontract.ErrInvalidOccurrence) {
		t.Fatalf("context authorized recursive rollup: %v", err)
	}
}

func TestOccurrenceRequestRequiredFieldsAndTypes(t *testing.T) {
	in, _ := occurrenceFixture(t, 0)
	for _, field := range []string{"schemaVersion", "requirement", "scope"} {
		for _, mode := range []string{"missing", "null", "wrong type"} {
			t.Run(field+"/"+mode, func(t *testing.T) {
				var raw map[string]any
				if err := json.Unmarshal(occurrenceBytes(t, in.Request), &raw); err != nil {
					t.Fatal(err)
				}
				switch mode {
				case "missing":
					delete(raw, field)
				case "null":
					raw[field] = nil
				case "wrong type":
					raw[field] = []any{true}
				}
				if _, err := DecodeOccurrenceRequest(occurrenceBytes(t, raw)); !errors.Is(err, recordcontract.ErrInvalidOccurrence) {
					t.Fatalf("invalid request accepted: %v", err)
				}
			})
		}
	}
	for _, field := range []string{"parent", "context", "mechanism"} {
		t.Run("scope/"+field, func(t *testing.T) {
			var raw map[string]any
			if err := json.Unmarshal(occurrenceBytes(t, in.Request), &raw); err != nil {
				t.Fatal(err)
			}
			delete(raw["scope"].(map[string]any), field)
			if _, err := DecodeOccurrenceRequest(occurrenceBytes(t, raw)); !errors.Is(err, recordcontract.ErrInvalidOccurrence) {
				t.Fatalf("incomplete scope accepted: %v", err)
			}
		})
	}
}
