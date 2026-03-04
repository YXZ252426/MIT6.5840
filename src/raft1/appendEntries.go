package raft

import "slices"

// appendEntries handling for Raft.

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	rf.updateTerm(args.Term)
	reply.Term = rf.currentTerm
	reply.Success = false
	reply.XTerm = -1
	reply.XIndex = -1
	reply.Xlen = rf.lastLog().Index + 1
	if args.Term < rf.currentTerm {
		rf.logf("Rejected AppendEntries from %d: sender's term %d is stale (our term is %d).", args.LeaderId, args.Term, rf.currentTerm)
		return
	}
	rf.toFollower()
	if args.PrevLogIndex > rf.lastLog().Index {
		reply.Term, reply.Success = rf.currentTerm, false
		rf.logf("Rejected AppendEntries from %d: PrevLogIndex %d is out of bounds (our log len is %d).", args.LeaderId, args.PrevLogIndex, len(rf.log))
		reply.Xlen = rf.lastLog().Index + 1
		return
	}

	idx, ok := rf.rebase(args.PrevLogIndex)
	if !ok {
		reply.Xlen = rf.firstLog().Index + 1
		return
	}
	if rf.log[idx].Term != args.PrevLogTerm {
		rf.logf("Rejected AppendEntries from %d: Term mismatch at PrevLogIndex %d.", args.LeaderId, args.PrevLogIndex)
		reply.XTerm = rf.log[idx].Term
		reply.XIndex = rf.findFirstIndexOfTerm(idx, reply.Term)
		return
	}

	reply.Success = true
	if len(args.Entries) != 0 && rf.isConflict(args) {
		rf.log = append(rf.log[:idx+1], args.Entries...)
		rf.logf("Append %d new entries from leader %d starting at index %d.", len(args.Entries), args.LeaderId, args.PrevLogIndex+1)
		rf.persist(nil)
	}
	if args.LeaderCommit > rf.commitIndex {
		rf.commitIndex = min(args.LeaderCommit, rf.lastLog().Index)
		rf.logf("Updated commitIndex to %d.", rf.commitIndex)
		rf.applyCond.Broadcast()
	}
}

func (rf *Raft) appendOnce(server int) {
	rf.mu.Lock()
	if rf.state != Leader {
		rf.mu.Unlock()
		return
	}
	index, ok := rf.rebase(rf.nextIndex[server] - 1)
	if !ok {
		rf.mu.Unlock()
		return
	}
	entries := slices.Clone(rf.log[index+1:])
	args := AppendEntriesArgs{
		Term:         rf.currentTerm,
		LeaderId:     rf.me,
		PrevLogIndex: rf.log[index].Index,
		PrevLogTerm:  rf.log[index].Term,
		Entries:      entries,
		LeaderCommit: rf.commitIndex,
	}
	rf.mu.Unlock()

	reply := AppendEntriesReply{}
	if !rf.peers[server].Call("Raft.AppendEntries", &args, &reply) {
		return
	}

	rf.mu.Lock()
	defer rf.mu.Unlock()
	if rf.isStateBehind(reply.Term, Leader, args.Term) {
		return
	}
	if reply.Success {
		rf.matchIndex[server] = args.PrevLogIndex + len(args.Entries)
		rf.nextIndex[server] = args.PrevLogIndex + len(args.Entries) + 1
		rf.commitEntries()
		return
	}

	if reply.XTerm == -1 {
		rf.nextIndex[server] = reply.Xlen
	} else {
		lastIndex := rf.findLastIndexOfTerm(reply.XTerm)
		if lastIndex == -1 {
			rf.nextIndex[server] = reply.XIndex
		} else {
			rf.nextIndex[server] = lastIndex + 1
		}
	}
}

// isConflict checks if there is a conflict between the log entries and the incoming AppendEntries RPC.
func (rf *Raft) isConflict(args *AppendEntriesArgs) bool {
	base_index := args.PrevLogIndex + 1
	for i, entry := range args.Entries {
		local, ok := rf.getEntry(i + base_index)
		if !ok {
			return true
		}
		if local.Term != entry.Term {
			return true
		}
	}
	return false
}

// findFirstIndexOfTerm finds the first index with the given term
func (rf *Raft) findFirstIndexOfTerm(idx, term int) int {
	for idx > 0 && rf.log[idx-1].Term == term {
		idx--
	}
	return rf.log[idx].Index
}

// findLastIndexOfTerm returns the highest index in rf.log whose entry has the given term.
// It returns -1 fi the term does not exist in the log
func (rf *Raft) findLastIndexOfTerm(term int) int {
	for idx := len(rf.log) - 1; idx >= 0; idx-- {
		if rf.log[idx].Term == term {
			return rf.log[idx].Index
		}
	}
	return -1
}
