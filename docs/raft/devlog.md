这次更新有几个比较有意思改动？

首先是把选举计时器内置在结构体内

其次是取消了heartbeat CH

不需要通过管道传递信息，而是通过Candidate逻辑判断退出

由于日志的加入,toLeader函数会比upgrade函数承载更加丰富的语义

The Raft paper views the log as 1-indexed, but we suggest that you implement it as 0-indexed, starting with a dummy entry at index=0 that has term 0. That allows the very first AppendEntries RPC to contain 0 as PrevLogIndex, and be a valid index into the log.