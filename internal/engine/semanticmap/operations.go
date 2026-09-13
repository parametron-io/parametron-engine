package semanticmap

type OperationName string

const (
	OperationSuppress       OperationName = "suppress"
	OperationUnsuppress     OperationName = "unsuppress"
	OperationHide           OperationName = "hide"
	OperationUnhide         OperationName = "unhide"
	OperationDelete         OperationName = "delete"
	OperationWriteParameter OperationName = "write_parameter"
	OperationWriteMetadata  OperationName = "write_metadata"
)

var supportedOperationNames = map[string]struct{}{
	string(OperationSuppress):       {},
	string(OperationUnsuppress):     {},
	string(OperationHide):           {},
	string(OperationUnhide):         {},
	string(OperationDelete):         {},
	string(OperationWriteParameter): {},
	string(OperationWriteMetadata):  {},
}

func SupportedOperationNames() []string {
	return []string{
		string(OperationDelete),
		string(OperationHide),
		string(OperationSuppress),
		string(OperationUnhide),
		string(OperationUnsuppress),
		string(OperationWriteMetadata),
		string(OperationWriteParameter),
	}
}
