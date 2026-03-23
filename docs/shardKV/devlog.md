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