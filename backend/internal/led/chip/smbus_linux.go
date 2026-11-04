//go:build linux

package chip

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/hw"
)

const (
	ioctlSlave      = 0x0703
	ioctlSlaveForce = 0x0706
	ioctlSMBus      = 0x0720

	smbusWrite = 0
	smbusRead  = 1

	smbusByte     = 1
	smbusByteData = 2
	smbusBlockMax = 32

	lockFile = "/var/run/ugreen-led-i2c.lock"
)

// struct i2c_smbus_ioctl_data: u8 read_write, u8 command, u32 size, union i2c_smbus_data *data.
type smbusIoctlData struct {
	readWrite uint8
	command   uint8
	size      uint32
	data      *[smbusBlockMax + 2]byte
}

type bus struct {
	path string
	file *os.File
	lock *os.File
	dw   *dwI2C // DXP4800 GT without a kernel driver: the controller is driven through /dev/mem
	gt   bool   // DXP4800 GT: SMBus block writes instead of I2C block writes
}

func ioctl(fd, request, arg uintptr) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, request, arg); errno != 0 {
		return errno
	}
	return nil
}

// findBus returns the I801 SMBus or DesignWare adapter (DXP4800 GT) the controller sits on, e.g. /dev/i2c-0.
// The adapter is listed under /sys/bus/i2c even before i2c-dev creates /dev/i2c-N.
func findBus() (string, error) {
	names, _ := filepath.Glob("/sys/bus/i2c/devices/i2c-*/name")

	for _, prefix := range []string{"SMBus I801 adapter", dwAdapter} {
		for _, name := range names {
			content, err := os.ReadFile(name)
			if err != nil || !strings.HasPrefix(string(content), prefix) {
				continue
			}

			path := "/dev/" + filepath.Base(filepath.Dir(name))
			if _, err := os.Stat(path); err != nil {
				return "", fmt.Errorf("%s is missing; load the i2c-dev module (modprobe i2c-dev)", path)
			}
			return path, nil
		}
	}

	if names, _ := dwControllers(); len(names) > 0 {
		return dwPath, nil
	}

	return "", errors.New("no SMBus I801 or DesignWare I2C adapter found; load the i2c_i801 module (modprobe i2c_i801) and check dmesg | grep -i -e i801 -e designware")
}

// acquireLock allows one writer at a time: the web UI and the status daemon share the controller. It waits at most
// hw.LockTimeout, then fails with hw.ErrLockTimeout.
func acquireLock() (*os.File, error) {
	lock, err := os.OpenFile(lockFile, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := hw.FlockExclusive(int(lock.Fd())); err != nil {
		_ = lock.Close()
		return nil, err
	}
	return lock, nil
}

func openBus(path string, force bool) (*bus, error) {
	return openBusAt(path, controllerAddr, force)
}

func openBusAt(path string, addr uintptr, force bool) (*bus, error) {
	if path == "" {
		found, err := findBus()
		if err != nil {
			return nil, err
		}
		path = found
	}

	lock, err := acquireLock()
	if err != nil {
		return nil, err
	}

	if path == dwPath {
		dw, err := openDW(uint8(addr))
		if err != nil {
			_ = lock.Close()
			return nil, err
		}
		return &bus{path: path, dw: dw, lock: lock, gt: true}, nil
	}

	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		_ = lock.Close()
		return nil, err
	}

	request := uintptr(ioctlSlave)
	if force {
		request = ioctlSlaveForce
	}

	if err := ioctl(file.Fd(), request, addr); err != nil {
		_ = file.Close()
		_ = lock.Close()
		if errors.Is(err, syscall.EBUSY) {
			return nil, fmt.Errorf("address 0x%02x on %s is used by a kernel driver (led_ugreen loaded?); unload it or pass --force", addr, path)
		}
		return nil, err
	}

	name, _ := os.ReadFile("/sys/bus/i2c/devices/" + filepath.Base(path) + "/name")
	return &bus{path: path, file: file, lock: lock, gt: strings.HasPrefix(string(name), dwAdapter)}, nil
}

func (b *bus) close() {
	if b.dw != nil {
		b.dw.close()
	} else {
		_ = b.file.Close()
	}
	_ = b.lock.Close()
}

