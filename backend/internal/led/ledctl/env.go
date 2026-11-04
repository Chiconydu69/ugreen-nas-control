package ledctl

import (
	"path/filepath"
	"slices"
	"strings"
)

// Env holds every path and switch the controller reads. The names of the
// environment variables are fixed by the web UI and the goldens, so tests and the
// harness can point the tool at a fake system.
type Env struct {
	Tool           string   // UGREEN_LED_I2C
	ToolFromEnv    bool     // UGREEN_LED_I2C set: no SMBus preflight (tests bring their own tool)
	Self           string   // resolved path of this executable (re-run for the daemon and timers)
	SelfArgs       []string // arguments in front of the command when Self is re-run (the multicall binary: "led-ctl")
	ToolArgs       []string // the same for Tool (the multicall binary: "led-i2c")
	Config         string
	DisksINI       string
	VarINI         string
	SmartDir       string
	SysBlock       string
	SysNet         string
	SysATA         string
	SysPCI         string
	PidFile        string
	ScheduleState  string
	FxPidFile      string
	FxLog          string
	IdentifyPrefix string
	AlertState     string
	PhasePrefix    string
	ShutdownMark   string
	BusCache       string
	DynamixCfg     string
	UdevData       string
	MaxspeedCache  string
	NotifyDir      string // UGREEN_NOTIFY_DIR, empty = dynamix.cfg or /tmp/notifications
	Now            string // UGREEN_NOW "HHMM", empty = the clock
	Product        string // UGREEN_PRODUCT or DMI product name

	// Fixed system paths; injectable here for unit tests.
	DMIProduct  string
	OSRelease   string
	ProcIomem   string
	ProcModules string
	SysI2C      string
	Proc        string // proc filesystem, to tell our own pids from reused ones
}

func orDefault(get func(string) string, key, def string) string {
	if v := get(key); v != "" {
		return v
	}
	return def
}

func EnvFromGetenv(get func(string) string, self string) Env {
	e := Env{
		Self:           self,
		Config:         orDefault(get, "UGREEN_LED_CONFIG", "/boot/config/plugins/ugreen-nas-control/leds.cfg"),
		DisksINI:       orDefault(get, "UGREEN_DISKS_INI", "/var/local/emhttp/disks.ini"),
		VarINI:         orDefault(get, "UGREEN_VAR_INI", "/var/local/emhttp/var.ini"),
		SmartDir:       orDefault(get, "UGREEN_SMART_DIR", "/var/local/emhttp/smart"),
		SysBlock:       orDefault(get, "UGREEN_SYS_BLOCK", "/sys/block"),
		SysNet:         orDefault(get, "UGREEN_SYS_NET", "/sys/class/net"),
		SysATA:         orDefault(get, "UGREEN_SYS_ATA", "/sys/class/ata_port"),
		SysPCI:         orDefault(get, "UGREEN_SYS_PCI", "/sys/bus/pci/devices"),
		PidFile:        orDefault(get, "UGREEN_PIDFILE", "/var/run/ugreen-nas-control.led.pid"),
		ScheduleState:  orDefault(get, "UGREEN_SCHEDULE_STATE", "/var/run/ugreen-nas-control.led.schedule"),
		FxPidFile:      orDefault(get, "UGREEN_FX_PIDFILE", "/var/run/ugreen-nas-control.led-fx.pid"),
		FxLog:          orDefault(get, "UGREEN_FX_LOG", "/var/run/ugreen-nas-control.led-fx.log"),
		IdentifyPrefix: orDefault(get, "UGREEN_IDENTIFY_PREFIX", "/var/run/ugreen-nas-control.led.identify-"),
		AlertState:     orDefault(get, "UGREEN_ALERT_STATE", "/var/run/ugreen-nas-control.led.alert"),
		PhasePrefix:    orDefault(get, "UGREEN_PHASE_PREFIX", "/var/run/ugreen-nas-control.led.phase-"),
		ShutdownMark:   orDefault(get, "UGREEN_SHUTDOWN_MARK", "/var/run/ugreen-nas-control.led.shutdown"),
		BusCache:       orDefault(get, "UGREEN_BUS_CACHE", "/var/run/ugreen-nas-control.led.bus"),
		DynamixCfg:     orDefault(get, "UGREEN_DYNAMIX_CFG", "/boot/config/plugins/dynamix/dynamix.cfg"),
		UdevData:       orDefault(get, "UGREEN_UDEV_DATA", "/run/udev/data"),
		MaxspeedCache:  orDefault(get, "UGREEN_MAXSPEED_CACHE", "/var/run/ugreen-nas-control.led.maxspeed"),
		NotifyDir:      get("UGREEN_NOTIFY_DIR"),
		Now:            get("UGREEN_NOW"),
		DMIProduct:     orDefault(get, "UGREEN_DMI_PRODUCT", "/sys/class/dmi/id/product_name"),
		OSRelease:      orDefault(get, "UGREEN_OSRELEASE", "/proc/sys/kernel/osrelease"),
		ProcIomem:      orDefault(get, "UGREEN_PROC_IOMEM", "/proc/iomem"),
		ProcModules:    orDefault(get, "UGREEN_PROC_MODULES", "/proc/modules"),
		SysI2C:         orDefault(get, "UGREEN_SYS_I2C", "/sys/bus/i2c/devices"),
		Proc:           orDefault(get, "UGREEN_PROC", "/proc"),
	}

	e.Tool = get("UGREEN_LED_I2C")
	e.ToolFromEnv = e.Tool != ""
	if !e.ToolFromEnv {
		e.Tool = filepath.Join(filepath.Dir(self), "ugreen-led-i2c")
	}

	e.Product = get("UGREEN_PRODUCT")
	if e.Product == "" {
		e.Product, _ = readLine(e.DMIProduct)
	}
	if e.Product == "" {
		e.Product = "UGREEN NAS"
	}
	return e
}

// bayCount: DXP6800 Pro -> 6, DX4600 Pro -> 4, DXP8800 Plus -> 8, DXP480T Plus -> 0.
func bayCount(product string) int {
	if strings.HasPrefix(product, "DXP480T") {
		return 0 // power LED only
	}
	if globMatch("iDX601[12]*", product) {
		return 6
	}
	if len(product) >= 5 && strings.HasPrefix(product, "DX") {
		rest := product[2:]
		rest = strings.TrimPrefix(rest, "P")
		if len(rest) >= 4 && rest[0] >= '2' && rest[0] <= '8' && isDigits(rest[1:4]) {
			return int(rest[0] - '0')
		}
	}
	return 8
}

func (e Env) selfSpec(args ...string) SpawnSpec {
	return SpawnSpec{Path: e.Self, Args: append(slices.Clone(e.SelfArgs), args...)}
}

func (e Env) toolSpec(args []string, stderr string) SpawnSpec {
	return SpawnSpec{Path: e.Tool, Args: append(slices.Clone(e.ToolArgs), args...), Stderr: stderr}
}
