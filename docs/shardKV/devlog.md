## 26.3.22
the understanding of the architecture of shardKV and some interesting comparison to Ethereum client archi!!
first we have two clerk shardKV.clerk and shardgrp.clerk

and shardKV.clerk is a gateaway, it's function is routing while shardgrp.clerk contain the real manipulate request, for example Get, Put, FreezeShard

just like Ethereum client, our shardkv also has consensus layer and excution layer

for every operation, shardgrp.server will initiate a request(e.x. Get) that invoke rsm.Submit, then rsm will invoke rf.Start, and then the story is very familiar, LogEntries try to spread thought every raft server to get consensus.

it should be noted that in raft server, only LogEntries is recorded, and after a while, the LogEntry is commited, just like a block is committed in Ethereum.

then the information is transmit through applyCh, and it's time for excution layer.

excution layer will do the correspond logic for example doGet, doPut

## 26.3.23
shardKV.clerk的缓存机制理解，以及对整个分片配置，shard，group的理解

这里缓存 grpClerks 的前提就是：gid -> servers 通常不会频繁变化，所以没必要每次请求都重新创建一个 shardgrp.Clerk。客户端只要记住“这个 group 对应哪组 server”，后面访问落到这个 gid 的 shard 时就可以直接复用。

grpClerks 缓存的就是这个层次：

key 是 gid
value 是这个 group 的 server 列表和对应的 shardgrp.Clerk
所以 pruneCache() 的意义是：

当配置更新后，有些旧 gid 已经不在 config.Groups 里了
这些 group 可能已经 leave，或者当前配置里已经不存在
继续保留它们的 clerk 没有价值，还可能让客户端继续往旧 group 发请求
因此需要清掉“已经不在当前配置中的 group clerk”。

不过这里有一个细一点的点：

pruneCache() 只处理“group 消失了”
它没有处理“gid 还在，但这个 gid 对应的 server 列表变了”
也就是说它默认了一个更强的假设：

只要 gid 还在，group 的 server 成员基本稳定，可以继续复用旧 clerk
这在 lab 的常见场景里通常是够用的，因为配置变化主要是 shard 在 group 之间迁移，而不是 group 内 server 列表频繁变化。

所以你可以这样理解这段设计：

shard ownership 经常变，所以客户端要 updateConfig()
group clerk 相对稳定，所以可以缓存
但如果 group 整个消失了，就必须 pruneCache()

## 26.3.24

the conurrent problem when config change
critical section problem: when shardctrler, shardKV clerk still constantly invoke put and get data, and it will query config, so will 
it read old config? The question is: can they observe an old config, and if so, is that
  safe?

changeconfig include a set of shardgrp.Clerk rpc request, and there is a risk of failure, so how can we handle error?

particular attention should be paid to the path from ChangeConfigTo -> migrate -> Clerk.(Freeze, Install, Delete) -> server.(doFreeze, doInstall, doDelete)

here is some of me understanding
- shardcfg is like a snapshot, when configchange, UpdateConfig is the last stop, so shardKV.client might read old config, but it is acceptable, because the real config has been update by migrate, we frozen, we lock, we just have not create the final new snapshot yet. As a result it is critical section safe.
- migrate use for loop,  and will it result in infinite loop? No, migrate's required err is aligned with the return err from Clerk(actually doOp return)
- config change will not influence the no move shard, to some extent, fearless concurrent, (because no move is not a critical seciton?)
- migrate involves three steps, when freeze, the dst part is still available until invoke InstallShard it is locked
- concurrent principle: we should to determine what is the critical section, and when manipulate the critical section, we should consider two question: when manipulate, is it already locked to avoid concurrent access, it is atomic, a transaction, for example, what if freeze success but install failed, it will bring unwished side effect(dead shard?)

Polished Version（prepare for ILETS exam）

  issuing Put and Get requests, and they may also query the current configuration. The question is: can they observe an old config, and if so, is that
  safe?

  ChangeConfig involves a series of RPCs through shardgrp.Clerk, so failures are possible. That raises another question: how should errors be handled
  during migration?

  Special attention should be paid to the execution path:

  ChangeConfigTo -> migrate -> Clerk.(Freeze, Install, Delete) -> server.(doFreeze, doInstall, doDelete)

  Here is my current understanding:

  - shardcfg is like a snapshot. During a configuration change, UpdateConfig is the final step. So a ShardKV client may still observe the old config for a
    while. This is acceptable, because the actual migration has already been performed: the shard is frozen, access is protected by locking, and only the
    final snapshot has not yet been published. From that perspective, the critical section remains safe.
  - migrate uses a retry loop. Could that lead to an infinite loop? In principle, no, because the expected errors in migrate are aligned with the errors
    returned by Clerk calls, which ultimately come from doOp.
  - A configuration change should not affect shards that do not move. To some extent, those shards can continue serving requests concurrently, because they
    are outside the critical section.
  - Migration consists of three steps. After Freeze, the destination side is still available. It is only locked when InstallShard is invoked.
  - The key concurrency principle is to identify the true critical section. Once we know what belongs to that section, we need to ask two questions: first,
    is it properly locked to prevent concurrent access; second, is the whole operation atomic enough? For example, if Freeze succeeds but Install fails,
    could that leave the system in an undesired intermediate state, such as an unavailable or orphaned shard?