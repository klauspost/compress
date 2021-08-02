// Copyright 2019+ Klaus Post. All rights reserved.
// License information can be found in the LICENSE file.

// +build race

package zstd

func init() {
	isRaceTest = true
}
