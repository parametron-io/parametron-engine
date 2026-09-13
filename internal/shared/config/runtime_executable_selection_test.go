package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFreeCADRuntimeEnv(t *testing.T) {
	if FreeCADRuntimeEnv != "PARAMETRON_FREECAD_RUNTIME" {
		t.Fatalf("FreeCADRuntimeEnv=%q", FreeCADRuntimeEnv)
	}
}

func TestLoad_CADRuntimeCommandsUnset(t *testing.T) {
	t.Setenv(FreeCADRuntimeEnv, "ignored")
	if err := os.Unsetenv(FreeCADRuntimeEnv); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PARAMETRON_FREECAD_BIN", "ignored")
	if err := os.Unsetenv("PARAMETRON_FREECAD_BIN"); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil || cfg == nil || len(cfg.CADRuntimeCommands()) != 0 {
		t.Fatalf("cfg=%#v commands=%v err=%v", cfg, cfg.CADRuntimeCommands(), err)
	}
	if _, ok := cfg.CADRuntimeCommands()[freeCADAdapterID]; ok {
		t.Fatal("unexpected explicit freecad mapping")
	}
}

func TestLoad_IgnoresParametronFreeCADBin(t *testing.T) {
	t.Setenv(FreeCADRuntimeEnv, "ignored")
	if err := os.Unsetenv(FreeCADRuntimeEnv); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PARAMETRON_FREECAD_BIN", "/custom/freecadcmd")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for adapter, command := range cfg.CADRuntimeCommands() {
		if adapter == freeCADAdapterID || command == "/custom/freecadcmd" {
			t.Fatalf("underlying host leaked into commands: %v", cfg.CADRuntimeCommands())
		}
	}
}

func TestLoad_CapturesFreeCADRuntimeCommand(t *testing.T) {
	want := "/opt/parametron/bin/parametron-freecad"
	t.Setenv(FreeCADRuntimeEnv, want)
	cfg, err := Load()
	if err != nil || cfg.CADRuntimeCommands()[freeCADAdapterID] != want {
		t.Fatalf("commands=%v err=%v", cfg.CADRuntimeCommands(), err)
	}
}

func TestLoad_PreservesConfiguredRuntimeValueForResolverValidation(t *testing.T) {
	for _, value := range []string{"", " ", "parametron-freecad", " parametron-freecad ", "./parametron-freecad", "/missing/parametron-freecad"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv(FreeCADRuntimeEnv, value)
			cfg, err := Load()
			got, ok := cfg.CADRuntimeCommands()[freeCADAdapterID]
			if err != nil || !ok || got != value {
				t.Fatalf("got=%q present=%v err=%v", got, ok, err)
			}
		})
	}
}

func TestConfig_CADRuntimeCommandsReturnsDefensiveCopies(t *testing.T) {
	t.Setenv(FreeCADRuntimeEnv, "original")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	first, second := cfg.CADRuntimeCommands(), cfg.CADRuntimeCommands()
	first[freeCADAdapterID] = "changed"
	first["solidworks"] = "added"
	delete(first, freeCADAdapterID)
	want := map[string]string{freeCADAdapterID: "original"}
	if !reflect.DeepEqual(second, want) || !reflect.DeepEqual(cfg.CADRuntimeCommands(), want) {
		t.Fatalf("second=%v third=%v", second, cfg.CADRuntimeCommands())
	}
}

func TestConfig_CADRuntimeCommandsNilReceiver(t *testing.T) {
	var cfg *Config
	got := cfg.CADRuntimeCommands()
	if got == nil || len(got) != 0 {
		t.Fatalf("commands=%v", got)
	}
	got["freecad"] = "changed"
	if len(cfg.CADRuntimeCommands()) != 0 {
		t.Fatal("nil receiver retained caller mutation")
	}
}

func TestLoad_DoesNotResolveOrExecuteRuntime(t *testing.T) {
	for _, executable := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "executable"}[executable], func(t *testing.T) {
			dir := t.TempDir()
			marker := filepath.Join(dir, "started")
			command := filepath.Join(dir, "missing")
			if executable {
				command = filepath.Join(dir, "runtime")
				script := "#!/bin/sh\ntouch \"" + marker + "\"\n"
				if err := os.WriteFile(command, []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv(FreeCADRuntimeEnv, command)
			cfg, err := Load()
			if err != nil || cfg.CADRuntimeCommands()[freeCADAdapterID] != command {
				t.Fatalf("commands=%v err=%v", cfg.CADRuntimeCommands(), err)
			}
			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("runtime started: %v", err)
			}
		})
	}
}
