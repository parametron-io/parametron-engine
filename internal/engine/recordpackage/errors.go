package recordpackage

import "errors"

var (
	// ErrInvalidLayoutPath reports a layout-relative path that is empty, absolute,
	// contains traversal segments, backslashes, or other unsafe structure.
	ErrInvalidLayoutPath = errors.New("invalid record package layout path")

	// ErrInvalidPackageInput reports missing or malformed writer input.
	ErrInvalidPackageInput = errors.New("invalid record package input")

	// ErrInvalidRecord reports a record payload that is missing, duplicated, or
	// does not match the selected record contract family.
	ErrInvalidRecord = errors.New("invalid record package record")

	// ErrPackageDestinationExists reports a package destination that already
	// contains material outside the canonical package layout.
	ErrPackageDestinationExists = errors.New("record package destination already exists")

	// ErrPackageManifestExists reports an existing package manifest when overwrite
	// is not enabled.
	ErrPackageManifestExists = errors.New("record package manifest already exists")
)
