//go:build unix

package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type ownedUnixProcess struct {
	pid, parent, group int
	started, state     string
}

// Remember descendants while their ancestry is still visible. Playwright's
// webServer and browsers can create their own sessions, escaping kill(-leader).
// Birth time rejects reused PIDs with a different start time (ps resolves seconds).
// This is observation, not kernel containment: a process that detaches and is
// reparented entirely between snapshots cannot be discovered this way.
type unixProcessTree struct {
	leader     int
	owned      map[int]string
	terminated map[int]string
	live       []ownedUnixProcess
	err        error
}

func newUnixProcessTree(pid int) *unixProcessTree {
	return &unixProcessTree{leader: pid, owned: make(map[int]string), terminated: make(map[int]string)}
}

// Required snapshots gate startup and cleanup. Retry a transient ps failure,
// but never accept successful completion without fresh ownership evidence.
func (tree *unixProcessTree) refreshRequired() {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if err = tree.refresh(); err == nil {
			return
		}
		if attempt < 2 {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if tree.err == nil {
		tree.err = err
	}
}

// Periodic observations are best effort. Keep remembered ownership across a
// missed observation, but clear live identities until they can be rechecked.
func (tree *unixProcessTree) refresh() error {
	table, err := unixProcessTable()
	if err != nil {
		// Never signal a remembered PID whose identity could not be rechecked.
		tree.live = nil
		return err
	}
	for pid, birth := range tree.owned {
		if current, ok := table[pid]; !ok || current.started != birth {
			delete(tree.owned, pid)
		}
	}
	for _, process := range table {
		if process.group == tree.leader {
			tree.owned[process.pid] = process.started
		}
	}
	for changed := true; changed; {
		changed = false
		for _, process := range table {
			if _, exists := tree.owned[process.pid]; exists {
				continue
			}
			if _, ownedParent := tree.owned[process.parent]; ownedParent {
				tree.owned[process.pid] = process.started
				changed = true
			}
		}
	}
	tree.live = nil
	for pid := range tree.owned {
		process := table[pid]
		if !strings.HasPrefix(process.state, "Z") {
			tree.live = append(tree.live, process)
		}
	}
	return nil
}

func (tree *unixProcessTree) signal(signal syscall.Signal) bool {
	sent := false
	for _, process := range tree.live {
		if signal == syscall.SIGTERM && tree.terminated[process.pid] == process.started {
			continue
		}
		// The original group is signaled separately, including unobserved children.
		// Signal detached descendants individually: their new group may also
		// contain processes for which we have no ownership evidence.
		if process.group != tree.leader && syscall.Kill(process.pid, signal) == nil {
			sent = true
			if signal == syscall.SIGTERM {
				tree.terminated[process.pid] = process.started
			}
		}
	}
	return sent
}

func unixProcessTable() (map[int]ownedUnixProcess, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// Only numeric relationships, status, and birth time. No arguments, command
	// output, environment, executable paths, or credentials are inspected.
	command := exec.CommandContext(ctx, "ps", "-A", "-o", "pid=,ppid=,pgid=,stat=,lstart=")
	// A PATH wrapper may exit while its children retain stdout. Bound pipe
	// drain too: the context deadline alone only terminates the wrapper.
	command.WaitDelay = 50 * time.Millisecond
	command.Env = append(os.Environ(), "LC_ALL=C")
	output := newLimitedBuffer(4 << 20)
	command.Stdout = output
	if err := command.Run(); err != nil {
		// A missing ps is a supervision failure, not a missing test/helper binary.
		return nil, fmt.Errorf("inspect owned process ancestry: %v", err)
	}
	if output.truncated {
		return nil, fmt.Errorf("owned process ancestry snapshot exceeds limit")
	}
	table := make(map[int]ownedUnixProcess)
	for _, line := range strings.Split(strings.TrimSpace(string(output.Bytes())), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 9 { // PID, PPID, PGID, state, and five lstart fields
			return nil, fmt.Errorf("invalid owned process ancestry row")
		}
		pid, pidErr := strconv.Atoi(fields[0])
		parent, parentErr := strconv.Atoi(fields[1])
		group, groupErr := strconv.Atoi(fields[2])
		if pidErr != nil || parentErr != nil || groupErr != nil || pid <= 0 {
			return nil, fmt.Errorf("invalid owned process ancestry identity")
		}
		table[pid] = ownedUnixProcess{pid, parent, group, strings.Join(fields[4:], " "), fields[3]}
	}
	return table, nil
}
