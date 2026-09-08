package cmd

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestHardwareCmd_RunsWithoutError(t *testing.T) {
	cmd := hardwareCmd
	buf := &strings.Builder{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	err := runHardware(cmd, nil)
	if err != nil {
		t.Fatalf("runHardware returned error: %v", err)
	}
}

func TestHardwareCmd_HasNoSubcommands(t *testing.T) {
	if len(hardwareCmd.Commands()) != 0 {
		t.Errorf("expected no subcommands, got %d", len(hardwareCmd.Commands()))
	}
}

func TestParseKB(t *testing.T) {
	tests := []struct {
		input    string
		expected uint64
	}{
		{"  16384064 kB", 16384064},
		{"  16384064", 16384064},
		{"1000", 1000},
		{"0", 0},
	}
	for _, tc := range tests {
		got := parseKB(tc.input)
		if got != tc.expected {
			t.Errorf("parseKB(%q): got %d, want %d", tc.input, got, tc.expected)
		}
	}
}

func TestDfLine(t *testing.T) {
	avail, total, ok := dfLine()
	if !ok {
		t.Skip("dfLine not available on this platform")
	}
	if avail < 0 || total < 0 {
		t.Errorf("dfLine returned negative values: avail=%f total=%f", avail, total)
	}
	if avail > total {
		t.Errorf("avail > total: %f > %f", avail, total)
	}
}

func TestPrintCPU(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("printCPU panicked: %v", r)
		}
	}()
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w
	printCPU()
	w.Close()
	os.Stdout = old
	io.Copy(io.Discard, r)
	r.Close()
}
