package verificationtest

import (
	"strconv"
	"strings"

	"parametron/internal/engine/observed"
	"parametron/internal/engine/verification"
)

type Fixture struct {
	Contract *verification.Contract
	Observed *observed.Observed
}

func PassFixture() Fixture {
	contract := validContract()
	return Fixture{
		Contract: contract,
		Observed: MatchingObserved(contract),
	}
}

func MetadataMismatchFixture() Fixture {
	fixture := PassFixture()
	fixture.Observed = MetadataMismatchObserved(fixture.Contract)
	return fixture
}

func ReferenceMismatchFixture() Fixture {
	fixture := PassFixture()
	fixture.Observed = ReferenceMismatchObserved(fixture.Contract)
	return fixture
}

func MissingRequiredMetadataFixture() Fixture {
	fixture := PassFixture()
	fixture.Observed = MissingRequiredMetadataObserved(fixture.Contract)
	return fixture
}

func MissingRequiredReferenceFixture() Fixture {
	fixture := PassFixture()
	fixture.Observed = MissingRequiredReferenceObserved(fixture.Contract)
	return fixture
}

func validContract() *verification.Contract {
	return &verification.Contract{
		SchemaVersion: verification.SchemaVersion,
		Observe: verification.Observe{
			Components: false,
			Parameters: false,
			Metadata:   true,
			References: true,
		},
		ObservationContext: verification.ObservationContext{
			Parameters: []verification.ObservedParameterBinding{},
		},
		Expected: verification.Expected{
			Components: []verification.ExpectedComponent{},
			Parameters: []verification.ExpectedParameter{
				{ID: "par.root.height", Name: "height", Type: "number", Unit: "mm", Value: 5},
				{ID: "par.root.length", Name: "length", Type: "number", Unit: "mm", Value: 35},
			},
			Metadata: []verification.ExpectedMetadata{
				{Key: "working_copy_sha256", Value: strings.Repeat("a", 64)},
			},
			References: []verification.ExpectedReference{
				{Kind: "working_copy_path", Name: "/tmp/out/_working/widget.FCStd"},
			},
		},
		Checks: verification.Checks{
			Components: verification.Check{Enabled: false},
			Parameters: verification.Check{Enabled: false},
			Metadata:   verification.Check{Enabled: true},
			References: verification.Check{Enabled: true},
		},
	}
}

func ParameterEnabledContract() *verification.Contract {
	contract := validContract()
	contract.Expected.Components = []verification.ExpectedComponent{}
	contract.Observe.Parameters = true
	contract.Checks.Parameters.Enabled = true
	contract.ObservationContext.Parameters = []verification.ObservedParameterBinding{
		{ID: "par.root.height", Name: "Height", GroupName: "Dimensions"},
		{ID: "par.root.length", Name: "Length", GroupName: "Dimensions"},
	}
	return contract
}

func MatchingObserved(contract *verification.Contract) *observed.Observed {
	obs := &observed.Observed{
		SchemaVersion: observed.SchemaVersion,
		WorkingCopy: observed.WorkingCopy{
			Path:   contract.Expected.References[0].Name,
			SHA256: contract.Expected.Metadata[0].Value,
		},
		Observation: observed.Observation{
			Parameters: []observed.Parameter{},
			Metadata: []observed.Metadata{{
				ID:        "working-copy:sha256",
				Key:       contract.Expected.Metadata[0].Key,
				Value:     mustObservedMetadataValue(contract.Expected.Metadata[0].Value),
				ValueKind: "string",
			}},
			References: []observed.Reference{{
				Kind: contract.Expected.References[0].Kind,
				Name: contract.Expected.References[0].Name,
			}},
			Components: []observed.Component{},
		},
	}
	if contract.Checks.Parameters.Enabled {
		obs.Observation.Parameters = matchingObservedParameters(contract)
	}
	return obs
}

func matchingObservedParameters(contract *verification.Contract) []observed.Parameter {
	byID := make(map[string]verification.ObservedParameterBinding, len(contract.ObservationContext.Parameters))
	for _, binding := range contract.ObservationContext.Parameters {
		byID[binding.ID] = binding
	}

	parameters := make([]observed.Parameter, 0, len(contract.Expected.Parameters))
	for _, parameter := range contract.Expected.Parameters {
		name := parameter.Name
		groupID := ""
		if binding, ok := byID[parameter.ID]; ok {
			if binding.Name != "" {
				name = binding.Name
			}
			groupID = binding.GroupName
		}
		parameters = append(parameters, observed.Parameter{
			ID:        parameter.ID,
			Name:      name,
			GroupID:   groupID,
			Value:     mustObservedValue(strconv.FormatFloat(parameter.Value, 'f', -1, 64)),
			ValueKind: "number",
		})
	}
	return parameters
}

func MetadataMismatchObserved(contract *verification.Contract) *observed.Observed {
	obs := MatchingObserved(contract)
	obs.Observation.Metadata[0].Value = mustObservedMetadataValue(strings.Repeat("0", 64))
	obs.WorkingCopy.SHA256 = contract.Expected.Metadata[0].Value
	return obs
}

func ReferenceMismatchObserved(contract *verification.Contract) *observed.Observed {
	obs := MatchingObserved(contract)
	obs.Observation.References[0].Name = "/tmp/out/_working/other-widget.FCStd"
	obs.WorkingCopy.Path = contract.Expected.References[0].Name
	return obs
}

func MissingRequiredMetadataObserved(contract *verification.Contract) *observed.Observed {
	obs := MatchingObserved(contract)
	obs.Observation.Metadata = []observed.Metadata{}
	return obs
}

func MissingRequiredReferenceObserved(contract *verification.Contract) *observed.Observed {
	obs := MatchingObserved(contract)
	obs.Observation.References = []observed.Reference{}
	return obs
}

func mustObservedMetadataValue(value string) observed.Value {
	raw, err := observed.NewValue([]byte(`"` + value + `"`))
	if err != nil {
		panic(err)
	}
	return raw
}

func mustObservedValue(value string) observed.Value {
	raw, err := observed.NewValue([]byte(value))
	if err != nil {
		panic(err)
	}
	return raw
}
