package cadruntime

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"parametron/internal/engine/adapter"
	"parametron/internal/engine/adapter/freecad"
	"parametron/internal/engine/observed"
	"parametron/internal/engine/runtimecap"
)

const (
	FreeCADRuntimeObservedFilename           = "prm.observed.json"
	FreeCADRuntimeReferenceTraversalFilename = "prm.reference-traversal.json"

	FreeCADRuntimeRunStageRequestMaterialization   = "request_materialization"
	FreeCADRuntimeRunStageExecutionRequest         = "execution_request"
	FreeCADRuntimeRunStageOutputPreparation        = "output_preparation"
	FreeCADRuntimeRunStageProcessInvocation        = "process_invocation"
	FreeCADRuntimeRunStageResultLoad               = "result_load"
	FreeCADRuntimeRunStageProcessResultCorrelation = "process_result_correlation"
	FreeCADRuntimeRunStageRuntimeReportedFailure   = "runtime_reported_failure"
	FreeCADRuntimeRunStageArtifactValidation       = "artifact_validation"
	FreeCADRuntimeRunStageObservedLoad             = "observed_load"
	FreeCADRuntimeRunStageObservedCorrelation      = "observed_correlation"
	FreeCADRuntimeRunStageReferenceTraversalLoad   = "reference_traversal_load"
)

// FreeCADRuntimeValidatedArtifact is attempt-local artifact validation state.
// It does not represent acceptance, registration, packaging, or record output.
type FreeCADRuntimeValidatedArtifact struct {
	ID           string
	Format       string
	RelativePath string
	Path         string
}

// FreeCADRuntimeRun retains the inspectable Task 9 inputs and raw Task 10
// process, result, artifact, and observed state.
type FreeCADRuntimeRun struct {
	ObservationRequest        FreeCADRuntimeObservationRequestMaterialization
	ReferenceTraversalRequest FreeCADReferenceTraversalRequestMaterialization
	ExecutionRequest          freecad.FreeCADRuntimeExecutionRequest
	Process                   *freecad.FreeCADRuntimeExecutionResult
	Result                    *freecad.FreeCADRuntimeResult
	Observed                  *observed.Observed
	ObservedPath              string
	ReferenceTraversalPath    string
	ReferenceTraversalJSON    []byte
	Artifacts                 []FreeCADRuntimeValidatedArtifact
}

// FreeCADRuntimeRunError is a typed Task 10 failure with attempt and process
// context. Err retains the underlying filesystem, process, decoder, or Task 9
// cause and may contain multiple causes through errors.Join.
type FreeCADRuntimeRunError struct {
	Stage       string
	Field       string
	AttemptID   string
	Path        string
	ProcessKind string
	ExitCode    int
	Err         error
}

func (e *FreeCADRuntimeRunError) Error() string {
	if e == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("freecad runtime run")
	for _, part := range []string{e.Stage, e.Field} {
		if part != "" {
			b.WriteString(": ")
			b.WriteString(part)
		}
	}
	if e.AttemptID != "" {
		b.WriteString(": attempt=")
		b.WriteString(e.AttemptID)
	}
	if e.Path != "" {
		b.WriteString(": path=")
		b.WriteString(e.Path)
	}
	if e.ProcessKind != "" {
		b.WriteString(": process=")
		b.WriteString(e.ProcessKind)
	}
	if e.ProcessKind != "" || e.ExitCode != 0 {
		fmt.Fprintf(&b, ": exit=%d", e.ExitCode)
	}
	if e.Err != nil {
		b.WriteString(": ")
		b.WriteString(e.Err.Error())
	}
	return b.String()
}

