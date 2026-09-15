//go:build linux && arm64 && !appengine && !noasm && gc

package zstd

import (
	"os"
	"strconv"
	"strings"
)

// The two-pass decode wins on real data only on the Neoverse N1 (Graviton2,
// Ampere Altra), whose core exposes far-source misses from 128 KiB; the
// wider V1, V2 and V3 cores hide those and lose 5-15% on the same data, and
// gain only from 1 MiB. Linux exposes the core's MIDR register in sysfs.
func init() {
	if b, err := os.ReadFile("/sys/devices/system/cpu/cpu0/regs/identification/midr_el1"); err == nil && isNeoverseN1(string(b)) {
		twoPassFarCode = 17
	}
}

// isNeoverseN1 reports whether a MIDR_EL1 value names an Arm (0x41)
// Neoverse N1 (part 0xd0c).
func isNeoverseN1(midr string) bool {
	v, err := strconv.ParseUint(strings.TrimPrefix(strings.TrimSpace(midr), "0x"), 16, 64)
	if err != nil {
		return false
	}
	return v>>24&0xff == 0x41 && v>>4&0xfff == 0xd0c
}
