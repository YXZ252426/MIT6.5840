package raft

// The file ../raftapi/raftapi.go defines the interface that raft must
// expose to servers (or the tester), but see comments below for each
// of these functions for more details.
//
// In addition,  Make() creates a new raft peer that implements the
// raft interface.

import (
	//	"bytes"
	"bytes"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"6.5840/labgob"
	"6.5840/labrpc"
	"6.5840/raftapi"
	tester "6.5840/tester1"
)

// RaftState represents the role of a Raft peer
type (
	RaftState int
)

// String makes RaftState printable for debugging.
func (s RaftState) String() string {
	switch s {
	case Follower:
		return "Follower"
	case Candidate:
		return "Candidate"
	case Leader:
		return "Leader"
	default:
		return "Unknow"
	}
}

// RaftState constants
const (
	Follower RaftState = iota
	Candidate
	Leader
)

// Timing constants
const (
	HeartbeatInterval = 100 * time.Millisecond
	ElectionInterval  = 600 * time.Second
	RPCTimeout        = 50 * time.Millisecond
	ElectionTimeout   = 300 * time.Millisecond
	ElectionJitter    = 600 * time.Millisecond
)

// electionTimeout returns a randomized election timeout duration
func (rf *Raft) electionTimeout() time.Duration {
	return ElectionTimeout + time.Duration(rand.Int63n(int64(ElectionJitter)))
}

// heartbeatTimeout return the heartbeat timeout duration.
func (rf *Raft) heartbeatTimeout() time.Duration {
	return HeartbeatInterval
}

// wrapper to reset a time safely
func (rf *Raft) resetTimer(t *time.Timer) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	switch t {
	case rf.electionTimer:
		t.Reset(rf.electionTimeout())
	case rf.hearbeatTimer:
		t.Reset(rf.heartbeatTimeout())
	default:
		panic("unknown timer!")
	}
}

// A Go object implementing a single Raft peer.
type Raft struct {
	mu        sync.Mutex          // Lock to protect shared access to this peer's state
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *tester.Persister   // Object to hold this peer's persisted state
	me        int                 // this peer's index into peers[]
	dead      int32

	applyCh   chan raftapi.ApplyMsg // channel to send ApplyMsg messages to the service
	applyCond *sync.Cond            // Condition variable to signal the applier goroutine
	// Persistent state
	state       RaftState
	currentTerm int
	votedFor    int
	log         []LogEntry

	// Volatile state on all servers
	commitIndex int
	lastApplied int

	// Volatile stats on leaders
	nextIndex  []int
	matchIndex []int

	// Timers
	electionTimer *time.Timer
	hearbeatTimer *time.Timer
}

func (rf *Raft) logf(format string, a ...interface{}) {
	prefix := fmt.Sprintf("[%d][T%d][%s] ", rf.me, rf.currentTerm, rf.state.String())
	format = prefix + format
	DPrintf(format, a...)
}

type LogEntry struct {
	Term    int
	Command any
	Index   int
}

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {

	// Your code here (3A).
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.currentTerm, rf.state == Leader
}

// lastLog returns the last log entry
func (rf *Raft) lastLog() LogEntry {
	return rf.log[len(rf.log)-1]
}

// first log returns the first log entry
func (rf *Raft) firstLog() LogEntry {
	return rf.log[0]
}

func (rf *Raft) getLogEntry(index int) (LogEntry, int) {
	base := rf.firstLog().Index
	end := rf.lastLog().Index
	if index < base || index > end {
		return LogEntry{}, -1
	}
	return rf.log[index-base], 0
}

func (rf *Raft) rebaseIndex(index int) (int, int) {
	base := rf.firstLog().Index
	end := rf.lastLog().Index
	if index < base || index > end {
		return -1, -1
	}
	return index - base, 0
}
func (rf *Raft) toLeader() {
	rf.logf("Transitioned to Leader. current logs: %v", rf.log)
	rf.state = Leader
	last := rf.lastLog().Index
	for i := range rf.peers {
		rf.nextIndex[i] = last + 1
		rf.matchIndex[i] = 0
	}
	rf.resetTimer(rf.hearbeatTimer)
	rf.electionTimer.Stop()
	rf.appendBroadcast()
}

func (rf *Raft) toCandidate() {
	rf.logf("Transitioned to Candidate.")
	rf.state = Candidate
	rf.currentTerm += 1
	rf.votedFor = rf.me
	rf.persist(nil)
}

