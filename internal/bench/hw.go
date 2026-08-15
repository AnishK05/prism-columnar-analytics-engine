package bench

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// Hardware is recorded alongside a bench JSON file.
type Hardware struct {
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	GoVersion  string `json:"go_version"`
	NumCPU     int    `json:"num_cpu"`
	GOMAXPROCS int    `json:"gomaxprocs"`
	Hostname   string `json:"hostname,omitempty"`
	MemBytes   uint64 `json:"mem_bytes,omitempty"`
	Note       string `json:"note"`
}

const hardwareNote = "Windows benches are hot-cache; do not drop the OS page cache. " +
	"peak_rss_bytes is Linux VmHWM when /proc is available; otherwise 0. " +
	"go_mem_sys_bytes is runtime.MemStats.Sys (Go heap + mapped spans). " +
	"Arrow Go v18 typically allocates through the Go runtime, but off-heap RSS can still differ."

// CaptureHardware fills OS/CPU/RAM facts for the result file.
func CaptureHardware() Hardware {
	h := Hardware{
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		GoVersion:  runtime.Version(),
		NumCPU:     runtime.NumCPU(),
		GOMAXPROCS: runtime.GOMAXPROCS(0),
		Note:       hardwareNote,
	}
	if name, err := os.Hostname(); err == nil {
		h.Hostname = name
	}
	h.MemBytes = systemMemory()
	return h
}

func systemMemory() uint64 {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0
		}
		return kb * 1024
	}
	return 0
}

// PeakRSSBytes is Linux VmHWM in bytes, or 0 if unavailable.
func PeakRSSBytes() int64 {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "VmHWM:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kb, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0
		}
		return kb * 1024
	}
	return 0
}

// GoMemSysBytes is runtime.MemStats.Sys.
func GoMemSysBytes() uint64 {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return ms.Sys
}
