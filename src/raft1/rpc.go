package raft

// RPC argument and reply structures

// RequestVoteArgs: the argument structure for RequestVote RPCs.
type RequestVoteArgs struct {
	// Your data here (3A, 3B).
	Term         int
	CandidateId  int
	LastLogIndex int
	LastLogTerm  int
}

// RequestVoteReply: the reply structure for RequestVote RPCs.
type RequestVoteReply struct {
	// Your data here (3A).
	Term        int
	VoteGranted bool
}

// AppendEntriesArgs: the argument structure for AppendEntries RPCs.
type AppendEntriesArgs struct {
	Term         int
	LeaderId     int
	PrevLogIndex int
	PrevLogTerm  int
	Entries      []LogEntry
	LeaderCommit int
}

// AppendEntriesReply: the reply structure for AppendEntries RPCs.
type AppendEntriesReply struct {
	Term    int
	Success bool
	XTerm   int
	XIndex  int
	Xlen    int
}

// InstallSnapshotArgs: the arguments structure for InstallSnapshot RPCs.
type InstallSnapshotArgs struct {
	Term              int
	LeaderId          int
	LastIncludedIndex int
	LastIncludedTerm  int
	Data              []byte
}

// InstallSnapshptReply: the reply structure for InstallSnapshot RPCs.
type InstallSnapshotReply struct {
	Term int
}