// toFollower transitions the server to the follower state.
func (rf *Raft) toFollower() {
	if rf.state != Follower {
		rf.logf("Transitioned to follower")
		rf.state = Follower
		rf.hearbeatTimer.Stop()
	}
	rf.resetTimer(rf.electionTimer)
}

func (rf *Raft) updateTerm(newTerm int) bool {
	if newTerm > rf.currentTerm {
		rf.logf("Discovered a newer term %d (our term is %d). Transitioning to Follower.", newTerm, rf.currentTerm)
		rf.currentTerm, rf.votedFor = newTerm, -1
		rf.toFollower()
		rf.persist(nil)
		return true
	}
	return false
}

func (rf *Raft) persist(snapshot []byte) {
	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	e.Encode(rf.currentTerm)
	e.Encode(rf.votedFor)
	e.Encode(rf.log)
	raftstate := w.Bytes()
	if snapshot == nil {
		snapshot = rf.persister.ReadSnapshot()
	}
	rf.persister.Save(raftstate, snapshot)
}

// restore previously persisted state.
func (rf *Raft) readPersist(data []byte) {
	if len(data) < 1 { // bootstrap without any state?
		return
	}

	r := bytes.NewBuffer(data)
	d := labgob.NewDecoder(r)

	var currentTerm int
	var votedFor int
	var log []LogEntry

	if d.Decode(&currentTerm) != nil ||
		d.Decode(&votedFor) != nil ||
		d.Decode(&log) != nil {
		DPrintf("readPersist failed\n")
	} else {
		rf.currentTerm = currentTerm
		rf.votedFor = votedFor
		rf.log = log
	}
}

// how many bytes in Raft's persisted log?
func (rf *Raft) PersistBytes() int {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.persister.RaftStateSize()
}

// InstallSnapshotArgs is the arguments structure for InstallSnapshot RPC
type InstallSnapshotArgs struct {
	Term              int
	LeaderId          int
	LastIncludedIndex int
	LastIncludedTerm  int
	Data              []byte
}

// InstallSnapshptReply is the reply structure for InstallSnapshot RPC
type InstallSnapshotReply struct {
	Term int
}

// InstallSnapshot handles an InstallSnapshot RPC request
func (rf *Raft) InstallSnapshot(args *InstallSnapshotArgs, reply *InstallSnapshotReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	rf.updateTerm(args.Term)
	reply.Term = rf.currentTerm
	if args.Term < rf.currentTerm {
		return
	}

	rf.toFollower()
	if args.LastIncludedIndex <= rf.commitIndex {
		return
	}

	rf.logf("Installing snapshpt (laastIncludedIndex: %d, lastIncludedTerm: %d).", args.LastIncludedIndex, args.LastIncludedTerm)
	if args.LastIncludedIndex > rf.lastLog().Index {
		rf.log = make([]LogEntry, 1)
	} else {
		idx, _ := rf.rebaseIndex(args.LastIncludedIndex)
		rf.log = rf.log[idx:]
	}

	rf.log[0] = LogEntry{
		Term:    args.LastIncludedIndex,
		Index:   args.LastIncludedTerm,
		Command: nil,
	}
	rf.persist(args.Data)

	if args.LastIncludedIndex > rf.lastApplied {
		rf.lastApplied = args.LastIncludedIndex
	}
	if args.LastIncludedIndex > rf.commitIndex {
		rf.commitIndex = args.LastIncludedIndex
	}
	go func() {
		rf.applyCh <- raftapi.ApplyMsg{
			SnapshotValid: true,
			Snapshot:      args.Data,
			SnapshotTerm:  args.LastIncludedTerm,
			SnapshotIndex: args.LastIncludedIndex,
		}
	}()
}

// sendInstallSnapshot sends an InstallSnapshot RPC to server.
func (rf *Raft) sendInstallSnapshot(server int) {
	rf.mu.Lock()
	if rf.state != Leader {
		rf.mu.Unlock()
		return
	}
	args := InstallSnapshotArgs{
		Term:              rf.currentTerm,
		LeaderId:          rf.me,
		LastIncludedIndex: rf.firstLog().Index,
		LastIncludedTerm:  rf.firstLog().Term,
		Data:              rf.persister.ReadSnapshot(),
	}
	rf.logf("Sending InstallSnapshpt to peer %d (lastIncludedIndex: %d).", server, args.LastIncludedIndex)
	rf.mu.Unlock()

	reply := InstallSnapshotReply{}
	if !rf.peers[server].Call("Raft.InstallSnapshot", &args, &reply) {
		return
	}

	rf.mu.Lock()
	defer rf.mu.Unlock()
	if rf.updateTerm(reply.Term) || args.Term != rf.currentTerm || rf.state != Leader {
		return
	}
	rf.nextIndex[server] = args.LastIncludedIndex + 1
	rf.matchIndex[server] = args.LastIncludedIndex
}

