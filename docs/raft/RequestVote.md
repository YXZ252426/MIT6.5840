 这段 RequestVote 的作用是：收到候选人拉票请求后，判断“要不要把票投给它”。

  逻辑拆解

  1. rf.updateTerm(args.Term)
      - 先用请求里的 Term 更新自己任期（如果对方任期更大，自己要“认新任期”，通常会转 follower、清空投票状态等）。
  2. 加锁 rf.mu.Lock()
      - 下面会读写 currentTerm / VotedFor / log，需要互斥。
  3. 默认拒绝
      - reply.Term = rf.currentTerm
      - reply.VoteGranted = false
  4. 满足这两个前提才继续看日志
      - args.Term == rf.currentTerm：候选人任期必须等于当前任期（过期任期直接拒绝）。
      - rf.VotedFor == -1：本任期还没投过票。
  5. 比较日志新旧（Raft 核心规则）
      - 先比最后一条日志的 Term，谁大谁更新。
      - 如果最后日志 Term 相同，再比 Index，谁大谁更新。
      - 代码里是：
          - args.LastLogTerm > lastLogTerm，或
          - args.LastLogTerm == lastLogTerm && args.LastLogIndex >= lastLogIndex
  6. 如果候选人日志不落后，就投票
      - rf.VotedFor = args.CandidateID
      - reply.VoteGranted = true

  ———

  例子 1：会投票

  - 本地：currentTerm=5, VotedFor=-1, lastLog=(term=4, index=10)
  - 候选人请求：Term=5, LastLog=(term=4, index=12)
  - 判断：任期匹配、没投过票、日志更新（同 term 但 index 更大）
  - 结果：VoteGranted=true

  例子 2：不会投票（日志太旧）

  - 本地：currentTerm=5, VotedFor=-1, lastLog=(term=5, index=8)
  - 候选人请求：Term=5, LastLog=(term=4, index=20)
  - 判断：候选人最后日志 term 更小（4 < 5），即使 index 大也算旧
  - 结果：VoteGranted=false

  例子 3：不会投票（本任期已投票）

  - 本地：currentTerm=5, VotedFor=2
  - 候选人 3 请求：Term=5, ...
  - 判断：VotedFor != -1
  - 结果：VoteGranted=false

  ———

  补充一点：严格按 Raft，若 VotedFor 已经是同一个候选人（重复 RPC），通常也应返回 true（幂等）。你这段代码只允许 VotedFor==-1，可能会拒绝重复请求。

   Raft 里，投票是按 term 计数的：每个节点在一个 term 内最多投一票，而且只能给“当前 term”的候选人。

  你这段里先 updateTerm(args.Term)，之后再判断 args.Term == rf.currentTerm，含义是：

  1. args.Term < currentTerm：旧任期请求，必须拒绝。
  2. args.Term > currentTerm：先把自己升级到新任期（变 follower、清空投票），然后两者就会相等，再进入投票逻辑。
  3. 所以最终只会对“当前任期”的请求投票，符合论文规则。