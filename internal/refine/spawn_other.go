//go:build !windows

package refine

import "os/exec"

func hideConsole(*exec.Cmd) {}
