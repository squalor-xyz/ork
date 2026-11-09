// SPDX-License-Identifier: GPL-3.0-or-later

//go:build unix

package clirun

import (
	"os"
	"reflect"
	"testing"
)

func TestParseProcTable(t *testing.T) {
	text := "  PID  \n" + // malformed: not numbers
		"    1     0\n" +
		"  100     1\n" +
		"\n" +
		"  101   100\n" +
		"12345 100\n" +
		"abc 7\n" +
		"200\n" + // a single column
		"300 x\n"
	got := parseProcTable(text)
	want := map[int]int{1: 0, 100: 1, 101: 100, 12345: 100}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseProcTable = %v, want %v", got, want)
	}
}

func TestDescendantsFollowsParentsTransitively(t *testing.T) {
	table := map[int]int{
		101: 100, // child
		102: 101, // grandchild
		103: 102, // great-grandchild
		104: 100, // second child
		200: 1,   // unrelated
		201: 200, // its child
	}
	got := descendants(table, 100)
	want := []int{101, 102, 103, 104}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("descendants = %v, want %v", got, want)
	}
}

func TestDescendantsNeverReturnsOneOrSelf(t *testing.T) {
	self, parent := os.Getpid(), os.Getppid()
	table := map[int]int{
		1:      100,
		0:      100,
		self:   100,
		parent: 100,
		300:    100,
		100:    300, // a cycle back to the root
		400:    1,   // below pid 1, so never reached through it
		401:    self,
		402:    parent,
	}
	got := descendants(table, 100)
	want := []int{300}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("descendants = %v, want %v", got, want)
	}
}

func TestDescendantsOfAnUnknownPidIsEmpty(t *testing.T) {
	table := map[int]int{101: 100, 102: 101}
	if got := descendants(table, 999); len(got) != 0 {
		t.Fatalf("descendants of an unknown pid = %v", got)
	}
	if got := descendants(nil, 100); len(got) != 0 {
		t.Fatalf("descendants of a nil table = %v", got)
	}
}
