// Copyright 2019+ Klaus Post. All rights reserved.
// License information can be found in the LICENSE file.

package zstd

import (
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"testing"
	"time"
)

var isRaceTest bool

func TestMain(m *testing.M) {
	ec := m.Run()
	if ec == 0 && runtime.NumGoroutine() > 2 {
		n := 0
		for n < 15 {
			n++
			time.Sleep(time.Second)
			if runtime.NumGoroutine() == 2 {
				os.Exit(0)
			}
		}
		fmt.Println("goroutines:", runtime.NumGoroutine())
		pprof.Lookup("goroutine").WriteTo(os.Stderr, 1)
		os.Exit(1)
	}
	os.Exit(ec)
}

func TestMatchLen(t *testing.T) {
	a := make([]byte, 130)
	for i := range a {
		a[i] = byte(i)
	}
	b := append([]byte{}, a...)

	check := func(x, y []byte, l int) {
		if m := matchLen(x, y); m != l {
			t.Error("expected", l, "got", m)
		}
	}

	for l := range a {
		a[l] = ^a[l]
		check(a, b, l)
		check(a[:l], b, l)
		a[l] = ^a[l]
	}
}
