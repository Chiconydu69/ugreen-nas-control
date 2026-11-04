package fan

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/hw"
)

var dmiDir = envOr("UGREEN_DMI", hw.DMIDir)

type Info struct {
	Model    string `json:"model"`
	BIOS     string `json:"bios"`
	BIOSDate string `json:"bios_date"`
	CPU      string `json:"cpu"`
	Threads  int    `json:"threads"`
	MemoryKB int    `json:"memory_kb"`
	Kernel   string `json:"kernel"`
	Unraid   string `json:"unraid"`
	Uptime   int    `json:"uptime"`
}

// ModelSupported tells whether this tool can drive the fans of the model: wiring known (DXP6800 Pro, DXP4800 GT,
// iDX6011 Pro) or found by the probe (IT8613E models, DXP2800 GT, see FanKind). A probe model still needs a
// confirmed map. The plain iDX is read only and not driven.
func ModelSupported(model string) bool {
	k := FanKind(model)
	return k != "" && k != KindReadOnly
}

// requireSupportedModel keeps every access to the fan controller ports to the models the tool was made for, and on
// a probe model to a confirmed map: without one the fan wiring is a guess, so fan control stays off.
func requireSupportedModel() error {
	model := productName()
	switch FanKind(model) {
	case KindFixed:
		return nil
	case KindReadOnly:
		// Reads only: the daemon refuses this kind, SetPWM returns errReadOnly.
		return nil
	case KindProbe:
		if _, err := loadFanMap(model); err != nil {
			if errors.Is(err, errNoFanMap) && isDXP2800GT(model) {
				return errCSNotConfirmed
			}
			if isDXP2800GT(model) {
				return fmt.Errorf("%s: %w; confirm again: /usr/local/emhttp/plugins/ugreen-nas-control/backend/ugreen-nas-fan cs201x confirm", model, err)
			}
			if errors.Is(err, errNoFanMap) {
				return fmt.Errorf("%s has no confirmed fan map yet: run the fan probe first (/usr/local/emhttp/plugins/ugreen-nas-control/backend/ugreen-nas-fan probe-fans run)", model)
			}
			return fmt.Errorf("%s: %w; run the fan probe again", model, err)
		}
		return nil
	}
	if model == "" {
		model = "unknown model"
	}
	return fmt.Errorf("%s has no fan control: it exists for the DXP6800 Pro, DXP4800 GT, iDX6011 Pro and, after a fan probe, the other IT8613E models", model)
}

func ReadInfo() Info {
	info := Info{
		Model:    readString(filepath.Join(dmiDir, "product_name")),
		BIOS:     readString(filepath.Join(dmiDir, "bios_version")),
		BIOSDate: readString(filepath.Join(dmiDir, "bios_date")),
		Threads:  runtime.NumCPU(),
		Kernel:   readString("/proc/sys/kernel/osrelease"),
	}

	if f, err := os.Open("/proc/cpuinfo"); err == nil {
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			if key, value, ok := strings.Cut(scanner.Text(), ":"); ok && strings.TrimSpace(key) == "model name" {
				info.CPU = strings.TrimSpace(value)
				break
			}
		}
		_ = f.Close()
	}

	if f, err := os.Open("/proc/meminfo"); err == nil {
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) >= 2 && fields[0] == "MemTotal:" {
				info.MemoryKB, _ = strconv.Atoi(fields[1])
				break
			}
		}
		_ = f.Close()
	}

	if v := readValues("/etc/unraid-version")["version"]; v != "" {
		info.Unraid = v
	}

	if fields := strings.Fields(readString("/proc/uptime")); len(fields) > 0 {
		if f, err := strconv.ParseFloat(fields[0], 64); err == nil {
			info.Uptime = int(f)
		}
	}

	return info
}
