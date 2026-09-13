package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"parametron/internal/authoring/dsl"
)

type sweepCaseSummary struct {
	Index    int            `json:"index"`
	Inputs   map[string]any `json:"inputs"`
	Status   string         `json:"status"`
	PlanHash string         `json:"planHash,omitempty"`
	Error    string         `json:"error,omitempty"`
}

type sweepReport struct {
	SchemaVersion          string             `json:"schemaVersion"`
	Product                string             `json:"product"`
	Params                 []string           `json:"params"`
	TotalCases             int                `json:"totalCases"`
	PassedCases            int                `json:"passedCases"`
	FailedCases            int                `json:"failedCases"`
	FailureCountsByMessage map[string]int     `json:"failureCountsByMessage,omitempty"`
	Cases                  []sweepCaseSummary `json:"cases"`
}

type sweepParamSpec struct {
	Name    string
	Raw     string
	Values  []string
	Display []any
}

func newSweepCmd() *cobra.Command {
	var productName string
	var rawParams []string

	cmd := &cobra.Command{
		Use:   "sweep",
		Short: "Generate a Cartesian product of parameter values and validate planning for each case",
		RunE: func(cmd *cobra.Command, args []string) error {
			configureCommandLogging(debugMode, false)
			entryPath, err := resolveExecutionEntrypoint(dslFilePath, projectPath)
			if err != nil {
				return err
			}
			if strings.TrimSpace(productName) == "" {
				return fmt.Errorf("required flag(s) \"product\" not set")
			}
			if len(rawParams) == 0 {
				return fmt.Errorf("required flag(s) \"param\" not set")
			}

			planned, err := loadPlannedRun(entryPath, map[string]string{}, args)
			if err != nil {
				return err
			}

			product := findProduct(planned.AST, productName)
			if product == nil {
				return fmt.Errorf("product %q not found in DSL", productName)
			}

			specs, err := parseSweepSpecs(rawParams, product)
			if err != nil {
				return err
			}

			cases := buildSweepCases(specs, 0, map[string]string{}, map[string]any{}, nil)
			summary := sweepReport{
				SchemaVersion:          harnessSchemaVersion,
				Product:                productName,
				Params:                 append([]string(nil), rawParams...),
				TotalCases:             len(cases),
				FailureCountsByMessage: map[string]int{},
				Cases:                  make([]sweepCaseSummary, 0, len(cases)),
			}

			for index, oneCase := range cases {
				entry := sweepCaseSummary{
					Index:  index,
					Inputs: oneCase.display,
				}
				casePlan, err := loadPlannedRun(entryPath, oneCase.overrides, args)
				if err != nil {
					entry.Status = "failed"
					entry.Error = err.Error()
					summary.FailedCases++
					summary.FailureCountsByMessage[entry.Error]++
				} else {
					entry.Status = "passed"
					entry.PlanHash = casePlan.PlanHash
					summary.PassedCases++
				}
				summary.Cases = append(summary.Cases, entry)
			}
			if len(summary.FailureCountsByMessage) == 0 {
				summary.FailureCountsByMessage = nil
			}

			if err := os.MkdirAll(outputDir, 0755); err != nil {
				return err
			}
			if err := writeJSONFile(filepath.Join(outputDir, "sweep_report.json"), summary); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "total generated cases: %d\npassed: %d\nfailed: %d\n", summary.TotalCases, summary.PassedCases, summary.FailedCases)
			return err
		},
	}

	cmd.Flags().StringVar(&productName, "product", "", "Product name to validate against")
	cmd.Flags().StringArrayVar(&rawParams, "param", nil, "Sweep parameter definition (name=start..end:step or name=[A,B])")
	return cmd
}

type generatedSweepCase struct {
	overrides map[string]string
	display   map[string]any
}

