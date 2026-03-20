 res.(rpc.GetReply) is not really a "type cast" in Go. It is a type assertion.

  In your code, Submit() returns res as any:

  err, res := kv.rsm.Submit(args)

  any means “some value of unknown concrete type”. To use it as a rpc.GetReply, you must tell Go:

  res.(rpc.GetReply)

  That means: “I assert that res actually holds a rpc.GetReply.”

  So this line:

  *reply = res.(rpc.GetReply)

  means:

  1. Take res, whose static type is any
  2. Assert its concrete value is rpc.GetReply
  3. Assign that value into *reply

  Important distinction:

  - Type conversion / cast: converting one type to another, like int(x) or string(b)
  - Type assertion: extracting a concrete type from an interface value, like res.(rpc.GetReply)

  Safer form:

  rep, ok := res.(rpc.GetReply)
  if !ok {
        reply.Err = rpc.ErrWrongLeader // or some suitable error
        return
  }
  *reply = rep

  Because if res is not actually a rpc.GetReply, res.(rpc.GetReply) will panic.