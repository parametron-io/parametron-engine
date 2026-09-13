// Package recordpackage defines the local Engine-produced record package layout
// and writer used to materialize validated normalized records, artifact payloads,
// raw evidence, and the package manifest.
//
// Contract paths use forward slashes and are stable across platforms. Layout
// helpers describe canonical directory and file paths; the writer materializes
// local package files from explicit PackageInput values.
//
// The package is PDM-ready but PDM-independent: it writes local package files
// from explicit PackageInput values, but it does not integrate package emission
// into normal Engine job/run execution, does not map operational outputs into
// normalized records, and does not write to PDM storage.
package recordpackage
