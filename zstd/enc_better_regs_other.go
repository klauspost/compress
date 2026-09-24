// Copyright 2026+ Klaus Post. All rights reserved.
// License information can be found in the LICENSE file.

//go:build !arm64

package zstd

// betterPrimesInRegs: amd64 builds each prime in one instruction and spills if they are held.
const betterPrimesInRegs = false
