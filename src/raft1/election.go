package raft

// the elecion logic for Raft

// startElection initiates a new election
func (rf *Raft) startElection() {
	rf.logf("Election timer expired, starting new election.")
	rf.toCandidate()
	votes := 1
	// ?? no lock??
	args := RequestVoteArgs{
		Term:         rf.currentTerm,
		CandidateId:  rf.me,
		LastLogIndex: rf.lastLog().Index,
		LastLogTerm:  rf.lastLog().Term,
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
			if rf.isStateBehind(reply.Term, Candidate, args.Term) {
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

// RequestVote handles incoming RequestVote RPCs.
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
