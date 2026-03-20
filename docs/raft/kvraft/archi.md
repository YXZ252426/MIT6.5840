In this design, KVServer is the actual key/value state machine, and Raft is the mechanism that makes every replica apply the same operations in the same order.

  The split is:

  - /home/yxz/Public/Coding/golang/6.5840/src/kvraft1/server.go: KVServer owns the real application state, like stores and cache.
  - rsm.go: RSM sends client requests through Raft and waits for them to commit.
  - Raft: does not directly manage your map[string]Entry; it replicates an ordered log of operations like PutArgs and GetArgs.

  The flow is:

  1. Client calls KVServer.Put() or KVServer.Get().
  2. That handler calls kv.rsm.Submit(args).
  5. When committed, RSM.reader() gets it from applyCh.
  6. RSM.handleApply() calls kv.DoOp(op.Req).
  7. KVServer.DoOp() finally updates or reads stores.

  So your intuition is right: the actual data lives in KVServer, not inside Raft. But each server has its own local copy of that data, and Raft ensures all servers mutate that copy in the same sequence. That is why the
  replicated state stays consistent.

  A useful way to think about it:

  - KVServer = "what the database state is"
  - Raft = "the agreement protocol for the order of state changes"
  - RSM = "the glue between them"

  Why even Get goes through Raft here: not because Get changes the map, but because routing reads through the leader's committed log gives a simple linearizable design for the lab.

  Also, after crashes, stores is only in memory, so durability comes from replaying Raft state or restoring from snapshots. That is why Snapshot() and Restore() exist: Raft persists the log/snapshot, and KVServer
  rebuilds its in-memory map from that persisted replicated state.

  ```

           Client
             |
             v
          Leader KVServer
             |
             v
          Leader RSM
             |
             v
          Leader Raft  <--------replicates log------->  Follower Raft
             |                                              |
        committed Op                                    committed Op
             v                                              v
          applyCh                                         applyCh
             v                                              v
          Leader KVServer.DoOp()                        Follower KVServer.DoOp()
             |                                              |
             v                                              v
        leader's stores                                follower's stores
  ```