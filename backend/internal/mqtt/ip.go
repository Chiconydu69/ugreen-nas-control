package mqtt

import (
	"net"
	"os"
	"path/filepath"
)

// addrsOf lists the addresses of an interface (replaced in tests).
var addrsOf = func(name string) []net.Addr {
	ifc, err := net.InterfaceByName(name)
	if err != nil {
		return nil
	}
	a, _ := ifc.Addrs()
	return a
}

func firstIPv4(addrs []net.Addr) string {
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		if ip := ipn.IP.To4(); ip != nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
			return ip.String()
		}
	}
	return ""
}

// ifaceIPv4 is the IPv4 address of a port. Unraid usually puts it on a bridge or bond (br0, bond0) that the port
// belongs to, so the port's master is tried when the port itself has none.
func ifaceIPv4(name string) string {
	if ip := firstIPv4(addrsOf(name)); ip != "" {
		return ip
	}
	if l, err := os.Readlink(filepath.Join(sysDir, "class/net", name, "master")); err == nil {
		return firstIPv4(addrsOf(filepath.Base(l)))
	}
	return ""
}
