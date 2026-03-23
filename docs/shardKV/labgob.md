labgob is not gRPC. It is a thin wrapper around Go’s standard encoding/gob serializer in this lab, defined at labgob.go (line 1).

labgob.Register(x) at labgob.go (line 58) does two things:

It calls gob.Register(x), which tells Go’s gob encoder/decoder about a concrete type.
It runs a local safety check that warns if your struct has lowercase fields, because gob/RPC won’t serialize unexported fields correctly. See labgob.go (line 68) and labgob.go (line 87).
Why registration is needed here:

Gob can often encode plain concrete structs without manual registration.
But when a value may travel through an interface{} / any, gob needs to know the concrete runtime type in advance.
That is exactly your case:

rsm.Op contains Req any at rsm.go (line 20).
Raft replicates Op.
Req may actually hold *rpc.GetArgs, *rpc.PutArgs, *shardrpc.FreezeShardArgs, etc.
So those concrete request types must be registered before gob can encode/decode them correctly.
That is why StartServerShardGrp registers these at server.go (line 267):

&rpc.PutArgs{}
&rpc.GetArgs{}
&shardrpc.FreezeShardArgs{}
&shardrpc.InstallShardArgs{}
&shardrpc.DeleteShardArgs{}
rsm.Op{}
snapshot-related structs/maps like Entry{}, CacheEntry{}, shardHdr{}, map[string]Entry{}
You can think of it as: “tell the serializer all possible concrete message/state types that may appear inside generic containers or persisted snapshots.”

Two practical rules:

Register every concrete type that may be stored inside an interface{} or persisted in snapshots.
Make fields exported if they need to be encoded. Value string works; value string does not.