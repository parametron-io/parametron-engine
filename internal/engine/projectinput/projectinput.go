package projectinput

import (
	"fmt"
	"os"
	"path/filepath"

	"parametron/internal/authoring/dsl"
	"parametron/internal/engine/table"
	"parametron/internal/engine/tableloader"
	"parametron/internal/shared/projectmap"
)

type Resolved struct {
	ProjectFile string
	ProjectRoot string
	DSLPath     string
	ModelPaths  map[string]string
	TablePaths  map[string]string
}

func ResolveProject(inputPath string) (*Resolved, error) {
	projectFile, isProject, err := resolveProjectFilePath(inputPath)
	if err != nil || !isProject {
		return nil, err
	}

	projectFile, err = filepath.Abs(projectFile)
	if err != nil {
		return nil, fmt.Errorf("resolve absolute project file path %q: %w", inputPath, err)
	}

	project, err := projectmap.Load(projectFile)
	if err != nil {
		return nil, err
	}

	projectRoot := filepath.Dir(projectFile)
	dslPath := filepath.Clean(filepath.Join(projectRoot, filepath.FromSlash(project.DSL)))
	if _, err := os.Stat(dslPath); err != nil {
		return nil, fmt.Errorf("load project DSL %q from %q: %w", project.DSL, projectFile, err)
	}

	modelPaths := make(map[string]string, len(project.Resources.Models))
	for logicalID, relativePath := range project.Resources.Models {
		modelPaths[logicalID] = filepath.Clean(filepath.Join(projectRoot, filepath.FromSlash(relativePath)))
	}

	tablePaths := make(map[string]string, len(project.Tables))
	for logicalID, relativePath := range project.Tables {
		tablePaths[logicalID] = filepath.Clean(filepath.Join(projectRoot, filepath.FromSlash(relativePath)))
	}

	return &Resolved{
		ProjectFile: projectFile,
		ProjectRoot: projectRoot,
		DSLPath:     dslPath,
		ModelPaths:  modelPaths,
		TablePaths:  tablePaths,
	}, nil
}

func LoadProjectTables(resolved *Resolved) (map[string]*table.Table, error) {
	if resolved == nil {
		return map[string]*table.Table{}, nil
	}
	return tableloader.LoadFiles(resolved.TablePaths)
}

func MergeTables(projectTables map[string]*table.Table, flagTables map[string]*table.Table) (map[string]*table.Table, error) {
	if len(projectTables) == 0 && len(flagTables) == 0 {
		return map[string]*table.Table{}, nil
	}

	merged := make(map[string]*table.Table, len(projectTables)+len(flagTables))
	for logicalID, tbl := range projectTables {
		merged[logicalID] = tbl
	}
	for logicalID, tbl := range flagTables {
		if _, exists := merged[logicalID]; exists {
			return nil, fmt.Errorf("duplicate table logical ID %q across project mapping and --table", logicalID)
		}
		merged[logicalID] = tbl
	}
	return merged, nil
}

func ApplyModelMappings(ast *dsl.AST, resolved *Resolved) error {
	if ast == nil || resolved == nil {
		return nil
	}

	for _, product := range ast.Products {
		if product.SourceModel == nil {
			continue
		}

		logicalID, err := resolveModelLogicalID(product.SourceModel, ast.ResolvedConstants)
		if err != nil {
			return fmt.Errorf("project %q product %q source_model: %w", resolved.ProjectFile, product.Name, err)
		}

		modelPath, exists := resolved.ModelPaths[logicalID]
		if !exists {
			return fmt.Errorf("project %q product %q references unknown model logical ID %q", resolved.ProjectFile, product.Name, logicalID)
		}
		if _, err := os.Stat(modelPath); err != nil {
			return fmt.Errorf("project %q product %q mapped model %q to %q: %w", resolved.ProjectFile, product.Name, logicalID, modelPath, err)
		}

		product.SourceModel = &dsl.LiteralExpression{Value: modelPath}
	}

	return nil
}

func resolveProjectFilePath(inputPath string) (string, bool, error) {
	info, err := os.Stat(inputPath)
	if err == nil {
		if info.IsDir() {
			projectFile := filepath.Join(inputPath, projectmap.FileName)
			if _, err := os.Stat(projectFile); err != nil {
				return "", false, fmt.Errorf("project directory %q does not contain %s", inputPath, projectmap.FileName)
			}
			return projectFile, true, nil
		}
		if filepath.Base(inputPath) == projectmap.FileName {
			return inputPath, true, nil
		}
		return "", false, nil
	}

	if filepath.Base(inputPath) == projectmap.FileName {
		return inputPath, true, nil
	}

	return "", false, nil
}

func resolveModelLogicalID(expr dsl.ExpressionNode, constScope map[string]dsl.ConstantValue) (string, error) {
	switch e := expr.(type) {
	case *dsl.LiteralExpression:
		value, ok := e.Value.(string)
		if !ok {
			return "", fmt.Errorf("must resolve to string, got %T", e.Value)
		}
		return value, nil
	case *dsl.IdentifierExpression:
		constVal, exists := constScope[e.Name]
		if !exists {
			return "", fmt.Errorf("undefined constant: %s", e.Name)
		}
		value, ok := constVal.Value.(string)
		if !ok {
			return "", fmt.Errorf("constant %q must resolve to string, got %T", e.Name, constVal.Value)
		}
		return value, nil
	default:
		return "", fmt.Errorf("must be a string literal or string constant")
	}
}