// the service says it has created a snapshot that has
// all info up to and including index.
func (rf *Raft) Snapshot(index int, snapshot []byte) {
	// Your code here (3D).
	rf.mu.Unlock()
	defer rf.mu.Unlock()
	if len(snapshot) == 0 {
		return
	}

	if index < rf.firstLog().Index {
		return
	}
	ent, ok := rf.getLogEntry(index)
	if ok < 0 {
		return
	}

	from, _ := rf.rebaseIndex(index + 1)
	newlog := []LogEntry{{Term: ent.Term, Index: index, Command: nil}}
	if from >= 0 && from < len(rf.log) {
		newlog = append(newlog, rf.log[from:]...)
	}
	rf.logf("Create snapshot up to index %d. New log %v", index, newlog)
	rf.log = newlog
	rf.persist(snapshot)
}

// example RequestVote RPC arguments structure.
// field names must start with capital letters!
type RequestVoteArgs struct {
	// Your data here (3A, 3B).
	Term         int
	CandidateId  int
	LastLogIndex int
	LastLogTerm  int
}

// example RequestVote RPC reply structure.
// field names must start with capital letters!
type RequestVoteReply struct {
	// Your data here (3A).
	Term        int
	VoteGranted bool
}

// example RequestVote RPC handler.
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (3A, 3B).
	rf.mu.Lock()
	defer rf.mu.Unlock()
	rf.updateTerm(args.Term)
	reply.Term = rf.currentTerm
	reply.VoteGranted = false

	if args.Term == rf.currentTerm && (rf.votedFor == -1 || rf.votedFor == args.CandidateId) {
		lastLogIndex := rf.lastLog().Index
		lastLogTerm := rf.lastLog().Term

		granted := args.LastLogTerm > lastLogTerm ||
			(args.LastLogTerm == lastLogTerm && args.LastLogIndex >= lastLogIndex)
		rf.logf("Granted: %v vote to candidate %d. args: %v, lastLogIndex: %d, lastLogTerm: %d", granted, args.CandidateId, args, lastLogIndex, lastLogTerm)
		if granted {
			reply.VoteGranted = true
			rf.votedFor = args.CandidateId
			rf.resetTimer(rf.electionTimer)
			rf.persist(nil)
		}
	}
}

type AppendEntriesArgs struct {
	Term         int
	LeaderId     int
	PrevLogIndex int
	PrevLogTerm  int
	Entries      []LogEntry
	LeaderCommit int
}

type AppendEntriesReply struct {
	Term    int
	Success bool
	XTerm   int
	XIndex  int
	Xlen    int
}

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
	if args.PrevLogIndex >= rf.lastLog().Index {
		reply.Term, reply.Success = rf.currentTerm, false
		rf.logf("Rejected AppendEntries from %d: PrevLogIndex %d is out of bounds (our log len is %d).", args.LeaderId, args.PrevLogIndex, len(rf.log))
		reply.Xlen = rf.lastLog().Index + 1
		return
	}

	idx, err := rf.rebaseIndex(args.PrevLogIndex)
	if err < 0 {
		reply.Xlen = rf.firstLog().Index + 1
		return
	}
	if rf.log[idx].Term != args.PrevLogIndex {
		rf.logf("Rejected AppendEntries from %d: Term mismatch at PrevLogIndex %d.", args.LeaderId, args.PrevLogIndex)
		reply.XTerm = rf.log[idx].Term
		xIndex := idx
		for xIndex > 0 && rf.log[xIndex-1].Term == reply.XIndex {
			xIndex--
		}
		reply.XIndex = rf.log[xIndex].Index
		return
	}

	reply.Success = true
	if len(args.Entries) != 0 && rf.isConflict(args) {
		rf.log = rf.log[:idx+1]
		entries := append([]LogEntry(nil), args.Entries...)
		rf.log = append(rf.log, entries...)
		rf.logf("Append %d new entries from leader %d starting at index %d.", len(entries), args.LeaderId, args.PrevLogIndex+1)
		rf.persist(nil)
	}
	if args.LeaderCommit > rf.commitIndex {
		rf.commitIndex = min(args.LeaderCommit, rf.lastLog().Index)
		rf.logf("Updated commitIndex to %d.", rf.commitIndex)
		rf.applyCond.Broadcast()
	}
}

