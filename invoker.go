package invoker

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/aws/aws-sdk-go/aws"
	awsreq "github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/lambda"
	router "github.com/edstell/lambda-router"
)

// LambdaInvoker abstracts the logic of invoking a lambda function behind an
// interface, this is to allow mocking the aws Lambda implementation.
type LambdaInvoker interface {
	InvokeWithContext(context.Context, *lambda.InvokeInput, ...awsreq.Option) (*lambda.InvokeOutput, error)
}

// Error wraps an error message with a status code.
type Error struct {
	error
	StatusCode int64
}

// Invoker is a wrapper around the aws lambda invoker implementation. It
// provides a convenient layer for middleware, as well as exposing a simpler
// method to invoke a lambda function with.
type Invoker struct {
	li           LambdaInvoker
	arn          string
	MutateInput  func(*lambda.InvokeInput) error
	MutateOutput func(*lambda.InvokeOutput) error
}

// Option implementations can mutate the Invoker allowing configuration of how
// invokations of a lambda function should be performed.
type Option func(*Invoker)

// New initializes an Invoker with the options passed.
func New(li LambdaInvoker, arn string, opts ...Option) *Invoker {
	invoker := &Invoker{
		li:  li,
		arn: arn,
		MutateInput: func(i *lambda.InvokeInput) error {
			return nil
		},
		MutateOutput: func(o *lambda.InvokeOutput) error {
			return nil
		},
	}
	for _, opt := range opts {
		opt(invoker)
	}
	return invoker
}

// Invoke _invokes_ the lambda function passing body as the InvokeInput.Payload
// and returning the InvokeOutput.Payload as the result. If InvokeOutput
// contains a FunctionError an Error is returned, wrapping the status code.
// By default lambda functions are invoked as a 'RequestResponse', but
// input mutators can be passed to change the InvocationType.
func (i *Invoker) Invoke(ctx context.Context, body json.RawMessage, opts ...awsreq.Option) (json.RawMessage, error) {
	input := &lambda.InvokeInput{
		FunctionName:   aws.String(i.arn),
		InvocationType: aws.String("RequestResponse"),
		Payload:        body,
	}
	if err := i.MutateInput(input); err != nil {
		return nil, err
	}
	output, err := i.li.InvokeWithContext(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	if err := i.MutateOutput(output); err != nil {
		return nil, err
	}
	if message := output.FunctionError; message != nil {
		statusCode := int64(-1)
		if output.StatusCode != nil {
			statusCode = *output.StatusCode
		}
		return nil, &Error{errors.New(*message), statusCode}
	}
	return output.Payload, nil
}

// AsProcedure returns an option which can be passed when initializing an
// Invoker. If provided it will configure invocation to be performed as a call
// to the named procedure.
func AsProcedure(procedure string, unmarshalError func(json.RawMessage) error) Option {
	return func(i *Invoker) {
		i.MutateInput = func(input *lambda.InvokeInput) error {
			bytes, err := json.Marshal(router.Request{
				Procedure: procedure,
				Body:      input.Payload,
			})
			if err != nil {
				return err
			}
			input.Payload = bytes
			return nil
		}
		i.MutateOutput = func(output *lambda.InvokeOutput) error {
			if output.Payload == nil {
				return nil
			}
			rsp := &router.Response{}
			if err := json.Unmarshal(output.Payload, rsp); err != nil {
				return err
			}
			if rsp.Error == nil {
				output.Payload = rsp.Body
				return nil
			}
			return unmarshalError(rsp.Error)
		}
	}
}
