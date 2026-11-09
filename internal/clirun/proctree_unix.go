// SPDX-License-Identifier: GPL-3.0-or-later

//go:build unix

package clirun

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// psTimeout bounds the process table snapshot, so a stuck ps cannot hold up a
// kill.
const psTimeout = 2 * time.Second

// processTable reads the process table as a pid to ppid map. It is a variable
// so a test can replace it.
var processTable = psTable

// psTable runs `ps -axo pid=,ppid=` (it works the same on macOS and Linux). It
// runs ps directly: ps needs no process group of its own.
func psTable() (map[int]int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), psTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ps", "-axo", "pid=,ppid=").Output()
	if err != nil {
		return nil, fmt.Errorf("ps: %w", err)
	}
	return parseProcTable(string(out)), nil
}

// parseProcTable reads the text of `ps -axo pid=,ppid=` into a pid to ppid
// map. It skips blank lines and any row that is not two integers.
func parseProcTable(text string) map[int]int {
	table := map[int]int{}
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil {
			continue
		}
		table[pid] = ppid
	}
	return table
}

// descendants returns every transitive child of root in the table, sorted. It
// never returns pid 0, pid 1, the current process, its parent or root itself,
// and it does not walk below them.
func descendants(table map[int]int, root int) []int {
	children := map[int][]int{}
	for pid, ppid := range table {
		children[ppid] = append(children[ppid], pid)
	}
	never := map[int]bool{0: true, 1: true, root: true, os.Getpid(): true, os.Getppid(): true}
	var found []int
	seen := map[int]bool{}
	queue := []int{root}
	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]
		for _, pid := range children[parent] {
			if never[pid] || seen[pid] {
				continue
			}
			seen[pid] = true
			found = append(found, pid)
			queue = append(queue, pid)
		}
	}
	sort.Ints(found)
	return found
}

// Descendants returns the pids of the processes below pid in a process table
// snapshot taken now. It returns nil when the table cannot be read.
func Descendants(pid int) []int {
	table, err := processTable()
	if err != nil {
		return nil
	}
	return descendants(table, pid)
}

// KillTree sends SIGKILL to the process group of pid and to every process
// below pid that left that group. The snapshot is taken first: once the group
// is dead, the escaped children are reparented to pid 1 and cannot be found
// from pid. A process already reparented before the call is not found. When
// the table cannot be read, it kills the group only. A process that is already
// gone is not an error.
func KillTree(pid int) error {
	err := killTree(pid)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

// killTree is KillTree, except that it returns the group kill's ESRCH (the
// group is gone) when that is the only problem, so Cancel can report it.
func killTree(pid int) error {
	below := Descendants(pid)
	err := syscall.Kill(-pid, syscall.SIGKILL)
	for _, d := range below {
		derr := syscall.Kill(d, syscall.SIGKILL)
		if derr != nil && !errors.Is(derr, syscall.ESRCH) && (err == nil || errors.Is(err, syscall.ESRCH)) {
			err = derr
		}
	}
	return err
}
