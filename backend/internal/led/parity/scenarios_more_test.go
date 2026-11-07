package parity

import (
	"fmt"
	"strconv"
	"time"
)

func more() []scenario {
	night := []string{"night_enabled", "1", "night_start", "22:00", "night_end", "07:00", "night_brightness", "20",
		"power_brightness", "200", "disk1_brightness", "100", "netdev_brightness", "255", "disk2_color", "9 9 9"}
	cfg := func(extra ...string) func(r *rig) {
		return func(r *rig) { r.config(append(append([]string{}, night...), extra...)...) }
	}

	var list []scenario

	for _, p := range []string{"DXP8800 Plus", "DX4600 Pro", "DXP4800 Plus", "DXP2800", "DX4700+", "iDX6011 Pro", "iDX6012B", "UGREEN NAS", "DXP4800 GT", "DXP480T Plus", "DXP6800 Max", "DXP9999 Future"} {
		s := scenario{name: "model-" + p, product: p, run: func(r *rig) {
			r.runs("status", "list", "get disk7", "get disk8", "all", "bays")
		}}
		if p == "iDX6011 Pro" {
			s.intended = "the Pro lists netdev2, its second LAN LED (register 2, leds-mcu.c:104-108@c75c5ab); the shell has no such LED"
		}
		if p == "DXP2800" {
			s.intended = "mapping is tested: the bay order was confirmed with Identify on a SUB2 board (#214); the shell said reported"
		}
		if p == "DXP480T Plus" {
			s.intended = "the model has no bays; the shell listed 8"
		}
		list = append(list, s)
	}

	list = append(list,
		scenario{name: "dxp480t-other", inert: true, silent: true, run: func(r *rig) { r.runs("dxp480t") }},
		scenario{name: "dxp480t", product: "DXP480T Plus", run: func(r *rig) { r.runs("dxp480t") }},

		scenario{name: "schedule-night", setup: cfg("disk1_effect", "blink"), run: func(r *rig) {
			r.env["UGREEN_NOW"] = "2330"
			r.runs("schedule", "schedule", "all", "schedule --force")
			r.env["UGREEN_NOW"] = "1200"
			r.runs("schedule", "schedule", "status")
			r.env["UGREEN_NOW"] = "0659"
			r.runs("schedule", "schedule --force")
			r.env["UGREEN_NOW"] = "0700"
			r.runs("schedule")
		}},
		scenario{name: "schedule-off", setup: cfg("night_action", "off"), run: func(r *rig) {
			r.env["UGREEN_NOW"] = "0300"
			r.runs("schedule", "all", "status")
		}},
		scenario{name: "schedule-equal-times", setup: cfg("night_start", "10:00", "night_end", "10:00"), run: func(r *rig) {
			r.env["UGREEN_NOW"] = "1000"
			r.runs("schedule", "status")
		}},
		scenario{name: "schedule-daemon", unordered: true, setup: func(r *rig) {
			cfg("netdev_status", "manual", "netdev_color", "1 2 3")(r)
			r.sleeper("run/led.pid")
		}, run: func(r *rig) {
			r.env["UGREEN_NOW"] = "2330"
			r.runs("schedule --force")
			r.settle(1500 * time.Millisecond)
		}},
		scenario{name: "schedule-shutdown-mark", setup: cfg(), run: func(r *rig) {
			r.write("run/shutdown", "")
			r.runs("schedule", "alerts", "alerts --force", "schedule --force")
		}},
		scenario{name: "schedule-logger-fails", setup: cfg(), run: func(r *rig) {
			r.write("stubs/logger.fail", "")
			r.env["UGREEN_NOW"] = "2330"
			r.runs("schedule")
		}},

		scenario{name: "alerts", setup: func(r *rig) {
			r.config("alert_enabled", "1", "alert_color", "255 0 0", "power_brightness", "150", "power_color", "1 2 3")
		}, run: func(r *rig) {
			r.runs("alerts", "alerts --force", "alerts --force")
			r.notification("n1", "warning")
			r.runs("alerts", "alerts", "get power", "status")
			r.remove("notify/unread/n1.notify")
			r.runs("alerts", "get power", "alerts", "alerts --force")
		}},
		scenario{name: "alerts-level-alert", setup: func(r *rig) { r.config("alert_enabled", "1", "alert_level", "alert") }, run: func(r *rig) {
			r.notification("n1", "warning")
			r.runs("alerts", "status")
			r.notification("n2", "alert")
			r.runs("alerts", "status")
		}},
		scenario{name: "alerts-disabled", setup: func(r *rig) { r.config("power_color", "1 1 1") }, run: func(r *rig) {
			r.notification("n1", "alert")
			r.runs("alerts", "alerts --force")
		}},
		scenario{name: "alerts-night-off", setup: cfg("alert_enabled", "1", "night_action", "off"), run: func(r *rig) {
			r.env["UGREEN_NOW"] = "2330"
			r.notification("n1", "alert")
			r.runs("alerts")
			r.env["UGREEN_NOW"] = "1200"
			r.runs("alerts")
		}},
		scenario{name: "alerts-night-dim", setup: cfg("alert_enabled", "1"), run: func(r *rig) {
			r.env["UGREEN_NOW"] = "2330"
			r.notification("n1", "alert")
			r.runs("alerts", "get power")
		}},
		scenario{name: "alerts-fx-on-power", setup: func(r *rig) {
			r.config("alert_enabled", "1", "fx_name", "rainbow", "fx_leds", "power,disk1")
			r.sleeper("run/fx.pid")
		}, run: func(r *rig) {
			r.notification("n1", "alert")
			r.runs("alerts", "fx status")
			r.remove("notify/unread/n1.notify")
			r.runs("alerts")
			r.settle(1200 * time.Millisecond)
		}},
		scenario{name: "alerts-via-schedule", setup: func(r *rig) {
			r.config("alert_enabled", "1", "night_enabled", "1")
		}, run: func(r *rig) {
			r.notification("n1", "alert")
			r.runs("schedule", "schedule", "get power")
		}},
		scenario{name: "alerts-notify-from-dynamix", setup: func(r *rig) {
			r.config("alert_enabled", "1")
			r.write("dynamix.cfg", "[notify]\nsomething=\"1\"\npath=\""+r.path("alt")+"\"\npath=\"/second\"\n")
			r.write("alt/unread/q.notify", "importance=\"alert\"\n")
		}, run: func(r *rig) { r.runs("alerts", "status") }},

		scenario{name: "shutdown", setup: func(r *rig) {
			r.config("shutdown_blink", "1", "shutdown_color", "200 10 10", "power_brightness", "180")
		}, run: func(r *rig) {
			r.write("stubs/runlevel", "N 3\n")
			r.runs("shutdown", "get power")
			r.write("stubs/runlevel", "3 6\n")
			r.runs("shutdown", "get power", "schedule")
			r.write("stubs/runlevel", "N 0\n")
			r.remove("run/shutdown")
			r.runs("shutdown")
		}},
		scenario{name: "shutdown-off", inert: true, silent: true, setup: func(r *rig) { r.config("power_color", "1 2 3") }, run: func(r *rig) {
			r.write("stubs/runlevel", "3 6\n")
			r.runs("shutdown")
		}},
		scenario{name: "shutdown-breath-night", setup: cfg("shutdown_blink", "1", "shutdown_effect", "breath"), run: func(r *rig) {
			r.write("stubs/runlevel", "3 6\n")
			r.env["UGREEN_NOW"] = "2330"
			r.runs("shutdown", "get power")
		}},
		scenario{name: "shutdown-night-dark", setup: cfg("shutdown_blink", "1", "night_action", "off"), run: func(r *rig) {
			r.write("stubs/runlevel", "3 6\n")
			r.env["UGREEN_NOW"] = "2330"
			r.runs("shutdown", "get power")
		}},
		scenario{name: "shutdown-test", setup: func(r *rig) {
			r.config("shutdown_color", "20 30 40", "power_color", "7 8 9", "power_brightness", "90")
		}, run: func(r *rig) {
			r.runs("shutdown --test", "get power")
			r.sleep(7500 * time.Millisecond)
			r.runs("get power")
		}},
		scenario{name: "power-restore", setup: func(r *rig) {
			r.config("alert_enabled", "1", "power_color", "7 8 9", "fx_name", "temperature", "fx_leds", "power")
			r.write("run/alert", "alert\n")
		}, run: func(r *rig) {
			r.runs("power-restore", "get power", "fx status")
			r.settle(700 * time.Millisecond)
		}},

		scenario{name: "fx", setup: func(r *rig) {
			r.config("fx_name", "rainbow", "fx_leds", "power,disk1", "fx_speed", "3", "fx_reverse", "1", "fx_idle", "1")
		}, run: func(r *rig) {
			r.runs("fx status", "fx start", "fx status", "fx restart", "fx status", "status", "fx stop", "fx status", "fx stop")
		}},
		scenario{name: "fx-defaults", setup: func(r *rig) { r.config("fx_name", "temperature") }, run: func(r *rig) {
			r.runs("fx start", "fx stop")
		}},
		scenario{name: "fx-none", inert: true, run: func(r *rig) { r.runs("fx start", "fx restart", "fx status", "fx") }},
		scenario{name: "fx-fails", setup: func(r *rig) {
			r.config("fx_name", "temperature", "fx_leds", "power")
			r.write("stubs/fx.fail", "")
		}, run: func(r *rig) { r.runs("fx start", "fx status") }},

		scenario{name: "identify", run: func(r *rig) {
			r.runs("color disk1 10 20 30", "brightness disk1 99", "blink disk1 600 700")
			r.runs("identify disk1", "get disk1", "status", "identify disk1 5", "identify-stop disk1 wrongtoken", "get disk1", "identify-stop disk1", "get disk1", "identify-stop disk1", "identify-stop nothing")
			r.settle(300 * time.Millisecond)
		}},
		scenario{name: "identify-off-led", run: func(r *rig) {
			r.runs("off disk2", "identify disk2 1")
			r.sleep(2500 * time.Millisecond)
			r.runs("get disk2", "identify disk2 2", "identify disk2 1")
			r.sleep(2500 * time.Millisecond)
			r.runs("get disk2")
		}},
		scenario{name: "identify-errors", run: func(r *rig) {
			r.runs("identify power", "identify netdev 5", "identify disk9", "identify nope", "identify disk1 abc", "identify disk1 -3", "identify disk7 5", "identify disk1 ''")
			r.write("stubs/chip.fail", "")
			r.runs("identify disk1")
		}},

		scenario{name: "daemon-ctl", setup: func(r *rig) {
			r.config("disk1_color", "3 3 3", "power_brightness", "50")
		}, run: func(r *rig) {
			r.runs("daemon status", "daemon", "daemon stop", "daemon status")
			r.runs("daemon start")
			r.sleep(1500 * time.Millisecond)
			r.runs("daemon status", "daemon start", "status")
			r.runs("daemon restart")
			r.sleep(1500 * time.Millisecond)
			r.runs("daemon status", "daemon stop", "daemon status")
			r.sleep(400 * time.Millisecond)
		}, unordered: true},
		scenario{name: "daemon-start-foreign", setup: func(r *rig) {
			r.write("stubs/pgrep.found", "")
		}, run: func(r *rig) { r.runs("daemon start", "daemon restart", "daemon status", "status") }},
		scenario{name: "daemon-stale-pid", run: func(r *rig) {
			r.write("run/led.pid", "999999\n")
			r.runs("daemon status", "status")
			r.write("run/led.pid", "abc\n")
			r.runs("daemon status")
			r.write("run/led.pid", "")
			r.runs("daemon status")
			r.write("run/led.pid", strconv.Itoa(1)+"\n")
			r.remove("run/led.pid")
			r.runs("daemon stop")
		}},

		// SMBus preflight (no tool in the environment: the script checks the adapter itself).
		scenario{name: "smbus-missing", noToolEnv: true, setup: func(r *rig) {
			r.write("stubs/dmesg", "i2c_i801 0000:00:1f.4: failed\nother line\nSMBus fail\nDesignWare x\n")
		}, run: func(r *rig) { r.runs("smbus", "help") }},
		scenario{name: "smbus-missing-gt", noToolEnv: true, product: "DXP4800 GT", run: func(r *rig) { r.runs("smbus") }},
		scenario{name: "smbus-no-modprobe", noToolEnv: true, setup: func(r *rig) {
			r.write("stubs/modprobe.fail", "")
		}, run: func(r *rig) { r.runs("smbus") }},
	)

	// The daemon loop: first pass, then a change, then the end.
	daemonScenarios := []struct {
		name  string
		setup func(r *rig)
		run   func(r *rig)
	}{
		{"daemon-run-default", nil, func(r *rig) { r.daemon(func() { r.quiet() }) }},
		{"daemon-run-activity", func(r *rig) {
			r.config("activity_enabled", "1")
		}, func(r *rig) {
			r.daemon(func() {
				r.quiet()
				r.write("sys/block/sdc/stat", "101 0 800 5 51 0 400 5 0 0 0 0\n")
				r.write("sys/class/net/eth0/statistics/rx_bytes", "5000\n")
				r.quiet()
			})
		}},
		{"daemon-run-activity-off", func(r *rig) { r.config("activity_enabled", "0") }, func(r *rig) {
			r.daemon(func() {
				r.quiet()
				r.write("sys/block/sdc/stat", "101 0 800 5 51 0 400 5 0 0 0 0\n")
				r.quiet()
			})
		}},
		{"daemon-run-temperature", func(r *rig) {
			r.config("disk_color_mode", "temperature")
			r.write("disks.ini", diskSection("disk1", "sdc", "A", "DISK_OK", "0", "20", "0")+diskSection("disk2", "sdd", "B", "DISK_OK", "0", "40", "0")+
				diskSection("disk3", "sde", "C", "DISK_OK", "0", "46", "0")+diskSection("disk4", "sdf", "D", "DISK_OK", "0", "52", "0")+
				diskSection("disk5", "sda", "E", "DISK_OK", "0", "70", "0")+diskSection("disk6", "sdb", "F", "DISK_OK", "0", "x", "0"))
			r.remove("smart")
		}, func(r *rig) { r.daemon(func() { r.quiet() }) }},
		{"daemon-run-standby-dim", func(r *rig) { r.config("standby_mode", "dim", "standby_level", "50") }, func(r *rig) { r.daemon(func() { r.quiet() }) }},
		{"daemon-run-standby-pulse", func(r *rig) { r.config("standby_mode", "pulse") }, func(r *rig) { r.daemon(func() { r.quiet() }) }},
		{"daemon-run-standby-off", func(r *rig) { r.config("standby_mode", "off") }, func(r *rig) { r.daemon(func() { r.quiet() }) }},
		{"daemon-run-sync", func(r *rig) {
			r.config("sync_color", "0 50 255")
			r.write("var.ini", "mdResync=\"100\"\nmdResyncPos=\"50\"\nmdResyncSize=\"100\"\nmdResyncAction=\"check\"\n")
		}, func(r *rig) { r.daemon(func() { r.quiet() }) }},
		{"daemon-run-sync-disabled", func(r *rig) {
			r.config("sync_enabled", "0")
			r.write("var.ini", "mdResync=\"100\"\nmdResyncPos=\"50\"\nmdResyncSize=\"100\"\n")
		}, func(r *rig) { r.daemon(func() { r.quiet() }) }},
		{"daemon-run-smart", func(r *rig) {
			r.config("smart_color", "10 20 30")
		}, func(r *rig) { r.daemon(func() { r.quiet() }) }},
		{"daemon-run-smart-acked", func(r *rig) {
			r.config("smart_ack_ST4000_SN3", "5=8")
		}, func(r *rig) { r.daemon(func() { r.quiet() }) }},
		{"daemon-run-errors-off", func(r *rig) { r.config("error_enabled", "0", "smart_enabled", "0") }, func(r *rig) { r.daemon(func() { r.quiet() }) }},
		{"daemon-run-night", cfg("disk3_brightness", "255", "disk4_color", "1 2 3"), func(r *rig) {
			r.env["UGREEN_NOW"] = "2330"
			r.daemon(func() { r.quiet() })
		}},
		{"daemon-run-night-off", cfg("night_action", "off"), func(r *rig) {
			r.env["UGREEN_NOW"] = "0100"
			r.daemon(func() { r.quiet() })
		}},
		{"daemon-run-config-change", func(r *rig) { r.config("disk1_color", "1 1 1") }, func(r *rig) {
			r.daemon(func() {
				r.quiet()
				r.config("disk1_color", "2 2 2", "disk1_brightness", "10", "netdev_color", "3 3 3")
				r.sleep(6 * time.Second)
				r.quiet()
			})
		}},
		{"daemon-run-netdev-manual", func(r *rig) {
			r.config("netdev_status", "manual", "netdev_color", "5 6 7", "netdev_brightness", "90", "netdev_effect", "breath", "netdev_on", "1300", "netdev_off", "800")
		}, func(r *rig) { r.daemon(func() { r.quiet() }) }},
		{"daemon-run-netdev-link", func(r *rig) { r.config("netdev_status", "link") }, func(r *rig) {
			r.daemon(func() {
				r.quiet()
				r.write("sys/class/net/eth0/statistics/rx_bytes", "5000\n")
				r.quiet()
			})
		}},
		{"daemon-run-netdev-down", func(r *rig) {
			r.config("disk_error_color", "9 0 0")
			r.write("sys/class/net/eth0/operstate", "down\n")
		}, func(r *rig) { r.daemon(func() { r.quiet() }) }},
		{"daemon-run-netdev-gateway-down", func(r *rig) {
			r.write("stubs/ping.fail", "")
		}, func(r *rig) { r.daemon(func() { r.quiet(); r.sleep(500 * time.Millisecond); r.quiet() }) }},
		{"daemon-run-netdev-speed", func(r *rig) {
			r.config("netdev_speed_enabled", "1", "netdev_speed_1000", "1 2 3")
		}, func(r *rig) { r.daemon(func() { r.quiet() }) }},
		{"daemon-run-netdev-off", func(r *rig) { r.config("netdev_brightness", "0") }, func(r *rig) { r.daemon(func() { r.quiet() }) }},
		{"daemon-run-no-network", func(r *rig) { r.write("stubs/ip.out", "") }, func(r *rig) { r.daemon(func() { r.quiet() }) }},
		{"daemon-run-identify", func(r *rig) {
			r.write("run/identify-disk1", "tok\nsnapshot\n")
		}, func(r *rig) {
			r.daemon(func() {
				r.quiet()
				r.remove("run/identify-disk1")
				r.sleep(6 * time.Second)
				r.quiet()
			})
		}},
		{"daemon-run-foreign-monitor", func(r *rig) {
			r.write("stubs/pgrep.found", "")
			r.write("run/led.pid", "12345\n")
		}, func(r *rig) { r.daemon(func() { r.sleep(800 * time.Millisecond) }) }},
		{"daemon-run-serial-map", func(r *rig) { r.config("bay1_serial", "SN6", "bay2_serial", "SN1") }, func(r *rig) { r.daemon(func() { r.quiet() }) }},
		{"daemon-run-no-disks", func(r *rig) {
			r.remove("sys/block")
			r.remove("disks.ini")
		}, func(r *rig) { r.daemon(func() { r.quiet() }) }},
		{"daemon-run-chip-down", func(r *rig) { r.write("stubs/chip.fail", "") }, func(r *rig) { r.daemon(func() { r.sleep(900 * time.Millisecond) }) }},
	}
	for _, d := range daemonScenarios {
		sc := scenario{name: d.name, unordered: true, setup: d.setup, run: d.run}
		if d.name != "daemon-run-foreign-monitor" { // that one leaves at once, before it writes any LED
			sc.expect = []string{"set "}
		}
		if d.name == "daemon-run-night-off" {
			sc.intended = "a bay at brightness 0 (night off) stays dark when its disk is in standby; the shell clamped the dimmed value to 1 and lit it faintly"
		}
		if d.name == "daemon-run-netdev-gateway-down" {
			sc.intended = "one silent gateway no longer turns the network LED red: it takes 3 failed probes (about 90 s) after 2 pings each; the shell turned red on the first single ping"
		}
		list = append(list, sc)
	}
	_ = fmt.Sprint
	return list
}
