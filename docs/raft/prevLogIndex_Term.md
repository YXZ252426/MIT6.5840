  Leader（L）日志：

  - 1:(t1,A)
  - 2:(t1,B)
  - 3:(t2,C)
  - 4:(t3,D)

  Follower（F）日志：

  - 1:(t1,A)
  - 2:(t1,B)
  - 3:(t4,X)  ← 从这里开始和 Leader 不同

  现在 L 想把 4:(t3,D) 复制给 F，会先发：

  - PrevLogIndex = 3
  - PrevLogTerm = 2
  - Entries = [4:(t3,D)]

  F 检查自己 index=3 的 term：是 t4，不是 t2，所以拒绝。

  这就是 PrevLogTerm 的作用：
  虽然两边都有 index=3，但不是同一条历史（term 不同），不能直接接后面的日志。

  然后 L 回退再试，比如改成：

  - PrevLogIndex = 2
  - PrevLogTerm = 1
  - Entries = [3:(t2,C), 4:(t3,D)]

  这次 F 在 index=2 的 term 是 t1，匹配，接受；并把自己原来的 3:(t4,X) 覆盖成 Leader 的日志。