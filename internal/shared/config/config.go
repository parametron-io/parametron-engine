package config

import "os"

// FreeCADRuntimeEnv is the Engine-side environment variable for the aligned
// FreeCAD runtime wrapper executable reference (parametron-freecad).
// It is distinct from PARAMETRON_FREECAD_BIN, which identifies the underlying
// FreeCAD host used by the wrapper.
const FreeCADRuntimeEnv = "PARAMETRON_FREECAD_RUNTIME"

const freeCADAdapterID = "freecad"

// Config is an immutable snapshot of Engine configuration.
type Config struct {
	cadRuntimeCommands map[string]string
}

// Load captures Engine configuration from the process environment.
// It does not perform filesystem lookup or start processes.
// Invalid executable references are deferred to runtime executable selection.
func Load() (*Config, error) {
	commands := make(map[string]string)
	if value, ok := os.LookupEnv(FreeCADRuntimeEnv); ok {
		commands[freeCADAdapterID] = value
	}
	return &Config{cadRuntimeCommands: commands}, nil
}

// CADRuntimeCommands returns a defensive copy of configured CAD-runtime
// executable references keyed by adapter identity.
func (c *Config) CADRuntimeCommands() map[string]string {
	if c == nil || c.cadRuntimeCommands == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(c.cadRuntimeCommands))
	for adapter, command := range c.cadRuntimeCommands {
		out[adapter] = command
	}
	return out
}
