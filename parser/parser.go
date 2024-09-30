package parser

import (
	"github.com/karagenc/socket.io-go/parser/json/serializer"
	"reflect"
)

const ProtocolVersion = 5

type (
	Creator func() Parser
	Finish  func(header *PacketHeader, eventName string, decode Decode)
	Decode  func(types ...reflect.Type) (values []reflect.Value, err error)
)

type Parser interface {
	Encode(header *PacketHeader, v any) (buffers [][]byte, err error)
	Add(data []byte, finish Finish) error
	Reset()
	JSONSerializer() serializer.JSONSerializer
}
