package semanticmap

import (
	"fmt"
	"strconv"
	"strings"
)

type VersionStatus string

const (
	VersionStatusMissing              VersionStatus = "missing"
	VersionStatusMalformed            VersionStatus = "malformed"
	VersionStatusSupported            VersionStatus = "supported"
	VersionStatusUnsupportedSameMajor VersionStatus = "unsupported_same_major"
	VersionStatusUnsupportedMajor     VersionStatus = "unsupported_major"
)

type SchemaVersionInfo struct {
	Raw            string
	Status         VersionStatus
	Major          int
	Minor          int
	SupportedMajor int
	SupportedMinor int
	Parsed         bool
}

func SupportedSchemaVersion() string {
	return SchemaVersion
}

func InspectSchemaVersion(version string) SchemaVersionInfo {
	supportedMajor, supportedMinor, _ := parseSchemaVersion(SchemaVersion)
	info := SchemaVersionInfo{
		Raw:            version,
		SupportedMajor: supportedMajor,
		SupportedMinor: supportedMinor,
	}

	if version == "" {
		info.Status = VersionStatusMissing
		return info
	}

	major, minor, ok := parseSchemaVersion(version)
	if !ok {
		info.Status = VersionStatusMalformed
		return info
	}

	info.Major = major
	info.Minor = minor
	info.Parsed = true

	if major == supportedMajor && minor == supportedMinor {
		info.Status = VersionStatusSupported
		return info
	}
	if major == supportedMajor {
		info.Status = VersionStatusUnsupportedSameMajor
		return info
	}

	info.Status = VersionStatusUnsupportedMajor
	return info
}

func ClassifySchemaVersion(version string) VersionStatus {
	return InspectSchemaVersion(version).Status
}

func ValidateSchemaVersion(version string) error {
	switch InspectSchemaVersion(version).Status {
	case VersionStatusSupported:
		return nil
	case VersionStatusMissing:
		return &ValidationError{Problems: []string{"schemaVersion is required"}}
	case VersionStatusMalformed:
		return &ValidationError{Problems: []string{fmt.Sprintf("schemaVersion %q is malformed", version)}}
	case VersionStatusUnsupportedSameMajor, VersionStatusUnsupportedMajor:
		return &ValidationError{Problems: []string{
			fmt.Sprintf("schemaVersion %q is not supported; only %q is supported", version, SchemaVersion),
		}}
	default:
		return &ValidationError{Problems: []string{fmt.Sprintf("schemaVersion %q is malformed", version)}}
	}
}

func parseSchemaVersion(version string) (int, int, bool) {
	if version == "" {
		return 0, 0, false
	}
	parts := strings.Split(version, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return 0, 0, false
	}
	if !isASCIIInt(parts[0]) || !isASCIIInt(parts[1]) {
		return 0, 0, false
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, false
	}
	return major, minor, true
}

func isASCIIInt(value string) bool {
	if value == "" {
		return false
	}
	if len(value) > 1 && value[0] == '0' {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
