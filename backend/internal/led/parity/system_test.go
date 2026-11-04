package parity

import (
	"fmt"
	"strings"
)

// bayPorts is the SATA port of each bay of the DXP6800 Pro (the model of most scenarios).
var bayPortsDXP6800 = []int{3, 4, 5, 6, 1, 2}

const smartTable = `smartctl 7.4 2023-08-01 r5530 [x86_64-linux-6.1.64-Unraid] (local build)

SMART Attributes Data Structure revision number: 16
Vendor Specific SMART Attributes with Thresholds:
ID# ATTRIBUTE_NAME          FLAG     VALUE WORST THRESH TYPE      UPDATED  WHEN_FAILED RAW_VALUE
  1 Raw_Read_Error_Rate     0x000f   083   064   044    Pre-fail  Always       -       204726792
  5 Reallocated_Sector_Ct   0x0033   100   100   010    Pre-fail  Always       -       %s
  9 Power_On_Hours          0x0032   085   085   000    Old_age   Always       -       13219 (21 14 0)
187 Reported_Uncorrect      0x0032   100   100   000    Old_age   Always       -       0
188 Command_Timeout         0x0032   100   100   000    Old_age   Always       -       0 0 0
197 Current_Pending_Sector  0x0012   100   100   000    Old_age   Always       -       %s
198 Offline_Uncorrectable   0x0010   100   100   000    Old_age   Offline      -       0
`

func diskSection(name, dev, id, status, spundown, temp, errs string) string {
	return fmt.Sprintf("[%s]\nname=\"%s\"\ndevice=\"%s\"\nid=\"%s\"\nstatus=\"%s\"\nspundown=\"%s\"\ntemp=\"%s\"\nnumErrors=\"%s\"\nrotational=\"1\"\nwarning=\"\"\ncritical=\"\"\n", name, name, dev, id, status, spundown, temp, errs)
}

// defaultSystem lays out a DXP6800 Pro: six SATA disks behind ata1..ata6, a gigabit
// link, no saved settings, an idle array.
func (r *rig) defaultSystem() {
	for n := 1; n <= 6; n++ {
		dev := "sd" + string(rune('a'+n-1))
		pci := "sys/devices/pci0000:00/0000:00:17.0"
		leaf := fmt.Sprintf("%s/ata%d/host%d/target%d:0:0/%d:0:0:0", pci, n, n-1, n-1, n-1)
		r.mkdir(leaf)
		r.symlink("sys/block/"+dev+"/device", "../../"+strings.TrimPrefix(leaf, "sys/"))
		r.symlink(fmt.Sprintf("sys/class/ata_port/ata%d", n), fmt.Sprintf("../../devices/pci0000:00/0000:00:17.0/ata%d", n))
		mm := fmt.Sprintf("8:%d", 16*(n-1))
		r.write("sys/block/"+dev+"/dev", mm+"\n")
		r.write("sys/block/"+dev+"/stat", "100 0 800 5 50 0 400 5 0 0 0 0\n")
		r.write("udev/b"+mm, fmt.Sprintf("S:disk/by-id/ata-X\nE:ID_MODEL=ST4000\nE:ID_SERIAL_SHORT=SN%d\nE:ID_BUS=ata\n", n))
	}
	r.write("sys/bus/pci/devices/0000:00:17.0/vendor", "0x8086\n")

	r.write("disks.ini", diskSection("parity", "sda", "ST4000_SN1", "DISK_OK", "0", "34", "0")+
		diskSection("disk1", "sdb", "ST4000_SN2", "DISK_OK", "0", "41", "0")+
		diskSection("disk2", "sdc", "ST4000_SN3", "DISK_OK", "1", "*", "0")+
		diskSection("disk3", "sdd", "ST4000_SN4", "DISK_DSBL", "0", "44", "3")+
		diskSection("cache", "sde", "ST4000_SN5", "DISK_OK", "0", "50", "0"))
	r.write("var.ini", "mdState=\"STARTED\"\nmdResync=\"0\"\nmdResyncPos=\"0\"\nmdResyncSize=\"0\"\nmdResyncAction=\"check P\"\n")
	r.write("smart/disk1", fmt.Sprintf(smartTable, "0", "0"))
	r.write("smart/disk2", fmt.Sprintf(smartTable, "8", "0"))
	r.write("dynamix.cfg", "[display]\nhot=\"45\"\nmax=\"55\"\nhotssd=\"60\"\nmaxssd=\"70\"\n[notify]\npath=\""+r.path("notify")+"\"\n")

	// Network: eth0 up with a gateway, eth1 without a cable, plus things that are no port.
	for _, n := range []struct{ name, carrier, speed string }{{"eth0", "1", "1000"}, {"eth1", "0", "-1"}} {
		r.write("sys/class/net/"+n.name+"/operstate", map[string]string{"1": "up", "0": "down"}[n.carrier]+"\n")
		r.write("sys/class/net/"+n.name+"/carrier", n.carrier+"\n")
		r.write("sys/class/net/"+n.name+"/speed", n.speed+"\n")
		r.write("sys/class/net/"+n.name+"/statistics/rx_bytes", "1000\n")
		r.write("sys/class/net/"+n.name+"/statistics/tx_bytes", "2000\n")
		r.mkdir("sys/class/net/" + n.name + "/device")
	}
	r.mkdir("sys/class/net/lo")
	r.mkdir("sys/class/net/docker0")
	r.write("stubs/ip.out", "default via 192.168.1.1 dev eth0 proto dhcp src 192.168.1.5 metric 1\n")
	r.write("stubs/ethtool.eth0", `Settings for eth0:
	Supported ports: [ TP ]
	Supported link modes:   10baseT/Half 10baseT/Full
	                        100baseT/Half 100baseT/Full
	                        1000baseT/Full
	                        2500baseT/Full
	Supported pause frame use: Symmetric
	Supports auto-negotiation: Yes
`)
}

func (r *rig) config(kv ...string) {
	var sb strings.Builder
	for i := 0; i+1 < len(kv); i += 2 {
		fmt.Fprintf(&sb, "%s=\"%s\"\n", kv[i], kv[i+1])
	}
	r.write("boot/leds.cfg", sb.String())
}

func (r *rig) notification(name, importance string) {
	r.write("notify/unread/"+name+".notify", fmt.Sprintf("timestamp=\"1700000000\"\nevent=\"x\"\nimportance=\"%s\"\n", importance))
}