func smbusIoctl(file *os.File, readWrite, command byte, size uint32, data *[smbusBlockMax + 2]byte) error {
	args := smbusIoctlData{readWrite: readWrite, command: command, size: size, data: data}
	err := ioctl(file.Fd(), ioctlSMBus, uintptr(unsafe.Pointer(&args))) //nolint:gosec // ioctl argument struct
	runtime.KeepAlive(data)
	return err
}

func (b *bus) smbus(readWrite, command byte, size uint32, data *[smbusBlockMax + 2]byte) error {
	if b.dw != nil {
		return b.dw.smbus(readWrite, command, size, data)
	}
	return smbusIoctl(b.file, readWrite, command, size, data)
}

func (b *bus) readBlock(command byte, length int) ([]byte, error) {
	var data [smbusBlockMax + 2]byte
	data[0] = byte(length)

	if err := b.smbus(smbusRead, command, smbusI2CBlockData, &data); err != nil {
		return nil, err
	}
	return append([]byte(nil), data[1:1+length]...), nil
}

func (b *bus) writeBlock(command byte, size uint32, payload []byte) error {
	var data [smbusBlockMax + 2]byte
	data[0] = byte(len(payload))
	copy(data[1:], payload)

	return b.smbus(smbusWrite, command, size, &data)
}

func (b *bus) readByte(command byte) (byte, error) {
	var data [smbusBlockMax + 2]byte

	if err := b.smbus(smbusRead, command, smbusByteData, &data); err != nil {
		return 0, err
	}
	return data[0], nil
}

// change sends one command and waits until the controller confirms it (see framesFor for the frame order).
func (b *bus) change(id, command byte, params [4]byte) error {
	return sendFrames(b, framesFor(b.gt), id, command, params)
}

func (b *bus) status(name string) (ledStatus, error) {
	id, err := ledID(name)
	if err != nil {
		return ledStatus{}, err
	}

	for i := range retries {
		backoff(i)

		var block []byte
		if block, err = b.readBlock(regStatusBase+id, statusLength); err != nil {
			continue
		}
		var s ledStatus
		if s, err = parseStatus(name, block); err == nil {
			return s, nil
		}
	}

	return ledStatus{}, fmt.Errorf("%s: %w", name, err)
}

// writeByteData is "i2cset -y <bus> <addr> <register> <value> b".
func (b *bus) writeByteData(register, value byte) error {
	var data [smbusBlockMax + 2]byte
	data[0] = value

	return b.smbus(smbusWrite, register, smbusByteData, &data)
}

// scanBuses lists the I2C buses to scan: the named one, or with all every /dev/i2c-N.
func scanBuses(path string, all bool) ([]string, error) {
	if all {
		paths, _ := filepath.Glob("/dev/i2c-*")
		if len(paths) == 0 {
			return nil, errors.New("no /dev/i2c-* found; load the i2c-dev module (modprobe i2c-dev)")
		}
		return paths, nil
	}
	if path == "" {
		found, err := findBus()
		if err != nil {
			return nil, err
		}
		path = found
	}
	return []string{path}, nil
}

// scanBus reads one byte from every address 0x03..0x77 (like "i2cdetect -y -r") and
// returns the ones that answer; busy are addresses a kernel driver already holds.
// It only reads, nothing is written to any device.
func scanBus(path string) (found, busy []string, err error) {
	lock, err := acquireLock()
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = lock.Close() }()

	if path == dwPath {
		return scanDW()
	}

	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = file.Close() }()

	for addr := uintptr(0x03); addr <= 0x77; addr++ {
		name := fmt.Sprintf("0x%02x", addr)
		if err := ioctl(file.Fd(), ioctlSlave, addr); err != nil {
			if errors.Is(err, syscall.EBUSY) {
				busy = append(busy, name)
			}
			continue
		}

		var data [smbusBlockMax + 2]byte
		if smbusIoctl(file, smbusRead, 0, smbusByte, &data) == nil {
			found = append(found, name)
		}
	}

	return found, busy, nil
}

func (b *bus) busPath() string { return b.path }
