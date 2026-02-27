这次更新有几个比较有意思改动？

首先是把选举计时器内置在结构体内

其次是取消了heartbeat CH

不需要通过管道传递信息，而是通过Candidate逻辑判断退出

由于日志的加入,toLeader函数会比upgrade函数承载更加丰富的语义

The Raft paper views the log as 1-indexed, but we suggest that you implement it as 0-indexed, starting with a dummy entry at index=0 that has term 0. That allows the very first AppendEntries RPC to contain 0 as PrevLogIndex, and be a valid index into the log.

2.27
fix(raft): Complete log replicate
原先的upgrade， downgrade函数不好，用toLeader等更好的体现出状态的变化
election timeout 只有在“看到活跃 leader”或“你刚把票投给某候选人”时才应刷新。

term概念再深化，term就是任期，每个任期只能有一个leader，成为候选人后自增Term，评选下个任期的leader
在变成follower的时候似乎不需要状态清理？

代码里有非常多的防御性编程，主要用于server的角色变换防范
但是这个粒度要怎么把握