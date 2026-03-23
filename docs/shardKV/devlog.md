## 26.3.22
the understanding of the architecture of shardKV and some interesting comparison to Ethereum client archi!!
first we have two clerk shardKV.clerk and shardgrp.clerk

and shardKV.clerk is a gateaway, it's function is routing while shardgrp.clerk contain the real manipulate request, for example Get, Put, FreezeShard

just like Ethereum client, our shardkv also has consensus layer and excution layer

for every operation, shardgrp.server will initiate a request(e.x. Get) that invoke rsm.Submit, then rsm will invoke rf.Start, and then the story is very familiar, LogEntries try to spread thought every raft server to get consensus.

it should be noted that in raft server, only LogEntries is recorded, and after a while, the LogEntry is commited, just like a block is committed in Ethereum.

then the information is transmit through applyCh, and it's time for excution layer.

excution layer will do the correspond logic for example doGet, doPut