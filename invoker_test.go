package invoker

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	awsreq "github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/lambda"
	router "github.com/edstell/lambda-router"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type LambdaInvokerFunc func(context.Context, *lambda.InvokeInput, ...awsreq.Option) (*lambda.InvokeOutput, error)

func (f LambdaInvokerFunc) InvokeWithContext(ctx context.Context, i *lambda.InvokeInput, opts ...awsreq.Option) (*lambda.InvokeOutput, error) {
	return f(ctx, i, opts...)
}

func TestInvoke(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	arn := "test-arn"
	body := json.RawMessage(`{"key":"value"}`)
	output := []byte(`{"invoke":"result"}`)
	li := LambdaInvokerFunc(func(_ context.Context, i *lambda.InvokeInput, _ ...awsreq.Option) (*lambda.InvokeOutput, error) {
		assert.Equal(t, string(body), string(i.Payload))
		return &lambda.InvokeOutput{
			Payload: output,
		}, nil
	})
	invoker := New(li, arn)
	result, err := invoker.Invoke(ctx, body)
	require.NoError(t, err)
	assert.Equal(t, string(output), string(result))
}

func TestInvokeWithError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	arn := "test-arn"
	message := "invocation failed"
	li := LambdaInvokerFunc(func(_ context.Context, _ *lambda.InvokeInput, _ ...awsreq.Option) (*lambda.InvokeOutput, error) {
		return &lambda.InvokeOutput{
			FunctionError: aws.String(message),
			StatusCode:    aws.Int64(http.StatusUnauthorized),
		}, nil
	})
	invoker := New(li, arn)
	_, err := invoker.Invoke(ctx, nil)
	require.Error(t, err)
	assert.Equal(t, message, err.Error())
}

func TestInvokeAsProcedure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	arn := "test-arn"
	procedure := "Do"
	response := map[string]interface{}{
		"response": "content",
	}
	r := router.New()
	r.Route(procedure, router.HandlerFunc(func(_ context.Context, b json.RawMessage) (json.RawMessage, error) {
		bytes, err := json.Marshal(response)
		if err != nil {
			return nil, err
		}
		return bytes, nil
	}))
	li := LambdaInvokerFunc(func(ctx context.Context, input *lambda.InvokeInput, _ ...awsreq.Option) (*lambda.InvokeOutput, error) {
		req := &router.Request{}
		if err := json.Unmarshal(input.Payload, req); err != nil {
			return nil, err
		}
		rsp, err := r.Handle(ctx, *req)
		if err != nil {
			return nil, err
		}
		bytes, err := json.Marshal(rsp)
		if err != nil {
			return nil, err
		}
		return &lambda.InvokeOutput{
			Payload: bytes,
		}, nil
	})
	invoker := New(li, arn, AsProcedure(procedure, func(e json.RawMessage) error {
		return errors.New(string(e))
	}))
	result, err := invoker.Invoke(ctx, nil)
	require.NoError(t, err)
	rsp := map[string]interface{}{}
	err = json.Unmarshal(result, &rsp)
	require.NoError(t, err)
	assert.Equal(t, response, rsp)
}

func TestInvokeAsProcedureWithError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	arn := "test-arn"
	procedure := "Do"
	r := router.New(router.MarshalErrorsWith(func(error) ([]byte, error) {
		return []byte(`{"complex":"error"}`), nil
	}))
	r.Route(procedure, router.HandlerFunc(func(_ context.Context, b json.RawMessage) (json.RawMessage, error) {
		return nil, assert.AnError
	}))
	li := LambdaInvokerFunc(func(ctx context.Context, input *lambda.InvokeInput, _ ...awsreq.Option) (*lambda.InvokeOutput, error) {
		req := &router.Request{}
		if err := json.Unmarshal(input.Payload, req); err != nil {
			return nil, err
		}
		rsp, err := r.Handle(ctx, *req)
		if err != nil {
			return nil, err
		}
		bytes, err := json.Marshal(rsp)
		if err != nil {
			return nil, err
		}
		return &lambda.InvokeOutput{
			Payload: bytes,
		}, nil
	})
	type Error struct {
		error
		Complex string `json:"complex"`
	}
	invoker := New(li, arn, AsProcedure(procedure, func(e json.RawMessage) error {
		err := &Error{}
		if err := json.Unmarshal(e, err); err != nil {
			return err
		}
		return err
	}))
	_, err := invoker.Invoke(ctx, nil)
	require.Error(t, err)
	e, ok := err.(*Error)
	require.True(t, ok)
	assert.Equal(t, "error", e.Complex)
}
