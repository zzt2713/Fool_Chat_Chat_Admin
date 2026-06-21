package main

import (
	"context"
	"time"

	"github.com/golang/protobuf/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding"
)

func init() {
	encoding.RegisterCodec(legacyProtoCodec{})
}

type legacyProtoCodec struct{}

func (legacyProtoCodec) Name() string {
	return "proto"
}

func (legacyProtoCodec) Marshal(v any) ([]byte, error) {
	return proto.Marshal(v.(proto.Message))
}

func (legacyProtoCodec) Unmarshal(data []byte, v any) error {
	return proto.Unmarshal(data, v.(proto.Message))
}

type getVarifyReq struct {
	Email string `protobuf:"bytes,1,opt,name=email,proto3" json:"email,omitempty"`
}

func (m *getVarifyReq) Reset()         { *m = getVarifyReq{} }
func (m *getVarifyReq) String() string { return proto.CompactTextString(m) }
func (*getVarifyReq) ProtoMessage()    {}

type getVarifyRsp struct {
	Error int32  `protobuf:"varint,1,opt,name=error,proto3" json:"error,omitempty"`
	Email string `protobuf:"bytes,2,opt,name=email,proto3" json:"email,omitempty"`
	Code  string `protobuf:"bytes,3,opt,name=code,proto3" json:"code,omitempty"`
}

func (m *getVarifyRsp) Reset()         { *m = getVarifyRsp{} }
func (m *getVarifyRsp) String() string { return proto.CompactTextString(m) }
func (*getVarifyRsp) ProtoMessage()    {}

func (a *app) sendVerifyCode(email string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, env("VERIFY_SERVER_ADDR", a.cfg.VerifyAddr),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.CallContentSubtype("proto")),
	)
	if err != nil {
		return err
	}
	defer conn.Close()

	var rsp getVarifyRsp
	if err := conn.Invoke(ctx, "/message.VarifyService/GetVarifyCode", &getVarifyReq{Email: email}, &rsp); err != nil {
		return err
	}
	if rsp.Error != 0 {
		return errVerifyFailed
	}
	return nil
}