func (rf *Raft) isConflict(args *AppendEntriesArgs) bool {
	base_index := args.PrevLogIndex + 1
	for i, entry := range args.Entries {
		entry_rf, err := rf.getLogEntry(i + base_index)
		if err < 0 {
			return true
		}
		if entry_rf.Term != entry.Term {
			return true
		}
	}
	return false
}

func (rf *Raft) appendOnce(server int) {
	rf.mu.Lock()
	if rf.state != Leader {
		rf.mu.Unlock()
		return
	}
	index, err := rf.rebaseIndex(rf.nextIndex[server] - 1)
	if err < 0 {
		rf.mu.Unlock()
		return
	}
	prevIndex := rf.log[index].Index
	prevTerm := rf.log[index].Term
	entries := []LogEntry(nil)
	fromSlice, e2 := rf.rebaseIndex(rf.nextIndex[server])
	if e2 >= 0 && fromSlice < len(rf.log) {
		entries = append([]LogEntry(nil), rf.log[fromSlice:]...)
	}
	args := AppendEntriesArgs{
		Term:         rf.currentTerm,
		LeaderId:     rf.me,
		PrevLogIndex: prevIndex,
		PrevLogTerm:  prevTerm,
		Entries:      entries,
		LeaderCommit: rf.commitIndex,
	}
	rf.logf("Sending AppendEntries to peer %d (entries: %d, prevLogIndex: %v).", server, entries, prevIndex)
	rf.mu.Unlock()

	reply := AppendEntriesReply{}
	if !rf.peers[server].Call("Raft.AppendEntries", &args, &reply) {
		return
	}

	rf.mu.Lock()
	if rf.updateTerm(reply.Term) || args.Term != rf.currentTerm || rf.state != Leader {
		rf.mu.Unlock()
		return
	}
	if reply.Success {
		matchIdx := args.PrevLogIndex + len(args.Entries)
		if matchIdx > rf.matchIndex[server] {
			rf.matchIndex[server] = args.PrevLogIndex + len(args.Entries)
			rf.nextIndex[server] = args.PrevLogIndex + len(args.Entries) + 1
			rf.updateCommitIndexLocked()
		}
		rf.mu.Unlock()
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
	rf.mu.Unlock()

}

func (rf *Raft) appendBroadcast() {
	rf.logf("Heartbeat Timer expired, sending heatbeats")
	for i := range rf.peers {
		if i == rf.me {
			continue
		}
		if rf.nextIndex[i] <= rf.firstLog().Index {
			rf.logf("Sending InstallSnapshot to peer %d (nextIndex: %d, firstLog: %d).", i, rf.nextIndex[i], rf.firstLog().Index)
			go rf.sendInstallSnapshot(i)
		} else {
			go rf.appendOnce(i)
		}

	}
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

func (rf *Raft) updateCommitIndexLocked() {
	for idx := rf.lastLog().Index; idx > rf.commitIndex; idx-- {
		entry, err := rf.getLogEntry(idx)
		if err < 0 {
			continue
		}
		if entry.Term != rf.currentTerm {
			continue
		}
		count := 1
		for i := range rf.peers {
			if i == rf.me {
				continue
			}
			if rf.matchIndex[i] >= idx {
				count++
			}
		}
		if count > len(rf.peers)/2 {
			rf.commitIndex = idx
			rf.logf("Log entrt committed. Updating committed index to %d.", rf.commitIndex)
			rf.applyCond.Broadcast()
			break
		}
	}
}

// the service using Raft (e.g. a k/v server) wants to start
// agreement on the next command to be appended to Raft's log. if this
// server isn't the leader, returns false. otherwise start the
// agreement and return immediately. there is no guarantee that this
// command will ever be committed to the Raft log, since the leader
// may fail or lose an election.
//
// the first return value is the index that the command will appear at
// if it's ever committed. the second return value is the current
// term. the third return value is true if this server believes it is
// the leader.
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if rf.state != Leader {
		return -1, -1, false
	}

	term := rf.currentTerm
	index := rf.lastLog().Index + 1
	entry := LogEntry{
		Term:    term,
		Command: command,
		Index:   index,
	}
	rf.log = append(rf.log, entry)
	rf.matchIndex[rf.me] = index
	rf.nextIndex[rf.me] = index + 1
	rf.persist(nil)
	rf.appendBroadcast()
	return index, term, true
}

// the tester doesn't halt goroutines created by Raft after each test,
// but it does call the Kill() method. your code can use killed() to
// check whether Kill() has been called. the use of atomic avoids the
// need for a lock.
//
// the issue is that long-running goroutines use memory and may chew
// up CPU time, perhaps causing later tests to fail and generating
// confusing debug output. any goroutine with a long-running loop
// should call killed() to check whether it should stop.
func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
	if rf.electionTimer != nil {
		rf.resetTimer(rf.electionTimer)
	}
	if rf.hearbeatTimer != nil {
		rf.resetTimer(rf.hearbeatTimer)
	}
	rf.mu.Lock()
	rf.applyCond.Broadcast()
	rf.mu.Unlock()
	// Your code here, if desired.
}

