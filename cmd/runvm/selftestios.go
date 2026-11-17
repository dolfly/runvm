package main

import (
	"context"
	"fmt"

	"github.com/dolfly/runvm/internal/ios"
	"github.com/dolfly/runvm/internal/ios/iosprobe"
)

// selftestIOS is the iOS half of selftest: boot the embedded Mach-O arm64
// probe through the Darwin personality and call its only export.
//
// It lives in the selftest command rather than only in doctor because a
// Mach-O that no longer boots is otherwise invisible until someone tries
// ios_open on a real dylib; the probe costs one engine start.
//
// Returns an exit code: 0 pass, 1 fail, 2 skipped (no CPU engine, which CI's
// platform jobs hit on machines without libunicorn).
func selftestIOS() int {
	path, cleanup, err := iosprobe.Write()
	if err != nil {
		fmt.Printf("✗ ios     write probe: %v\n", err)
		return 1
	}
	defer cleanup()

	sess, err := ios.Open(path, ios.OpenOptions{Pool: 1})
	if err != nil {
		fmt.Printf("✗ ios     open: %v\n", err)
		return 1
	}
	defer sess.Close()

	res, err := sess.Call(context.Background(), "add", []ios.Arg{
		{Kind: ios.ArgWord, Word: 3},
		{Kind: ios.ArgWord, Word: 4},
	})
	if err != nil {
		fmt.Printf("✗ ios     call add: %v\n", err)
		return 1
	}
	if res.Value != iosprobe.Probe {
		fmt.Printf("✗ ios     add(3,4) = %d (%s), want %d\n", res.Value, ios.Hex(res.Value), iosprobe.Probe)
		return 1
	}
	fmt.Printf("✓ ios     Mach-O arm64 dylib, add(3,4)=%d, engine %s, no bionic and no JNI\n",
		res.Value, sess.Engine())
	return 0
}
