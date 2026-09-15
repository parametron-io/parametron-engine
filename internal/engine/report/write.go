package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// FileName is the active report output contract filename.
const FileName = "prm.report.json"

func Write(runRoot string, report Report) error {
	if err := os.MkdirAll(runRoot, 0755); err != nil {
		return fmt.Errorf("create run root: %w", err)
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	data = append(data, '\n')

	path := filepath.Join(runRoot, FileName)
	tmpFile, err := os.CreateTemp(runRoot, ".report-tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary report file: %w", err)
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write temporary report file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temporary report file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("finalize report file: %w", err)
	}

	return nil
}
