package invoker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

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
	li             LambdaInvoker
	arn            string
	InputMutators  []func(*lambda.InvokeInput) (*lambda.InvokeInput, error)
	OutputMutators []func(*lambda.InvokeOutput) (*lambda.InvokeOutput, error)
}

// Option implementations can mutate the Invoker allowing configuration of how
// invokations of a lambda function should be performed.
type Option func(*Invoker)

// New initializes an Invoker with the options passed.
func New(li LambdaInvoker, arn string, opts ...Option) *Invoker {
	invoker := &Invoker{
		li:  li,
		arn: arn,
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
func (i *Invoker) Invoke(ctx context.Context, body json.RawMessage) (json.RawMessage, error) {
	input := &lambda.InvokeInput{
		FunctionName:   aws.String(i.arn),
		InvocationType: aws.String("RequestResponse"),
		Payload:        body,
	}
	for _, mutate := range i.InputMutators {
		i, err := mutate(input)
		if err != nil {
			return nil, err
		}
		input = i
	}
	output, err := i.li.InvokeWithContext(ctx, input)
	if err != nil {
		return nil, err
	}
	for _, mutate := range i.OutputMutators {
		o, err := mutate(output)
		if err != nil {
			return nil, err
		}
		output = o
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

// AsProcedure adds input and output mutators to modify the requests and
// responses to adhere to the lambda-router protocol.
func AsProcedure(procedure string, unmarshalError func(json.RawMessage) error) Option {
	if unmarshalError == nil {
		unmarshalError = func(e json.RawMessage) error {
			var i interface{}
			if err := json.Unmarshal(e, &i); err != nil {
				return errors.New(string(e))
			}
			return errors.New(fmt.Sprint(i))
		}
	}
	return func(i *Invoker) {
		// Add an input mutator which marshals the request as a router.Request.
		i.InputMutators = append(i.InputMutators, func(input *lambda.InvokeInput) (*lambda.InvokeInput, error) {
			bytes, err := json.Marshal(router.Request{
				Procedure: procedure,
				Body:      input.Payload,
			})
			if err != nil {
				return nil, err
			}
			input.Payload = bytes
			return input, nil
		})
		// Add an output mutator which will extract and return an error if the
		// response contains one.
		i.OutputMutators = append(i.OutputMutators, func(output *lambda.InvokeOutput) (*lambda.InvokeOutput, error) {
			if output.Payload == nil {
				return output, nil
			}
			rsp := &router.Response{}
			if err := json.Unmarshal(output.Payload, rsp); err != nil {
				return nil, err
			}
			if rsp.Error == nil {
				output.Payload = rsp.Body
				return output, nil
			}
			return nil, unmarshalError(rsp.Error)
		})
	}
}
