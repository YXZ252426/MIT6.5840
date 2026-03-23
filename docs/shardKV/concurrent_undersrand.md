
## origin
i notice that each shard has a lock, but i think it is unneccesary because each ShardGroup owns its shards and will not be access by other ShardGroup.

and i think it will be better to use go rsm.handleCommand to doOp concurrently

and there are some clarification generate by codex

## lock is need
You do need the lock here, because this shard is not only touched by the single Raft apply path.

The important concurrent case is snapshotting:

RSM.handleCommand() may start go rsm.installSnapshot(...).
installSnapshot() calls sm.Snapshot().
ShardGroup.Snapshot() walks every shard and reads owned, frozen, maxNumSeen, and the maps.
At the same time, the apply goroutine can be running doInstall, doPut, doDelete, etc. for later log entries.
So even if state-machine commands are applied one-at-a-time, snapshot reads can overlap with command execution. Without s.mu.Lock() in doInstall, you could race with:

Snapshot() reading owned / frozen
pack() reading stores / cache
other shard ops that read or write the same shard state
doInstall is especially important to protect because it does a multi-step state transition:

check/update maxNumSeen
replace stores and cache via unpack
set owned and frozen
Without the lock, a concurrent snapshot could observe a half-installed shard, for example old flags with new maps, or new flags with partially replaced state.

So the correct mental model is:

Raft apply serializes command order.
Locks protect shared shard memory against concurrent snapshot/read access and keep each shard update atomic.

## concurrent will break serialized

No. Running rsm.handleCommand() or just DoOp() concurrently is usually the wrong design here.

Why:

Raft guarantees a total order of committed log entries.
The replicated state machine must apply those entries in that same order.
If you run DoOp() concurrently for different log indexes, later commands can overtake earlier ones and mutate state out of order.
That breaks correctness for cases like:

two Puts to the same key
shard migration RPCs interleaving with Get/Put
client dedup cache updates
config-number checks like maxNumSeen
Example:

log index 10: Put(x=1)
log index 11: Put(x=2)
If both DoOp() calls run concurrently, index 11 might finish first, then index 10 overwrites it. Final state becomes wrong even though Raft committed 10 -> 11.

What is okay to do concurrently:

snapshot creation, if protected by locks
notifying waiters after the state change is complete
background cleanup or metrics that do not affect replicated state
What must stay serialized:

all mutations of the replicated state machine
anything whose result depends on log order
So the usual rule is:

one applier goroutine reads applyCh
it applies committed commands one-by-one in log order
locks protect against concurrent readers/background snapshotting, not against parallel state-machine application