func (e *FreeCADRuntimeRunError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// InvokeAndValidateFreeCADRuntime materializes Task 9 inputs, invokes the
// aligned runtime exactly once, and validates its raw result, artifact files,
// and observed runtime identity. It performs neither Engine verification nor
// artifact acceptance.
func InvokeAndValidateFreeCADRuntime(
	ctx context.Context,
	capability runtimecap.Capability,
	req adapter.CADRuntimeOrchestrationRequest,
) (FreeCADRuntimeRun, error) {
	var run FreeCADRuntimeRun
	if ctx == nil {
		return run, runError(FreeCADRuntimeRunStageExecutionRequest, "Context", "", "", nil,
			fmt.Errorf("context must not be nil"))
	}

	traversalMaterialization, err := ComposeFreeCADReferenceTraversalRequest(req)
	if err != nil {
		return run, runError(FreeCADRuntimeRunStageRequestMaterialization, "ReferenceTraversalRequest", "", "", nil, err)
	}
	materialization, err := WriteFreeCADRuntimeObservationRequest(req)
	if err != nil {
		return run, runError(FreeCADRuntimeRunStageRequestMaterialization, "", "", "", nil, err)
	}
	run.ObservationRequest = materialization
	run.ReferenceTraversalRequest = traversalMaterialization
	attempt := materialization.Manifest.Attempt
	layout := attempt.Layout

	executionRequest := freecad.FreeCADRuntimeExecutionRequest{
		RuntimeCommand:                req.Executable.Path,
		WorkingCopyDir:                layout.WorkingCopyDir,
		ManifestPath:                  layout.ManifestPath,
		ResultPath:                    layout.ResultPath,
		OutputDir:                     layout.OutputDir,
		ObservationRequestPath:        materialization.Path,
		ReferenceTraversalRequestPath: traversalMaterialization.Path,
	}
	run.ExecutionRequest = executionRequest

	observedPath, expectedArtifacts, field, err := validateFreeCADRuntimeExecutionRequest(
		executionRequest, req.Executable.Path, materialization,
	)
	run.ObservedPath = observedPath
	referenceTraversalPath := filepath.Join(executionRequest.OutputDir, FreeCADRuntimeReferenceTraversalFilename)
	run.ReferenceTraversalPath = referenceTraversalPath
	if err != nil {
		return run, runError(FreeCADRuntimeRunStageExecutionRequest, field, attempt.Identity.ID, "", nil, err)
	}

	ownedPaths := make([]string, 0, len(expectedArtifacts)+3)
	ownedPaths = append(ownedPaths, executionRequest.ResultPath, observedPath, referenceTraversalPath)
	for _, artifact := range expectedArtifacts {
		ownedPaths = append(ownedPaths, artifact.Path)
	}
	for _, ownedPath := range ownedPaths {
		if err := removeStaleFreeCADRuntimeOutput(executionRequest.WorkingCopyDir, ownedPath); err != nil {
			return run, runError(FreeCADRuntimeRunStageOutputPreparation, "", attempt.Identity.ID, ownedPath, nil, err)
		}
	}
	writtenTraversal, err := WriteFreeCADReferenceTraversalRequest(req)
	if err != nil {
		return run, runError(FreeCADRuntimeRunStageRequestMaterialization, "ReferenceTraversalRequest", attempt.Identity.ID, traversalMaterialization.Path, nil, err)
	}
	run.ReferenceTraversalRequest = writtenTraversal

	process, invocationErr := freecad.InvokeFreeCADRuntime(capability, ctx, executionRequest)
	run.Process = copyFreeCADRuntimeExecutionResult(process)
	if invocationErr != nil {
		var executionErr *freecad.FreeCADRuntimeExecutionError
		if errors.As(invocationErr, &executionErr) && executionErr.Kind == freecad.FreeCADRuntimeExecutionErrorExit {
			return correlateFailedFreeCADRuntimeProcess(run, invocationErr, executionErr)
		}
		return run, runError(FreeCADRuntimeRunStageProcessInvocation, "", attempt.Identity.ID, "", executionErr, invocationErr)
	}

	result, err := loadRegularFreeCADRuntimeResult(executionRequest.WorkingCopyDir, executionRequest.ResultPath)
	if err != nil {
		return run, runError(FreeCADRuntimeRunStageResultLoad, "ResultPath", attempt.Identity.ID, executionRequest.ResultPath, nil, err)
	}
	run.Result = result
	if result.Status != freecad.FreeCADRuntimeResultStatusSucceeded {
		return run, runError(
			FreeCADRuntimeRunStageProcessResultCorrelation,
			"Result.Status",
			attempt.Identity.ID,
			executionRequest.ResultPath,
			nil,
			fmt.Errorf("process exited successfully with result status %q", result.Status),
		)
	}

	validatedArtifacts, err := validateFreeCADRuntimeArtifacts(result, materialization, expectedArtifacts)
	if err != nil {
		return run, runError(FreeCADRuntimeRunStageArtifactValidation, "Result.Artifacts", attempt.Identity.ID, "", nil, err)
	}
	run.Artifacts = validatedArtifacts

	loadedObserved, err := loadRegularFreeCADRuntimeObserved(executionRequest.WorkingCopyDir, observedPath)
	if err != nil {
		return run, runError(FreeCADRuntimeRunStageObservedLoad, "ObservedPath", attempt.Identity.ID, observedPath, nil, err)
	}
	run.Observed = loadedObserved

	if loadedObserved.WorkingCopy.Path != layout.WorkingCopyDir {
		return run, runError(
			FreeCADRuntimeRunStageObservedCorrelation,
			"Observed.WorkingCopy.Path",
			attempt.Identity.ID,
			observedPath,
			nil,
			fmt.Errorf("got %q, want %q", loadedObserved.WorkingCopy.Path, layout.WorkingCopyDir),
		)
	}
	expectedPreparedSourceDigest := materialization.Contract.Expected.Metadata[0].Value
	if loadedObserved.WorkingCopy.SHA256 != expectedPreparedSourceDigest {
		return run, runError(
			FreeCADRuntimeRunStageObservedCorrelation,
			"Observed.WorkingCopy.SHA256",
			attempt.Identity.ID,
			observedPath,
			nil,
			fmt.Errorf("got %q, want prepared-source digest %q", loadedObserved.WorkingCopy.SHA256, expectedPreparedSourceDigest),
		)
	}

	traversalJSON, err := loadOptionalRegularFreeCADRuntimeBytes(executionRequest.WorkingCopyDir, referenceTraversalPath)
	if err != nil {
		return run, runError(FreeCADRuntimeRunStageReferenceTraversalLoad, "ReferenceTraversalPath", attempt.Identity.ID, referenceTraversalPath, nil, err)
	}
	run.ReferenceTraversalJSON = traversalJSON

	return run, nil
}

func correlateFailedFreeCADRuntimeProcess(
	run FreeCADRuntimeRun,
	invocationErr error,
	executionErr *freecad.FreeCADRuntimeExecutionError,
) (FreeCADRuntimeRun, error) {
	attemptID := run.ObservationRequest.Manifest.Attempt.Identity.ID
	resultPath := run.ExecutionRequest.ResultPath
	result, resultErr := loadRegularFreeCADRuntimeResult(run.ExecutionRequest.WorkingCopyDir, resultPath)
	if resultErr != nil {
		return run, runError(
			FreeCADRuntimeRunStageResultLoad, "ResultPath", attemptID, resultPath, executionErr,
			errors.Join(invocationErr, resultErr),
		)
	}
	run.Result = result
	if result.Status == freecad.FreeCADRuntimeResultStatusFailed {
		return run, runError(
			FreeCADRuntimeRunStageRuntimeReportedFailure, "Result.Status", attemptID, resultPath, executionErr,
			invocationErr,
		)
	}
	return run, runError(
		FreeCADRuntimeRunStageProcessResultCorrelation, "Result.Status", attemptID, resultPath, executionErr,
		errors.Join(invocationErr, fmt.Errorf("process exited non-zero with result status %q", result.Status)),
	)
}

func validateFreeCADRuntimeExecutionRequest(
	req freecad.FreeCADRuntimeExecutionRequest,
	executablePath string,
	materialization FreeCADRuntimeObservationRequestMaterialization,
) (string, []FreeCADRuntimeValidatedArtifact, string, error) {
	layout := materialization.Manifest.Attempt.Layout
	checks := []struct {
		field string
		got   string
		want  string
	}{
		{"RuntimeCommand", req.RuntimeCommand, executablePath},
		{"WorkingCopyDir", req.WorkingCopyDir, layout.WorkingCopyDir},
		{"ManifestPath", req.ManifestPath, layout.ManifestPath},
		{"ResultPath", req.ResultPath, layout.ResultPath},
		{"OutputDir", req.OutputDir, layout.OutputDir},
		{"ObservationRequestPath", req.ObservationRequestPath, materialization.Path},
		{"ReferenceTraversalRequestPath", req.ReferenceTraversalRequestPath, filepath.Join(layout.WorkingCopyDir, FreeCADReferenceTraversalRequestFilename)},
	}
	for _, check := range checks {
		if check.got != check.want {
			return "", nil, check.field, fmt.Errorf("%s does not equal authoritative path: got=%q want=%q", check.field, check.got, check.want)
		}
		if err := requireCanonicalAbsolutePath(check.field, check.got); err != nil {
			return "", nil, check.field, err
		}
	}
	for _, file := range []struct {
		field string
		path  string
	}{
		{"ManifestPath", req.ManifestPath},
		{"ResultPath", req.ResultPath},
		{"ObservationRequestPath", req.ObservationRequestPath},
		{"ReferenceTraversalRequestPath", req.ReferenceTraversalRequestPath},
	} {
		if err := requireStrictPathContainment(req.WorkingCopyDir, file.path); err != nil {
			return "", nil, file.field, err
		}
	}
	if err := requireStrictPathContainment(req.WorkingCopyDir, req.OutputDir); err != nil {
		return "", nil, "OutputDir", err
	}
	wantOutputDir := filepath.Join(req.WorkingCopyDir, layout.OutputDirRelativePath)
	if req.OutputDir != wantOutputDir {
		return "", nil, "OutputDir", fmt.Errorf("does not equal the expected output directory: got=%q want=%q", req.OutputDir, wantOutputDir)
	}

	observedPath := filepath.Join(req.OutputDir, FreeCADRuntimeObservedFilename)
	if err := requireCanonicalAbsolutePath("ObservedPath", observedPath); err != nil {
		return observedPath, nil, "ObservedPath", err
	}
	if err := requireStrictPathContainment(req.OutputDir, observedPath); err != nil {
		return observedPath, nil, "ObservedPath", err
	}
	referenceTraversalPath := filepath.Join(req.OutputDir, FreeCADRuntimeReferenceTraversalFilename)
	if err := requireCanonicalAbsolutePath("ReferenceTraversalPath", referenceTraversalPath); err != nil {
		return observedPath, nil, "ReferenceTraversalPath", err
	}
	if err := requireStrictPathContainment(req.OutputDir, referenceTraversalPath); err != nil {
		return observedPath, nil, "ReferenceTraversalPath", err
	}
	if err := requireStrictPathContainment(req.WorkingCopyDir, referenceTraversalPath); err != nil {
		return observedPath, nil, "ReferenceTraversalPath", err
	}

	reserved := map[string]string{
		req.ManifestPath:                  "ManifestPath",
		req.ResultPath:                    "ResultPath",
		req.ObservationRequestPath:        "ObservationRequestPath",
		req.ReferenceTraversalRequestPath: "ReferenceTraversalRequestPath",
		layout.SourceDocumentPath:         "SourceDocumentPath",
		req.OutputDir:                     "OutputDir",
		observedPath:                      "ObservedPath",
		referenceTraversalPath:            "ReferenceTraversalPath",
	}
	if len(reserved) != 8 {
		return observedPath, nil, "Paths", fmt.Errorf("authoritative runtime paths collide")
	}

	expected := make([]FreeCADRuntimeValidatedArtifact, 0, len(materialization.Manifest.Manifest.Outputs))
	seen := make(map[string]int, len(materialization.Manifest.Manifest.Outputs))
	for index, output := range materialization.Manifest.Manifest.Outputs {
		artifactPath, err := resolveFreeCADRuntimeArtifactPath(req.WorkingCopyDir, req.OutputDir, output.Path)
		if err != nil {
			return observedPath, nil, fmt.Sprintf("Manifest.Outputs[%d].Path", index), err
		}
		if collision, exists := reserved[artifactPath]; exists {
			return observedPath, nil, fmt.Sprintf("Manifest.Outputs[%d].Path", index), fmt.Errorf("artifact path collides with %s", collision)
		}
		if previous, exists := seen[artifactPath]; exists {
			return observedPath, nil, fmt.Sprintf("Manifest.Outputs[%d].Path", index), fmt.Errorf("artifact path duplicates Manifest.Outputs[%d].Path", previous)
		}
		seen[artifactPath] = index
		expected = append(expected, FreeCADRuntimeValidatedArtifact{
			ID:           output.ID,
			Format:       output.Format,
			RelativePath: output.Path,
			Path:         artifactPath,
		})
	}
	return observedPath, expected, "", nil
}

func resolveFreeCADRuntimeArtifactPath(workingCopyDir, outputDir, relativePath string) (string, error) {
	if relativePath == "" {
		return "", fmt.Errorf("relative path is required")
	}
	if strings.Contains(relativePath, "\\") || path.IsAbs(relativePath) || path.Clean(relativePath) != relativePath {
		return "", fmt.Errorf("relative path must be canonical slash-separated path: %q", relativePath)
	}
	for _, segment := range strings.Split(relativePath, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("relative path contains unsafe segment: %q", relativePath)
		}
	}
	absolutePath := filepath.Join(workingCopyDir, filepath.FromSlash(relativePath))
	if err := requireCanonicalAbsolutePath("artifact path", absolutePath); err != nil {
		return absolutePath, err
	}
	if err := requireStrictPathContainment(workingCopyDir, absolutePath); err != nil {
		return absolutePath, err
	}
	if err := requireStrictPathContainment(outputDir, absolutePath); err != nil {
		return absolutePath, err
	}
	return absolutePath, nil
}

