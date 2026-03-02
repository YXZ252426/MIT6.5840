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

  ### 3.2
   假设当前 leader 的 rf.log 里只保留了这些日志（前面的被 snapshot 掉了）：

  - 绝对日志下标：100,101,102,103,104,105
  - 所以 len(rf.log)=6
  - rf.firstLog().Index = 100

  某个 follower 的 nextIndex[server] = 103，表示它下一条想要的是绝对下标 103。

  旧写法（不 rebase）：

  - prevIndex := 102
  - prevTerm := rf.log[102].Term  // 把绝对下标当切片下标，直接越界
  - if rf.nextIndex[server] < len(rf.log) -> 103 < 6 为假，entries 也不会发

  新写法（rebase）：

  - rebaseIndex(102) = 2，所以 prevTerm = rf.log[2].Term（正确，对应绝对下标 102）
  - rebaseIndex(103) = 3，所以 entries = rf.log[3:]（对应绝对下标 103,104,105，正确）

  所以这个函数不是“仅保护性”，而是把“Raft 绝对下标”映射到“本地切片下标”的必要步骤。

  你可以把 Raft 快照理解成一句话：

  “用一张状态机全量照片，替换一大段旧日志。”

  核心机制就 5 步：

  1. 状态机决定可压缩到 index，调用 rf.Snapshot(index, data)。
  2. Raft 丢弃 [oldFirst..index] 的日志，只保留一个锚点 log[0]={Index:index, Term:...}，再加 index 后面的后缀日志。
  3. Leader 发现某个 follower 太落后（通常 nextIndex[peer] <= firstLog().Index），就不再发旧日志，改发 InstallSnapshot。
  4. Follower 收到后安装快照，更新 commitIndex/lastApplied 到 LastIncludedIndex，并把 ApplyMsg{SnapshotValid:true} 发给上层状态机。
  5. 上层状态机加载快照后，继续只应用 SnapshotIndex 之后的新日志。

  你这套代码里最重要的理解点：

  - nextIndex/commitIndex/lastApplied 是绝对日志下标。
  - rf.log 是裁剪后的切片，必须用 rebaseIndex 做映射。
  - rf.log[0] 不是普通命令，是“快照边界锚点”。

  常见坑（你现在已经踩到边缘了）：

  1. 把绝对下标直接当切片下标（必须 rebase）。
  2. InstallSnapshot 后快照消息和普通日志消息并发发送，顺序可能乱。
  3. 安装快照时，若本地同 index 的 term 不匹配，应该丢弃后缀（不能盲目保留）。