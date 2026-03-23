# Note: `srvCopy`, Go Slice Aliasing, and Rust Comparison

## `srvCopy` Example

```go
func (ck *Clerk) genGrpClerk(key string) *shardgrp.Clerk {
	shard := shardcfg.Key2Shard(key)

	ck.mu.Lock()
	defer ck.mu.Unlock()
	gid, srv, _ := ck.config.GidServers(shard)
	entry, ok := ck.grpClerks[gid]
	if !ok || !slices.Equal(entry.servers, srv) {
		srvCopy := append([]string(nil), srv...)
		entry = &grpClerk{
			servers: srvCopy,
			clerk:   shardgrp.MakeClerk(ck.clnt, srvCopy),
		}
		ck.grpClerks[gid] = entry
	}
	raft.DPrintf("[Client] route key=%s shard=%d gid=%d servers=%v", key, shard, gid, entry.servers)
	return entry.clerk
}
```
Why srvCopy exists
GidServers() returns the slice stored in config.Groups[gid] directly:

```go

func (cfg *ShardConfig) GidServers(sh Tshid) (tester.Tgid, []string, bool) {
	gid := cfg.Shards[sh]
	srvs, ok := cfg.Groups[gid]
	return gid, srvs, ok
}
```
So srv is not an independent copy. It is another slice header pointing to the same backing array.

If we write:


``entry.servers = srv``
then entry.servers and cfg.Groups[gid] may alias the same underlying array.

Using:

``srvCopy := append([]string(nil), srv...)``
creates a fresh slice backing array and copies the elements. For []string, this is enough to make the cached server list independent.

Important Go point
In Go, assigning a slice does not deep-copy the underlying data.

A slice is roughly:

```go
type slice struct {
	data *T
	len  int
	cap  int
}
```
So:
``b := a``
copies the slice header, not the backing array.

That means:

a and b usually refer to the same underlying array
changing b[i] can affect a[i]
append may or may not allocate a new array depending on capacity
About append
append is not always a deep copy.



``b := append(a, x)``
has two cases:

If capacity is enough, it reuses the same backing array.
If capacity is not enough, it allocates a new array and copies elements.
But this pattern:



``srvCopy := append([]string(nil), srv...)``
is a standard way to force a new slice for srvCopy.

For []string, this behaves like a practical deep-enough copy.

Go vs Rust understanding
Rust
Rust makes ownership and memory behavior much more explicit:

ownership is tracked by the type system
aliasing and mutation are restricted
stack vs heap is often easier to reason about
heap allocation is usually explicit through types like Box, Vec, String, Rc, Arc
Go
Go is more implicit:

stack vs heap is decided mostly by escape analysis
reference-like behavior comes from the type: slice, map, pointer, interface, chan, func
value-like behavior comes from arrays and structs, but those may still contain reference-like fields inside
So in Go, the useful question is usually not:

"is this on the stack or heap?"
The useful question is:

"does this assignment copy the real data, or only copy a header/reference?"
Practical rule
In Go:

int, bool, arrays: value copy
structs: value copy, but fields may still alias if they are slices/maps/pointers
slices: copy header, usually share backing array
maps: copy map header, share underlying map storage
pointers: copy pointer, share pointee
So for this code, srvCopy is not about Rust-style ownership rules.
It is about avoiding slice aliasing and making the cached server list stable.