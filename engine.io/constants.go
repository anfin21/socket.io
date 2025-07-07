package eio

import (
	"time"

	"github.com/anfin21/socket.io/engine.io/parser"
)

const (
	ProtocolVersion = parser.ProtocolVersion

	defaultMaxBufferSize  int64 = 1e6 // 100 MB
	defaultPingTimeout          = time.Second * 20
	defaultPingInterval         = time.Second * 25
	defaultUpgradeTimeout       = time.Second * 10
)