func removeStaleFreeCADRuntimeOutput(workingCopyDir, outputPath string) error {
	if err := requireNoSymlinkPathComponents(workingCopyDir, outputPath); err != nil {
		return err
	}
	info, err := os.Lstat(outputPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("owned runtime output is a symlink")
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("owned runtime output is not a regular file: mode=%s", info.Mode())
	}
	return os.Remove(outputPath)
}

func loadRegularFreeCADRuntimeResult(workingCopyDir, resultPath string) (*freecad.FreeCADRuntimeResult, error) {
	if err := requireNoSymlinkPathComponents(workingCopyDir, resultPath); err != nil {
		return nil, &freecad.FreeCADRuntimeResultFileError{Path: resultPath, Err: err}
	}
	if err := requireRegularNonSymlinkFile(resultPath); err != nil {
		return nil, &freecad.FreeCADRuntimeResultFileError{Path: resultPath, Err: err}
	}
	return freecad.LoadFreeCADRuntimeResult(resultPath)
}

func loadRegularFreeCADRuntimeObserved(workingCopyDir, observedPath string) (*observed.Observed, error) {
	if err := requireNoSymlinkPathComponents(workingCopyDir, observedPath); err != nil {
		return nil, &observed.FileError{Path: observedPath, Err: err}
	}
	if err := requireRegularNonSymlinkFile(observedPath); err != nil {
		return nil, &observed.FileError{Path: observedPath, Err: err}
	}
	return observed.LoadFile(observedPath)
}

