package sysinfo

import "golang.org/x/sys/unix"

// TotalRAM returns physical memory in bytes, 0 if unknown.
func TotalRAM() uint64 {
	mem, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return 0
	}
	return mem
}
