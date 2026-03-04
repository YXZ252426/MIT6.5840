// Package raft implements the Raft consensus algorithm.
package raft

// raft.go contains the core Raft peer structure and methods
import (
	//	"bytes"

	"math/rand"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"6.5840/labrpc"
	"6.5840/raftapi"
	tester "6.5840/tester1"
)

// RaftState represents the role of a Raft peer
type (
	RaftState int
)

// RaftState contains the Follower, Candidate, and Leader states.
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

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {

	// Your code here (3A).
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.currentTerm, rf.state == Leader
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

// isStatebehind checks if the server's state is behind based on term and state
func (rf *Raft) isStateBehind(newTerm int, oldState RaftState, oldTerm int) bool {
	return rf.updateTerm(newTerm) || rf.state != oldState || rf.currentTerm != oldTerm
}

// Start initiates the argument on the next command to be appende to the log.
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
	rf.logf("Leader Appended new entry at index %d, %v", index, entry)
	rf.persist(nil)
	rf.appendBroadcast()
	return index, term, true
}

// applier is a long-running goroutine that applies committed log entries to the state machine.
func (rf *Raft) applier() {
	for !rf.killed() {
		rf.mu.Lock()
		for rf.lastApplied >= rf.commitIndex {
			rf.applyCond.Wait()
		}
		commitIndex := rf.commitIndex
		commit, _ := rf.rebase(commitIndex)
		applied, _ := rf.rebase(rf.lastApplied)
		entries := slices.Clone(rf.log[applied+1 : commit+1])
		rf.logf("Applying from index %d to %d, entries: %v.", applied, commit, entries)
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
			rf.logf("Heartbeat timer expired, sending heartbeats to peer %d.", i)
			go rf.appendOnce(i)
		}

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
			if rf.state != Leader {
				rf.startElection()
				rf.resetTimer(rf.electionTimer)
			}
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

// Kill stops this Raft peer.
func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
	rf.resetTimer(rf.electionTimer)
	rf.resetTimer(rf.hearbeatTimer)
	rf.mu.Lock()
	rf.applyCond.Broadcast()
	rf.mu.Unlock()
}

// Killed checks if this Raft peer has been killed
func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}

// Make creates a new Raft server
func Make(peers []*labrpc.ClientEnd, me int,
	persister *tester.Persister, applyCh chan raftapi.ApplyMsg) raftapi.Raft {
	// init Raft struct
	rf := &Raft{
		peers:       peers,
		persister:   persister,
		me:          me,
		currentTerm: 0,
		votedFor:    -1,
		commitIndex: 0,
		lastApplied: 0,
		nextIndex:   make([]int, len(peers)),
		matchIndex:  make([]int, len(peers)),
		applyCh:     applyCh,
	}
	rf.applyCond = sync.NewCond(&rf.mu)
	// dummy log entry at index 0
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