func loadOptionalRegularFreeCADRuntimeBytes(workingCopyDir, outputPath string) ([]byte, error) {
	if err := requireNoSymlinkPathComponents(workingCopyDir, outputPath); err != nil {
		return nil, err
	}
	if err := requireRegularNonSymlinkFile(outputPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	content, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, err
	}
	if len(content) == 0 {
		return nil, fmt.Errorf("runtime traversal output is empty")
	}
	return content, nil
}

func requireRegularNonSymlinkFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("must be a regular non-symlink file")
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("must be a regular file: mode=%s", info.Mode())
	}
	return nil
}

func validateFreeCADRuntimeArtifacts(
	result *freecad.FreeCADRuntimeResult,
	materialization FreeCADRuntimeObservationRequestMaterialization,
	expected []FreeCADRuntimeValidatedArtifact,
) ([]FreeCADRuntimeValidatedArtifact, error) {
	manifestOutputs := materialization.Manifest.Manifest.Outputs
	if len(result.Artifacts) != len(manifestOutputs) {
		return nil, fmt.Errorf("result artifact count %d does not match manifest output count %d", len(result.Artifacts), len(manifestOutputs))
	}
	validated := make([]FreeCADRuntimeValidatedArtifact, 0, len(expected))
	for index := range manifestOutputs {
		declaration := result.Artifacts[index]
		manifest := manifestOutputs[index]
		if declaration.ID != manifest.ID {
			return nil, fmt.Errorf("artifacts[%d].id got %q, want %q", index, declaration.ID, manifest.ID)
		}
		if declaration.Format != manifest.Format {
			return nil, fmt.Errorf("artifacts[%d].format got %q, want %q", index, declaration.Format, manifest.Format)
		}
		if declaration.Path != manifest.Path {
			return nil, fmt.Errorf("artifacts[%d].path got %q, want %q", index, declaration.Path, manifest.Path)
		}
		resolved, err := resolveFreeCADRuntimeArtifactPath(
			materialization.Manifest.Attempt.Layout.WorkingCopyDir,
			materialization.Manifest.Attempt.Layout.OutputDir,
			declaration.Path,
		)
		if err != nil {
			return nil, fmt.Errorf("artifacts[%d].path: %w", index, err)
		}
		if resolved != expected[index].Path {
			return nil, fmt.Errorf("artifacts[%d].path resolved to %q, want %q", index, resolved, expected[index].Path)
		}
		if err := requireNoSymlinkPathComponents(materialization.Manifest.Attempt.Layout.WorkingCopyDir, resolved); err != nil {
			return nil, fmt.Errorf("artifacts[%d] %q: %w", index, resolved, err)
		}
		if err := requireRegularNonSymlinkFile(resolved); err != nil {
			return nil, fmt.Errorf("artifacts[%d] %q: %w", index, resolved, err)
		}
		validated = append(validated, expected[index])
	}
	return validated, nil
}

