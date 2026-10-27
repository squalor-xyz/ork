// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"regexp"
	"strings"
	"sync"
)

// labelWords splits an id or role into its words: "-", "_", "," and "/" between
// them do not matter.
var labelWords = strings.NewReplacer("-", " ", "_", " ", ",", " ", "/", " ")

// TaskRepeatsLabel reports whether a step task is only the label the relay
// contract asks for, "<id> <role>" (or the id or the role alone, the ids of a
// run on several slices joined), which the Running row already says. Case,
// spacing and -, _, , and / between the words do not matter.
func TaskRepeatsLabel(task string, items []string, role string) bool {
	norm := func(s string) string {
		return strings.Join(strings.Fields(labelWords.Replace(strings.ToLower(s))), " ")
	}
	task, role = norm(task), norm(role)
	if task == "" {
		return false
	}
	ids := append([]string{norm(strings.Join(items, " "))}, items...)
	if task == role {
		return true
	}
	for _, id := range ids {
		id = norm(id)
		if id != "" && (task == id || task == id+" "+role) {
			return true
		}
	}
	return false
}

// StripTaskLabel cuts the leading "<id> <role>" label off a step task, with the
// ":", "-" and spaces after it, as the Running row says it already; "round 2: x"
// is left of "s41 gate round 2: x". A task that is only the label (or the id or
// the role alone) is "". Anything else is returned as it is. The Running row
// and ork status share it (S150).
func StripTaskLabel(task string, items []string, role string) string {
	if TaskRepeatsLabel(task, items, role) {
		return ""
	}
	ids := append([]string{strings.Join(items, " ")}, items...)
	for _, id := range ids {
		words := append(strings.Fields(labelWords.Replace(id)), strings.Fields(labelWords.Replace(role))...)
		if len(words) == 0 || id == "" {
			continue
		}
		if loc := labelPattern(words).FindStringIndex(task); loc != nil {
			return strings.TrimLeft(task[loc[1]:], " :-")
		}
	}
	return task
}

// labelPatterns holds the compiled pattern of each word list; a render asks
// for the same few on every frame, so each is compiled once.
var labelPatterns sync.Map // string -> *regexp.Regexp

// labelPattern is the pattern that matches those words at the start of a task:
// in order, apart by one run of space, _, , / or -, and followed by a
// separator (":" too) or the end.
func labelPattern(words []string) *regexp.Regexp {
	key := strings.Join(words, "\x00")
	if re, ok := labelPatterns.Load(key); ok {
		return re.(*regexp.Regexp)
	}
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = regexp.QuoteMeta(w)
	}
	re := regexp.MustCompile(`(?i)^\s*` + strings.Join(quoted, `[\s_,/-]+`) + `(?:[\s:_,/-]+|$)`)
	actual, _ := labelPatterns.LoadOrStore(key, re)
	return actual.(*regexp.Regexp)
}
