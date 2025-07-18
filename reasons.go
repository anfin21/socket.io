package sio

import (
	eio "github.com/anfin21/socket.io/engine.io"
	mapset "github.com/deckarep/golang-set/v2"
)

type Reason = eio.Reason

const (
	ReasonIOServerDisconnect Reason = "io server disconnect"
	ReasonIOClientDisconnect Reason = "io client disconnect"
)

const (
	ReasonForcedClose    Reason = eio.ReasonForcedClose
	ReasonTransportClose Reason = eio.ReasonTransportClose
	ReasonTransportError Reason = eio.ReasonTransportError
	ReasonPingTimeout    Reason = eio.ReasonPingTimeout
	ReasonParseError     Reason = eio.ReasonParseError
)

const (
	ReasonServerShuttingDown        Reason = "server shutting down"
	ReasonForcedServerClose         Reason = "forced server close"
	ReasonClientNamespaceDisconnect Reason = "client namespace disconnect"
	ReasonServerNamespaceDisconnect Reason = "server namespace disconnect"
)

var recoverableDisconnectReasons = mapset.NewThreadUnsafeSet(
	ReasonTransportError,
	ReasonTransportClose,
	ReasonForcedClose,
	ReasonPingTimeout,
	ReasonServerShuttingDown,
	ReasonForcedServerClose,
)