func parseSweepSpecs(rawParams []string, product *dsl.ProductNode) ([]sweepParamSpec, error) {
	seen := make(map[string]struct{}, len(rawParams))
	paramTypes := make(map[string]dsl.ParameterType, len(product.Parameters))
	for _, param := range product.Parameters {
		paramTypes[param.Name] = param.Type
	}

	specs := make([]sweepParamSpec, 0, len(rawParams))
	for _, raw := range rawParams {
		name, valueSpec, ok := strings.Cut(raw, "=")
		if !ok || strings.TrimSpace(name) == "" || strings.TrimSpace(valueSpec) == "" {
			return nil, fmt.Errorf("invalid --param value %q", raw)
		}
		name = strings.TrimSpace(name)
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("duplicate --param for %q", name)
		}
		seen[name] = struct{}{}

		paramType, ok := paramTypes[name]
		if !ok {
			return nil, fmt.Errorf("parameter %q is not defined on product %q", name, product.Name)
		}

		spec, err := parseSingleSweepSpec(name, strings.TrimSpace(valueSpec), paramType)
		if err != nil {
			return nil, err
		}
		spec.Raw = raw
		specs = append(specs, spec)
	}
	return specs, nil
}

func parseSingleSweepSpec(name, raw string, paramType dsl.ParameterType) (sweepParamSpec, error) {
	if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
		content := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(raw, "["), "]"))
		if content == "" {
			return sweepParamSpec{}, fmt.Errorf("parameter %q list cannot be empty", name)
		}
		parts := strings.Split(content, ",")
		values := make([]string, 0, len(parts))
		display := make([]any, 0, len(parts))
		for _, part := range parts {
			item := strings.TrimSpace(part)
			if item == "" {
				return sweepParamSpec{}, fmt.Errorf("parameter %q contains an empty list item", name)
			}
			values = append(values, item)
			display = append(display, item)
		}
		return sweepParamSpec{Name: name, Values: values, Display: display}, nil
	}

	rangeParts := strings.Split(raw, ":")
	if len(rangeParts) != 2 {
		return sweepParamSpec{}, fmt.Errorf("parameter %q has malformed range syntax", name)
	}
	bounds := strings.Split(rangeParts[0], "..")
	if len(bounds) != 2 {
		return sweepParamSpec{}, fmt.Errorf("parameter %q has malformed range syntax", name)
	}
	start, err := strconv.ParseFloat(strings.TrimSpace(bounds[0]), 64)
	if err != nil {
		return sweepParamSpec{}, fmt.Errorf("parameter %q has invalid range start", name)
	}
	end, err := strconv.ParseFloat(strings.TrimSpace(bounds[1]), 64)
	if err != nil {
		return sweepParamSpec{}, fmt.Errorf("parameter %q has invalid range end", name)
	}
	step, err := strconv.ParseFloat(strings.TrimSpace(rangeParts[1]), 64)
	if err != nil {
		return sweepParamSpec{}, fmt.Errorf("parameter %q has invalid range step", name)
	}
	if step <= 0 {
		return sweepParamSpec{}, fmt.Errorf("parameter %q must use a positive step", name)
	}
	if end < start {
		return sweepParamSpec{}, fmt.Errorf("parameter %q range end must be greater than or equal to start", name)
	}
	if paramType.Kind != dsl.ParamTypeNumber {
		return sweepParamSpec{}, fmt.Errorf("parameter %q range syntax requires a number parameter", name)
	}

	values := []string{}
	display := []any{}
	for value := start; value <= end+1e-9; value += step {
		rendered := strconv.FormatFloat(value, 'f', -1, 64)
		values = append(values, rendered)
		if strings.ContainsAny(rendered, ".eE") {
			floatValue, _ := strconv.ParseFloat(rendered, 64)
			display = append(display, floatValue)
		} else {
			intValue, err := strconv.Atoi(rendered)
			if err == nil {
				display = append(display, intValue)
			} else {
				display = append(display, rendered)
			}
		}
	}
	return sweepParamSpec{Name: name, Values: values, Display: display}, nil
}

func buildSweepCases(specs []sweepParamSpec, index int, overrides map[string]string, display map[string]any, out []generatedSweepCase) []generatedSweepCase {
	if index == len(specs) {
		overridesCopy := make(map[string]string, len(overrides))
		for key, value := range overrides {
			overridesCopy[key] = value
		}
		displayCopy := make(map[string]any, len(display))
		for key, value := range display {
			displayCopy[key] = value
		}
		return append(out, generatedSweepCase{overrides: overridesCopy, display: displayCopy})
	}

	spec := specs[index]
	for valueIndex, value := range spec.Values {
		overrides[spec.Name] = value
		display[spec.Name] = spec.Display[valueIndex]
		out = buildSweepCases(specs, index+1, overrides, display, out)
	}
	delete(overrides, spec.Name)
	delete(display, spec.Name)
	return out
}
