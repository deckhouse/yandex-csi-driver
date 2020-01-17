package ychelpers

import (
	"context"

	"github.com/golang/protobuf/proto"
	"github.com/yandex-cloud/go-genproto/yandex/cloud/operation"
	ycsdk "github.com/yandex-cloud/go-sdk"
	ycsdkoperation "github.com/yandex-cloud/go-sdk/operation"
)

func WaitForResult(ctx context.Context, sdk *ycsdk.SDK, origFunc func() (*operation.Operation, error)) (proto.Message, *ycsdkoperation.Operation, error) {
	op, err := sdk.WrapOperation(origFunc())
	if err != nil {
		return nil, nil, err
	}

	err = op.Wait(ctx)
	if err != nil {
		return nil, op, err
	}

	resp, err := op.Response()
	if err != nil {
		return nil, op, err
	}

	return resp, op, nil
}
