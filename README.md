# lambda-invoker
Lightweight library to simplify invoking a lambda function.

## Usage
Initialze an Invoker, passing it a lambda.Service and the arn of the lambda 
function being invoked. Then invoke the function passing it your json request.
```
invoker := New(svc, "function-arn")
rsp, err := invoker.Invoke(ctx, []byte(`{"request":"content"}`))
// Do something with output.
```

### Router
It's likely you'll want to use the invoker with 'edstell/lambda-router'; an
Option has been included with this package to make this easy. Initialize a new
Invoker, passing it the 'AsProcedure' option.
`AsProcedure` takes a procedure name which instructs the invoked function which
handler to route this request to, and an 'unmarshalError' func - which will 
be used when unmarshaling a returned error. This allows you to define your own
custom error implementations and pass them between lambda functions.
You'll need to initialize a new Invoker for each procedure you intend to call.
```
invoker := New(svc, "function-arn", AsProcedure("On", unmarshalErrorFunc))
rsp, err := invoker.Invoke(ctx, []byte(`{"request":"content"}`))
```