func requireNoSymlinkPathComponents(root, target string) error {
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("path component is a symlink: %q", root)
	}
	if !rootInfo.IsDir() {
		return fmt.Errorf("path root is not a directory: %q", root)
	}

	relative, err := filepath.Rel(root, target)
	if err != nil {
		return err
	}
	if relative == "." || relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path %q is not strictly contained under %q", target, root)
	}
	current := root
	for _, segment := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, segment)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("path component is a symlink: %q", current)
		}
	}
	return nil
}

func requireCanonicalAbsolutePath(field, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	if strings.TrimSpace(value) != value || strings.ContainsRune(value, '\x00') || !filepath.IsAbs(value) || filepath.Clean(value) != value {
		return fmt.Errorf("%s must be canonical, absolute, and clean: %q", field, value)
	}
	return nil
}

func requireStrictPathContainment(parent, child string) error {
	relative, err := filepath.Rel(parent, child)
	if err != nil {
		return fmt.Errorf("derive containment under %q: %w", parent, err)
	}
	if relative == "." || relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path %q must be strictly contained under %q", child, parent)
	}
	return nil
}

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func copyFreeCADRuntimeExecutionResult(result *freecad.FreeCADRuntimeExecutionResult) *freecad.FreeCADRuntimeExecutionResult {
	if result == nil {
		return nil
	}
	copyResult := *result
	copyResult.Command.Args = append([]string(nil), result.Command.Args...)
	return &copyResult
}

func runError(
	stage string,
	field string,
	attemptID string,
	path string,
	processErr *freecad.FreeCADRuntimeExecutionError,
	err error,
) *FreeCADRuntimeRunError {
	runErr := &FreeCADRuntimeRunError{
		Stage:     stage,
		Field:     field,
		AttemptID: attemptID,
		Path:      path,
		Err:       err,
	}
	if processErr != nil {
		runErr.ProcessKind = processErr.Kind
		runErr.ExitCode = processErr.ExitCode
	}
	return runErr
}