func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}

// startElection initiates a new election
func (rf *Raft) startElection() {
	rf.toCandidate()

	votes := 1
	entry := rf.lastLog()
	rf.logf("Election timer expired, starting new election. current log: %v", rf.log)
	// ?? no lock??
	args := RequestVoteArgs{
		Term:         rf.currentTerm,
		CandidateId:  rf.me,
		LastLogIndex: entry.Index,
		LastLogTerm:  entry.Term,
	}

	for peer := range rf.peers {
		if peer == rf.me {
			continue
		}
		go func(server int) {
			var reply RequestVoteReply
			if !rf.peers[server].Call("Raft.RequestVote", &args, &reply) {
				return
			}
			rf.mu.Lock()
			defer rf.mu.Unlock()
			if rf.state != Candidate || rf.currentTerm != args.Term {
				return
			}
			if rf.updateTerm(reply.Term) {
				return
			}
			if reply.VoteGranted {
				rf.logf("Receive vote from %d.", server)
				votes++
				if votes > len(rf.peers)/2 {
					rf.logf("Election won with %d votes", votes)
					rf.toLeader()
				}
			}
		}(peer)
	}
}

// ticker is a long-running goroutine that checks for election timeouts
// and sends heartbeats if this server is the leader
func (rf *Raft) ticker() {
	// Your code here (3A)
	// Check if a leader election should be started.
	for !rf.killed() {
		select {
		case <-rf.electionTimer.C:
			rf.mu.Lock()
			rf.startElection()
			rf.resetTimer(rf.electionTimer)
			rf.mu.Unlock()
		case <-rf.hearbeatTimer.C:
			rf.mu.Lock()
			if rf.state == Leader {
				rf.appendBroadcast()
				rf.resetTimer(rf.hearbeatTimer)
			}
			rf.mu.Unlock()
		}
	}
}

// applier is a long-running goroutine that applies committed log entries to the state machine.
func (rf *Raft) applier() {
	for !rf.killed() {
		rf.mu.Lock()
		for rf.lastApplied >= rf.commitIndex {
			rf.applyCond.Wait()
		}
		commitIndex := rf.commitIndex
		commit, _ := rf.rebaseIndex(commitIndex)
		applied, _ := rf.rebaseIndex(rf.lastApplied)
		entries := make([]LogEntry, commit-applied)
		copy(entries, rf.log[applied+1:commit+1])
		rf.logf("Applying %d from index %d to %d.", entries, applied, commit)
		rf.mu.Unlock()

		for _, entry := range entries {
			rf.applyCh <- raftapi.ApplyMsg{
				CommandValid: true,
				Command:      entry.Command,
				CommandIndex: entry.Index,
			}
		}
		rf.mu.Lock()
		rf.lastApplied = max(rf.lastApplied, commitIndex)
		rf.mu.Unlock()
	}
}

// Make creates a new Raft server
func Make(peers []*labrpc.ClientEnd, me int,
	persister *tester.Persister, applyCh chan raftapi.ApplyMsg) raftapi.Raft {
	// init Raft struct
	rf := &Raft{
		peers:       peers,
		persister:   persister,
		me:          me,
		votedFor:    -1,
		commitIndex: 0,
		lastApplied: 0,
		nextIndex:   make([]int, len(peers)),
		matchIndex:  make([]int, len(peers)),
	}
	rf.applyCond = sync.NewCond(&rf.mu)
	rf.log = append(rf.log, LogEntry{0, nil, 0})
	for i := range rf.nextIndex {
		rf.nextIndex[i] = 1
	}
	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())
	// initialize timers
	rf.electionTimer = time.NewTimer(rf.electionTimeout())
	rf.hearbeatTimer = time.NewTimer(rf.heartbeatTimeout())

	// start ticker goroutine to start elections
	go rf.ticker()
	// start applier goroutine to app;y committed entries
	go rf.applier()

	return rf
}
