package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var hardwareCmd = &cobra.Command{
	Use:   "hardware",
	Short: "Detect hardware capabilities (GPU, CPU, RAM, disk)",
	RunE:  runHardware,
}

func init() {
	rootCmd.AddCommand(hardwareCmd)
}

func runHardware(cmd *cobra.Command, args []string) error {
	fmt.Println("┌──────────────────────────────────────────────────────┐")
	fmt.Println("│          HiveMachine Hardware Detection              │")
	fmt.Println("└──────────────────────────────────────────────────────┘")

	printCPU()
	printRAM()
	printDisk()
	printGPU()

	return nil
}

func printCPU() {
	fmt.Printf("✅ %-20s %s\n", "CPU:", runtime.GOARCH)
	fmt.Printf("  Cores (virt):    %d\n", runtime.NumCPU())
	fmt.Printf("  GOOS/GOARCH:     %s/%s\n", runtime.GOOS, runtime.GOARCH)
}

func printRAM() {
	switch runtime.GOOS {
	case "linux":
		printRAMLinux()
	case "darwin":
		printRAMDarwin()
	case "windows":
		printRAMWindows()
	default:
		fmt.Printf("⚠  %-20s unsupported platform: %s\n", "RAM:", runtime.GOOS)
	}
}

func printRAMLinux() {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		fmt.Printf("❌ %-20s %v\n", "RAM:", err)
		return
	}
	defer f.Close()

	var memTotal, memAvailable uint64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			memTotal = parseKB(strings.TrimPrefix(line, "MemTotal:"))
		}
		if strings.HasPrefix(line, "MemAvailable:") {
			memAvailable = parseKB(strings.TrimPrefix(line, "MemAvailable:"))
		}
	}
	if memTotal == 0 {
		fmt.Printf("❌ %-20s could not read /proc/meminfo\n", "RAM:")
		return
	}
	fmt.Printf("✅ %-20s %.1f GB total / %.1f GB available\n", "RAM:",
		float64(memTotal)/1e6, float64(memAvailable)/1e6)
}

func parseKB(s string) uint64 {
	s = strings.TrimSpace(s)
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return 0
	}
	v, _ := strconv.ParseUint(fields[0], 10, 64)
	return v
}

func printRAMDarwin() {
	out, err := exec.Command("sysctl", "hw.memsize").CombinedOutput()
	if err != nil {
		fmt.Printf("❌ %-20s %v\n", "RAM:", err)
		return
	}
	var memBytes uint64
	fmt.Sscanf(string(out), "hw.memsize = %d", &memBytes)
	fmt.Printf("✅ %-20s %.1f GB total\n", "RAM:", float64(memBytes)/1e9)
}

func printRAMWindows() {
	out, err := exec.Command("powershell", "-NoProfile", "-Command",
		"[math]::Round((Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory/1GB, 1)").CombinedOutput()
	if err != nil {
		fmt.Printf("❌ %-20s %v\n", "RAM:", err)
		return
	}
	fmt.Printf("✅ %-20s %s GB total\n", "RAM:", strings.TrimSpace(string(out)))
}

func printDisk() {
	switch runtime.GOOS {
	case "linux", "darwin":
		printDiskUnix()
	case "windows":
		printDiskWindows()
	default:
		fmt.Printf("⚠  %-20s unsupported platform\n", "Disk:")
	}
}

func dfLine() (availGB, totalGB float64, ok bool) {
	out, err := exec.Command("df", "-B1", ".").CombinedOutput()
	if err != nil {
		return 0, 0, false
	}
	lines := strings.Split(string(out), "\n")
	if len(lines) < 2 {
		return 0, 0, false
	}
	// Last line is the filesystem for current dir.
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 4 {
		return 0, 0, false
	}
	avail, _ := strconv.ParseUint(fields[3], 10, 64)
	total, _ := strconv.ParseUint(fields[1], 10, 64)
	return float64(avail) / 1e9, float64(total) / 1e9, true
}

func printDiskUnix() {
	availGB, totalGB, ok := dfLine()
	if !ok {
		fmt.Printf("❌ %-20s df command failed\n", "Disk:")
		return
	}
	fmt.Printf("✅ %-20s %.1f / %.1f GB available\n", "Disk:", availGB, totalGB)
}

func printDiskWindows() {
	out, err := exec.Command("powershell", "-NoProfile", "-Command",
		"Get-CimInstance Win32_LogicalDisk | Where-Object {$_.DriveType -eq 3} | ForEach-Object { '{0} {1:N1}/{2:N1} GB' -f $_.DeviceID, ($_.FreeSpace/1GB), ($_.Size/1GB) }").CombinedOutput()
	if err != nil {
		fmt.Printf("❌ %-20s %v\n", "Disk:", err)
		return
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			fmt.Printf("  %s\n", line)
		}
	}
}

func printGPU() {
	var path string

	switch runtime.GOOS {
	case "windows":
		path, _ = exec.LookPath("nvidia-smi")
		if path == "" {
			if _, err := os.Stat("C:\\Program Files\\NVIDIA Corporation\\NVSMI\\nvidia-smi.exe"); err == nil {
				path = "C:\\Program Files\\NVIDIA Corporation\\NVSMI\\nvidia-smi.exe"
			}
		}
	case "linux", "darwin":
		path, _ = exec.LookPath("nvidia-smi")
	}

	if path == "" {
		fmt.Printf("⚠  %-20s nvidia-smi not found — no NVIDIA GPU detected\n", "GPU:")
		return
	}

	out, err := exec.Command(path,
		"--query-gpu=name,driver_version,memory.total",
		"--format=csv,noheader").CombinedOutput()
	if err != nil {
		fmt.Printf("⚠  %-20s nvidia-smi error: %v\n", "GPU:", err)
		return
	}

	var name, driver string
	var memMB int
	if _, scanErr := fmt.Sscanf(string(out), "%s %s %d", &name, &driver, &memMB); scanErr != nil {
		fmt.Printf("✅ %-20s %s", "GPU:", strings.TrimSpace(string(out)))
		return
	}
	fmt.Printf("✅ %-20s %s\n", "GPU:", name)
	fmt.Printf("  Driver:           %s\n", driver)
	fmt.Printf("  VRAM:            %d MB\n", memMB)
}
