这次更新有几个比较有意思改动？

首先是把选举计时器内置在结构体内

其次是取消了heartbeat CH

不需要通过管道传递信息，而是通过Candidate逻辑判断退出

由于日志的加入,toLeader函数会比upgrade函数承载更加丰富的语义

The Raft paper views the log as 1-indexed, but we suggest that you implement it as 0-indexed, starting with a dummy entry at index=0 that has term 0. That allows the very first AppendEntries RPC to contain 0 as PrevLogIndex, and be a valid index into the log.

### 2.27
fix(raft): Complete log replicate

原先的upgrade， downgrade函数不好，用toLeader等更好的体现出状态的变化
election timeout 只有在“看到活跃 leader”或“你刚把票投给某候选人”时才应刷新。

term概念再深化，term就是任期，每个任期只能有一个leader，成为候选人后自增Term，评选下个任期的leader
在变成follower的时候似乎不需要状态清理？

代码里有非常多的防御性编程，主要用于server的角色变换防范
但是这个粒度要怎么把握
### 3.1
refactor(raft): refactor Recursive appendonce to loop

这里有些变动去掉了锁，是因为上层已经加锁了，这就让我困惑，怎么把握锁的粒度

像这种变化是非常重要的，他把直接对数组的访问封装成函数，会更加安全
```golang
lastLogIndex := len(rf.log) - 1
lastLogTerm := rf.log[lastLogIndex].Term
```
```golang
lastLogIndex := rf.lastLog().Index
lastLogTerm := rf.lastLog().Term
```

“已经投过票还会再次回应”是 Raft 预期行为，不是重复投票。

  关键点：

  1. 每个 RequestVote RPC 都必须回复
     候选人会重试（丢包、超时、重复消息很常见）。如果 follower 不再回复，候选人可能拿不到这张已经投出的票，选举会卡住。
  2. rf.votedFor == args.CandidateId 表示：
     “这个 term 我已经投给你了”，所以可以再次返回 VoteGranted=true，这只是重复确认同一票，不是第二次投给别人。
  3. Raft 的限制是“每个 term 最多投给一个 candidate”，不是“只回复一次”。

  这是 Raft 论文里定义的最小“必须持久化状态”（不含快照）：

  - currentTerm
  - votedFor
  - log[]

  原因是它们直接关系到安全性，重启后不能丢：

  1. currentTerm
     防止节点重启后“回到旧任期”，错误地接受旧 leader / 旧投票请求。
  2. votedFor
     保证“一个 term 只投一票”。
     如果崩溃后忘了它，可能在同一 term 给不同候选人投票，破坏 Election Safety。
  3. log
     这是复制状态机的历史。丢了就可能把已提交命令弄没，破坏一致性。

  为什么“只要这些就能恢复”：

  - commitIndex、lastApplied、nextIndex、matchIndex 都是易失状态。
  - 它们可以在重启后通过协议重新得到：
      - follower 从 leader 的 AppendEntries(leaderCommit) 重新获知提交进度；
      - leader 重新初始化 nextIndex/matchIndex。
  - 角色（leader/candidate/follower）也可默认从 follower 开始重新选举，不需要持久化。

  初步感觉；在角色转化，纪元转化这些关键节点需要进行持久化

  你这个判断只在一个前提下成立: timer 还在正常倒计时且一定会再次触发。
  这个前提在 Raft 里不总成立。

  关键反例：

  1. timer 可能被 Stop() 了
     例如 Leader 会 Stop(electionTimer)，Follower 会 Stop(heartbeatTimer)。
     被 stop 的 timer 原始唤醒时间不是“更早”，而是“不会再唤醒（∞）”。
  2. select 等的是“下一次 channel 事件”
     如果当前没有未来事件（timer 已停、已触发但被消费且未 reset），ticker 会一直卡住。

  所以 Kill() 里 resetTimer 的作用是“制造一次未来事件”，保证 ticker 最终从 <-timer.C 醒来，再看到 dead 退出。

  你说“原本唤醒时间必然 <= reset 后唤醒时间”不总对，只有在“原 timer 确实还会触发”的情况下才可能比较。