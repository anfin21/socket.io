//go:build !amd64 || (amd64 && !(linux || windows || darwin))

package fast

import (
	"github.com/anfin21/socket.io/parser/json/serializer"
	gojson "github.com/anfin21/socket.io/parser/json/serializer/go-json"
)

func New() serializer.JSONSerializer {
	defaultConfig := DefaultConfig()
	return gojson.New(defaultConfig.GoJSON.EncodeOptions, defaultConfig.GoJSON.DecodeOptions)
}

func NewWithConfig(config Config) serializer.JSONSerializer {
	return gojson.New(config.GoJSON.EncodeOptions, config.GoJSON.DecodeOptions)
}

func Type() SerializerType {
	return SerializerTypeGoJSON
}
