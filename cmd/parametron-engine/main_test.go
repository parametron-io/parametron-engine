package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if err := run(context.Background(), []string{"--version"}, &stdout, &stderr); err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	if got := stdout.String(); got != "parametron-engine version dev\n" {
		t.Fatalf("unexpected version output: %q", got)
	}

	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr output, got %q", stderr.String())
	}
}

func TestRunHelp(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if err := run(context.Background(), []string{"--help"}, &stdout, &stderr); err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	help := stdout.String()
	if !strings.Contains(help, "Usage of parametron-engine:") {
		t.Fatalf("expected usage header, got %q", help)
	}
	if !strings.Contains(help, "--addr") {
		t.Fatalf("expected addr flag in help output, got %q", help)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr output, got %q", stderr.String())
	}
}
