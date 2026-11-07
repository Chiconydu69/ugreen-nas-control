package nasctl

import (
	"strings"
)

// Env holds every path the command reads. The variable names are fixed by the web UI and the goldens, so tests
// point it at a fake plugin tree.
type Env struct {
	FanTool      string // UGREEN_NAS_FAN
	PidFile      string // UGREEN_FAN_PIDFILE
	ErrLog       string // UGREEN_FAN_ERRLOG
	FanCfg       string // UGREEN_FAN_CONFIG
	PubPidFile   string // UGREEN_PUB_PIDFILE
	PubLog       string // UGREEN_PUB_LOG
	LEDDir       string // UGREEN_LED_DIR
	LEDCtl       string // UGREEN_LED_CTL
	LEDI2C       string // UGREEN_LED_I2C
	LEDFlag      string // UGREEN_LED_FLAG
	LEDCron      string // UGREEN_LED_CRON
	LEDCfg       string // UGREEN_LED_CFG
	LEDImportSrc string // UGREEN_LED_IMPORT_SRC
	DMIDir       string // UGREEN_DMI
	PluginsBoot  string // UGREEN_PLUGINS_BOOT
	PluginsWeb   string // UGREEN_PLUGINS_WEB
	ProcModules  string // UGREEN_PROC_MODULES
	UpdateCron   string // UGREEN_UPDATE_CRON
	LegacyPids   []string
	Proc         string // UGREEN_PROC
	UnraidFile   string // UGREEN_UNRAID_VERSION
	Syslog       string // UGREEN_SYSLOG
	Hwmon        string // UGREEN_SYS_HWMON
	Dev          string // UGREEN_DEV
	LEDPidFile   string // UGREEN_PIDFILE
	DisksINI     string // UGREEN_DISKS_INI
	CronD        string // UGREEN_CROND
	SysI2C       string // UGREEN_SYS_I2C
	Panel        string // UGREEN_NAS_PANEL: the multi-call binary; diag runs `panel buttons` (read only) through it
	SensorsDir   string // UGREEN_SENSORS_D: the scripts a fan input may run
	SysPCI       string // UGREEN_SYS_PCI
	SysACPI      string // UGREEN_SYS_ACPI
	LibModules   string // UGREEN_LIB_MODULES
}

func orDefault(get func(string) string, key, def string) string {
	if v := get(key); v != "" {
		return v
	}
	return def
}

// plugin's backend folder); the tools sit next to it and in ../led/backend.
func EnvFromGetenv(get func(string) string, here string) Env {
	const boot = "/boot/config/plugins/ugreen-nas-control"
	ledDir := orDefault(get, "UGREEN_LED_DIR", here+"/../led/backend")
	return Env{
		FanTool:      orDefault(get, "UGREEN_NAS_FAN", here+"/ugreen-nas-fan"),
		PidFile:      orDefault(get, "UGREEN_FAN_PIDFILE", "/var/run/ugreen-nas-control.pid"),
		ErrLog:       orDefault(get, "UGREEN_FAN_ERRLOG", "/var/run/ugreen-nas-control.err"),
		FanCfg:       orDefault(get, "UGREEN_FAN_CONFIG", boot+"/fan.cfg"),
		PubPidFile:   orDefault(get, "UGREEN_PUB_PIDFILE", "/var/run/ugreen-nas-control-mqtt.pid"),
		PubLog:       orDefault(get, "UGREEN_PUB_LOG", "/var/log/ugreen-nas-control-mqtt.log"),
		LEDDir:       ledDir,
		LEDCtl:       orDefault(get, "UGREEN_LED_CTL", ledDir+"/ugreen-led-ctl"),
		LEDI2C:       orDefault(get, "UGREEN_LED_I2C", ledDir+"/ugreen-led-i2c"),
		LEDFlag:      orDefault(get, "UGREEN_LED_FLAG", boot+"/led.enabled"),
		LEDCron:      orDefault(get, "UGREEN_LED_CRON", boot+"/ugreen-nas-control.cron"),
		LEDCfg:       orDefault(get, "UGREEN_LED_CFG", boot+"/leds.cfg"),
		LEDImportSrc: orDefault(get, "UGREEN_LED_IMPORT_SRC", "/boot/config/plugins/ugreen-led-control/leds.cfg"),
		DMIDir:       orDefault(get, "UGREEN_DMI", "/sys/class/dmi/id"),
		PluginsBoot:  orDefault(get, "UGREEN_PLUGINS_BOOT", "/boot/config/plugins"),
		PluginsWeb:   orDefault(get, "UGREEN_PLUGINS_WEB", "/usr/local/emhttp/plugins"),
		ProcModules:  orDefault(get, "UGREEN_PROC_MODULES", "/proc/modules"),
		UpdateCron:   orDefault(get, "UGREEN_UPDATE_CRON", "/usr/local/sbin/update_cron"),
		LegacyPids:   strings.Fields(orDefault(get, "UGREEN_LED_LEGACY_PIDS", "/var/run/ugreen-led-control.pid /var/run/ugreen-led-control-fx.pid")),
		Proc:         orDefault(get, "UGREEN_PROC", "/proc"),
		UnraidFile:   orDefault(get, "UGREEN_UNRAID_VERSION", "/etc/unraid-version"),
		Syslog:       orDefault(get, "UGREEN_SYSLOG", "/var/log/syslog"),
		Hwmon:        orDefault(get, "UGREEN_SYS_HWMON", "/sys/class/hwmon"),
		Dev:          orDefault(get, "UGREEN_DEV", "/dev"),
		LEDPidFile:   orDefault(get, "UGREEN_PIDFILE", "/var/run/ugreen-nas-control.led.pid"),
		DisksINI:     orDefault(get, "UGREEN_DISKS_INI", "/var/local/emhttp/disks.ini"),
		CronD:        orDefault(get, "UGREEN_CROND", "/etc/cron.d"),
		SysI2C:       orDefault(get, "UGREEN_SYS_I2C", "/sys/bus/i2c/devices"),
		Panel:        orDefault(get, "UGREEN_NAS_PANEL", here+"/ugreen-nas"),
		SensorsDir:   orDefault(get, "UGREEN_SENSORS_D", boot+"/sensors.d"),
		SysPCI:       orDefault(get, "UGREEN_SYS_PCI", "/sys/bus/pci/devices"),
		SysACPI:      orDefault(get, "UGREEN_SYS_ACPI", "/sys/bus/acpi/devices"),
		LibModules:   orDefault(get, "UGREEN_LIB_MODULES", "/lib/modules"),
	}
}
