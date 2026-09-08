package cmd

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

func init() {
	upCmd.Flags().String("addr", "127.0.0.1:11435", "Gateway HTTP address")
	upCmd.Flags().String("core", "127.0.0.1:50051", "Rust core gRPC address")
	upCmd.RunE = runUp
}

func runUp(cmd *cobra.Command, args []string) error {
	dir := mayhemDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create runtime dir: %w", err)
	}

	pidPath := filepath.Join(dir, "gateway.pid")

	// Check if already running.
	if pid, err := readPID(pidPath); err == nil && processAlive(pid) {
		return fmt.Errorf("gateway already running (PID %d). Run 'mayhem down' first.", pid)
	}

	// Check port.
	addr, _ := cmd.Flags().GetString("addr")
	if portListening(addr) {
		return fmt.Errorf("port %s already in use. Stop existing process or change --addr.", addr)
	}

	// Find binary.
	bin, err := findGateway()
	if err != nil {
		return err
	}

	core, _ := cmd.Flags().GetString("core")
	proc, err := startProcess(bin, addr, core)
	if err != nil {
		return fmt.Errorf("start gateway: %w", err)
	}

	os.WriteFile(pidPath, []byte(fmt.Sprintf("%d", proc.Pid)), 0644)

	if !waitPort(addr, 10*time.Second) {
		proc.Kill()
		os.Remove(pidPath)
		return fmt.Errorf("gateway did not open %s within 10s", addr)
	}

	fmt.Printf("✅ HiveMachine Gateway started (PID %d) on %s\n", proc.Pid, addr)
	fmt.Printf("   → Rust core gRPC: %s\n", core)
	return nil
}

func mayhemDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".hivemachine")
}

func readPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var pid int
	fmt.Sscanf(string(data), "%d", &pid)
	return pid, nil
}

func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}


func findGateway() (string, error) {
	paths := []string{
		filepath.Join("cmd", "gateway", "gateway.exe"),
		"gateway.exe",
		os.Getenv("GOPATH") + "/bin/gateway.exe",
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	if path, err := exec.LookPath("gateway"); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("gateway binary not found (build with: go build ./cmd/gateway)")
}

func startProcess(bin, addr, core string) (*os.Process, error) {
	attr := &os.ProcAttr{
		Dir:   filepath.Dir(bin),
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
	}
	return os.StartProcess(bin, []string{bin, "--addr", addr, "--core", core}, attr)
}

func waitPort(addr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			conn.Close()
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}
