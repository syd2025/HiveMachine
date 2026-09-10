package cmd

import (
	"testing"
)

func TestUpCmd_Use(t *testing.T) {
	if upCmd.Use != "up" {
		t.Errorf("Expected Use 'up', got %q", upCmd.Use)
	}
}

func TestUpCmd_Short(t *testing.T) {
	expected := "Start the HiveMachine gateway"
	if upCmd.Short != expected {
		t.Errorf("Expected Short %q, got %q", expected, upCmd.Short)
	}
}

func TestDownCmd_Use(t *testing.T) {
	if downCmd.Use != "down" {
		t.Errorf("Expected Use 'down', got %q", downCmd.Use)
	}
}

func TestDownCmd_Short(t *testing.T) {
	expected := "Stop the HiveMachine gateway"
	if downCmd.Short != expected {
		t.Errorf("Expected Short %q, got %q", expected, downCmd.Short)
	}
}

func TestDoctorCmd_Use(t *testing.T) {
	if doctorCmd.Use != "doctor" {
		t.Errorf("Expected Use 'doctor', got %q", doctorCmd.Use)
	}
}

func TestDoctorCmd_Short(t *testing.T) {
	expected := "Check system health and HiveMachine readiness"
	if doctorCmd.Short != expected {
		t.Errorf("Expected Short %q, got %q", expected, doctorCmd.Short)
	}
}

func TestModelsCmd_Use(t *testing.T) {
	if modelsCmd.Use != "models" {
		t.Errorf("Expected Use 'models', got %q", modelsCmd.Use)
	}
}

func TestModelsCmd_Short(t *testing.T) {
	expected := "List available models from the gateway"
	if modelsCmd.Short != expected {
		t.Errorf("Expected Short %q, got %q", expected, modelsCmd.Short)
	}
}

func TestProviderCmd_Use(t *testing.T) {
	if providerCmd.Use != "provider" {
		t.Errorf("Expected Use 'provider', got %q", providerCmd.Use)
	}
}

func TestPayCmd_Use(t *testing.T) {
	if payCmd.Use != "pay" {
		t.Errorf("Expected Use 'pay', got %q", payCmd.Use)
	}
}

func TestPayCmd_Short(t *testing.T) {
	expected := "Payment management"
	if payCmd.Short != expected {
		t.Errorf("Expected Short %q, got %q", expected, payCmd.Short)
	}
}

func TestRootCmd_Use(t *testing.T) {
	if rootCmd.Use != "mayhem" {
		t.Errorf("Expected Use 'mayhem', got %q", rootCmd.Use)
	}
}

func TestRootCmd_Short(t *testing.T) {
	expected := "HiveMachine CLI - P2P AI inference marketplace"
	if rootCmd.Short != expected {
		t.Errorf("Expected Short %q, got %q", expected, rootCmd.Short)
	}
}

func TestRootCmd_Long(t *testing.T) {
	expected := "HiveMachine - Sell inference from any machine, buy at market price."
	if rootCmd.Long != expected {
		t.Errorf("Expected Long %q, got %q", expected, rootCmd.Long)
	}
}

func TestRootCmd_HasSubcommands(t *testing.T) {
	cmds := rootCmd.Commands()
	if len(cmds) != 9 {
		t.Errorf("Expected 9 subcommands, got %d: %v", len(cmds), cmds)
	}
}
