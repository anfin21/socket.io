package sio

import eio "github.com/hhuuson97/socket.io-go/engine.io"

type Debugger = eio.Debugger

var (
	NewPrintDebugger = eio.NewPrintDebugger
	newNoopDebugger  = eio.NewNoopDebugger
)
