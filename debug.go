package sio

import eio "github.com/anfin21/socket.io/engine.io"

type Debugger = eio.Debugger

var (
	NewPrintDebugger = eio.NewPrintDebugger
	newNoopDebugger  = eio.NewNoopDebugger
)
