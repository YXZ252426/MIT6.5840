## 26.3.18
first glance of rsm.go

the whole chain from appendEntries raft.go to rsm.go server.go

distinguish the real disk data(persist data) change and the logically state change

the state change of raft kernel and the state change of upper level rsm

rsm.go, server.go act as a `gateway`!! a `frontend`!!, a little bit like syscall? accept the request from client and invoke raft function?

from now on, the raft kernel is a white box, a kernel, client call put, server wrap submit in it, and submit wait kernel for result

## 26.3.19

the most important field in RSM is waiter!
it represents a single transaction（事务）, it's lifetime is what we should concern

it begins at the Submit, then something happens in our white box raft kernel, and when the result of our white box pop or timeout, it report the result and 

so what we should pay attention? when waiter notify, when it is kicked out of the pending map!