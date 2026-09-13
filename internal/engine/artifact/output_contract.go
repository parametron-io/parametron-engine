package artifact

import "strings"

const (
	ExportOutputTypeSTEP = "step"
	ExportOutputTypeCSV  = "csv"
	ExportOutputTypePDF  = "pdf"
)

type ExportOutputSpec struct {
	FilenameExtension string
	RequiresObject    bool
}

var exportOutputSpecs = map[string]ExportOutputSpec{
	ExportOutputTypeSTEP: {FilenameExtension: ".step", RequiresObject: true},
	ExportOutputTypeCSV:  {FilenameExtension: ".csv"},
	ExportOutputTypePDF:  {FilenameExtension: ".pdf"},
}

func NormalizeExportOutputType(outputType string) string {
	return strings.ToLower(strings.TrimSpace(outputType))
}

func ExportOutputSpecFor(outputType string) (ExportOutputSpec, bool) {
	spec, ok := exportOutputSpecs[NormalizeExportOutputType(outputType)]
	return spec, ok
}

func IsSupportedExportOutputType(outputType string) bool {
	_, ok := ExportOutputSpecFor(outputType)
	return ok
}
