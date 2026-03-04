package raft

// log Entry structure and log handing methods

// LogEntry represents a single entry in the Raft log
type LogEntry struct {
	Term    int
	Command any
	Index   int
}

// lastLog returns the last log entry
func (rf *Raft) lastLog() LogEntry {
	return rf.log[len(rf.log)-1]
}

// first log returns the first log entry
func (rf *Raft) firstLog() LogEntry {
	return rf.log[0]
}

func (rf *Raft) getEntry(index int) (LogEntry, bool) {
	rebased, ok := rf.rebase(index)
	if !ok {
		return LogEntry{}, false
	}
	return rf.log[rebased], true
}

func (rf *Raft) rebase(index int) (int, bool) {
	base := rf.firstLog().Index
	end := rf.lastLog().Index
	if index < base || index > end {
		return -1, false
	}
	return index - base, true
}

// commitEntries attempts to advance the committIndex based on matchIndex of followers
func (rf *Raft) commitEntries() {
	for idx := rf.lastLog().Index; idx > rf.commitIndex; idx-- {
		entry, ok := rf.getEntry(idx)
		if !ok {
			continue
		}
		if entry.Term != rf.currentTerm {
			return
		}

		cnt := 1
		for peer := range rf.peers {
			if peer == rf.me {
				continue
			}
			if rf.matchIndex[peer] >= idx {
				cnt++
			}
		}
		if cnt > len(rf.peers)/2 {
			rf.logf("Committed entries up to index %d.", idx)
			rf.commitIndex = idx
			rf.applyCond.Broadcast()
			break
		}
	}
}
