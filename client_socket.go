package sio

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sync/atomic"
	"time"

	"github.com/anfin21/socket.io/internal/sync"

	"github.com/anfin21/socket.io/adapter"
	eioparser "github.com/anfin21/socket.io/engine.io/parser"
	"github.com/anfin21/socket.io/parser"
	"github.com/fatih/structs"
)

type (
	ClientSocketConfig struct {
		// Authentication data.
		//
		// This can also be set/overridden using Socket.SetAuth method.
		Auth any

		// The maximum number of retries for the packet to be sent.
		// Above the limit, the packet will be discarded.
		//
		// Using `Infinity` means the delivery guarantee is
		// "at-least-once" (instead of "at-most-once" by default),
		// but a smaller value like 10 should be sufficient in practice.
		Retries int

		// The default timeout used when waiting for an acknowledgement.
		AckTimeout time.Duration
	}

	clientSocket struct {
		id atomic.Value

		state   clientSocketConnectionState
		stateMu sync.RWMutex

		_pid        atomic.Value
		_lastOffset atomic.Value
		_recovered  atomic.Value

		config    *ClientSocketConfig
		namespace string
		manager   *Manager
		parser    parser.Parser

		authData   any
		authDataMu sync.Mutex

		sendBuffer   []sendBufferItem
		sendBufferMu sync.Mutex

		receiveBuffer   []*clientEvent
		receiveBufferMu sync.Mutex

		eventHandlers        *eventHandlerStore
		connectHandlers      *handlerStore[*ClientSocketConnectFunc]
		connectErrorHandlers *handlerStore[*ClientSocketConnectErrorFunc]
		disconnectHandlers   *handlerStore[*ClientSocketDisconnectFunc]

		acks   map[uint64]*ackHandler
		ackID  uint64
		acksMu sync.Mutex

		active        bool
		subDeregister func()
		activeMu      sync.Mutex

		packetQueue *clientPacketQueue
		sendBuffers func(volatile, forceSend bool, ackID *uint64, buffers ...[]byte)

		debug Debugger
	}
)

type clientSocketConnectionState int

// clientSocketConnStateConnectPending, as its name suggests, is set when
// CONNECT packet is sent, and we're waiting for server's response.

const (
	clientSocketConnStateConnected clientSocketConnectionState = iota
	clientSocketConnStateConnectPending
	clientSocketConnStateDisconnected
)

func newClientSocket(
	config *ClientSocketConfig,
	manager *Manager,
	namespace string,
	parser parser.Parser,
) *clientSocket {
	s := &clientSocket{
		state:     clientSocketConnStateDisconnected,
		config:    config,
		namespace: namespace,
		manager:   manager,
		parser:    parser,
		acks:      make(map[uint64]*ackHandler),

		eventHandlers:        newEventHandlerStore(),
		connectHandlers:      newHandlerStore[*ClientSocketConnectFunc](),
		connectErrorHandlers: newHandlerStore[*ClientSocketConnectErrorFunc](),
		disconnectHandlers:   newHandlerStore[*ClientSocketDisconnectFunc](),
	}
	s.sendBuffers = s._sendBuffers
	s.debug = manager.debug.WithContext("[sio/client] Socket (nsp: `" + namespace + "`)")
	s.packetQueue = newClientPacketQueue(s)
	s.setRecovered(false)
	s.SetAuth(config.Auth)
	return s
}

func (s *clientSocket) ID() SocketID {
	id, _ := s.id.Load().(SocketID)
	return id
}

func (s *clientSocket) setID(id SocketID) {
	if id == "" {
		s.debug.Log("sid is cleared (set to empty string)")
	} else {
		s.debug.Log("sid is set to", id)
	}
	s.id.Store(id)
}

func (s *clientSocket) pid() (pid adapter.PrivateSessionID, ok bool) {
	pid, ok = s._pid.Load().(adapter.PrivateSessionID)
	if ok && pid == "" {
		return "", false
	}
	return
}

func (s *clientSocket) setPID(pid adapter.PrivateSessionID) {
	s._pid.Store(pid)
}

func (s *clientSocket) lastOffset() (lastOffset string, ok bool) {
	lastOffset, ok = s._lastOffset.Load().(string)
	return
}

func (s *clientSocket) setLastOffset(lastOffset string) {
	s._lastOffset.Store(lastOffset)
}

func (s *clientSocket) Recovered() bool {
	recovered, _ := s._recovered.Load().(bool)
	return recovered
}

func (s *clientSocket) setRecovered(recovered bool) {
	s._recovered.Store(recovered)
}

func (s *clientSocket) Connected() bool {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.manager.connected() && s.state == clientSocketConnStateConnected
}

func (s *clientSocket) connectedOrConnectPending() bool {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.manager.connected() && (s.state == clientSocketConnStateConnected || s.state == clientSocketConnStateConnectPending)
}

// Whether the socket will try to reconnect when its Client (manager) connects or reconnects.
func (s *clientSocket) Active() bool {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	return s.active
}

func (s *clientSocket) registerSubEvents() {
	var (
		openFunc ManagerOpenFunc = func() {
			s.stateMu.Lock()
			defer s.stateMu.Unlock()
			if s.state == clientSocketConnStateConnectPending {
				return
			}
			s.state = clientSocketConnStateConnectPending
			s.onOpen()
		}
		errorFunc ManagerErrorFunc = func(err error) {
			if !s.connectedOrConnectPending() {
				s.connectErrorHandlers.forEach(func(handler *ClientSocketConnectErrorFunc) { (*handler)(err) }, true)
			}
		}
		closeFunc ManagerCloseFunc = func(reason Reason, err error) {
			s.onClose(reason)
		}
	)

	s.activeMu.Lock()
	s.active = true
	s.manager.openHandlers.onSubEvent(&openFunc)
	s.manager.errorHandlers.onSubEvent(&errorFunc)
	s.manager.closeHandlers.onSubEvent(&closeFunc)
	s.subDeregister = func() {
		s.manager.openHandlers.offSubEvent(&openFunc)
		s.manager.errorHandlers.offSubEvent(&errorFunc)
		s.manager.closeHandlers.offSubEvent(&closeFunc)
	}
	s.activeMu.Unlock()
}

func (s *clientSocket) deregisterSubEvents() {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	s.active = false
	if s.subDeregister != nil {
		s.subDeregister()
		s.subDeregister = nil
	}
}

func (s *clientSocket) Connect() {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()

	if s.state == clientSocketConnStateConnected {
		return
	}

	s.registerSubEvents()

	s.manager.stateMu.RLock()
	managerConnState := s.manager.state
	s.manager.stateMu.RUnlock()
	if managerConnState != clientConnStateReconnecting {
		go s.manager.open()
	}

	// If already connected, send a CONNECT packet.
	if managerConnState == clientConnStateConnected && s.state != clientSocketConnStateConnectPending {
		s.state = clientSocketConnStateConnectPending
		s.onOpen()
	}
}

func (s *clientSocket) Disconnect() {
	if s.connectedOrConnectPending() {
		s.debug.Log("Performing disconnect", s.namespace)
		s.sendControlPacket(parser.PacketTypeDisconnect, nil)
	}

	s.destroy()

	s.stateMu.RLock()
	connected := s.state == clientSocketConnStateConnected || s.state == clientSocketConnStateConnectPending
	s.stateMu.RUnlock()
	if connected {
		s.onClose(ReasonIOClientDisconnect)
	}
}

func (s *clientSocket) Manager() *Manager { return s.manager }

func (s *clientSocket) Auth() any {
	s.authDataMu.Lock()
	defer s.authDataMu.Unlock()
	return s.authData
}

func (s *clientSocket) SetAuth(data any) {
	err := s.setAuth(data)
	if err != nil {
		panic(fmt.Errorf("sio: %w", err))
	}
}

func (s *clientSocket) setAuth(data any) error {
	if data != nil {
		rt := reflect.TypeOf(data)
		k := rt.Kind()

		if k == reflect.Ptr {
			rt = rt.Elem()
			k = rt.Kind()
		}

		if k != reflect.Struct && k != reflect.Map {
			return fmt.Errorf("SetAuth: non-JSON data cannot be accepted. please provide a struct or map")
		}
	}
	s.authDataMu.Lock()
	s.authData = data
	s.authDataMu.Unlock()
	return nil
}

func (s *clientSocket) onOpen() {
	s.debug.Log("onOpen called. Connecting")
	authData := s.Auth()
	s.sendConnectPacket(authData)
}

func (s *clientSocket) sendConnectPacket(authData any) {
	var v any
	pid, ok := s.pid()
	if ok {
		m := make(map[string]any)
		m["pid"] = pid
		lastOffset, _ := s.lastOffset()
		m["offset"] = lastOffset

		if authData != nil {
			a := structs.New(&authData)
			a.TagName = "json"
			for k, v := range a.Map() {
				m[k] = v
			}
		}
		v = m
	} else if authData != nil {
		v = &authData
	}

	// This function is called from onOpen, and onOpen can be called via `Manager.openHandlers`.
	// We fire a seperate goroutine because eioMu is locked inside `Manager.Connect`, which indirectly calls this method.
	go s.sendControlPacket(parser.PacketTypeConnect, v)
}

func (s *clientSocket) onPacket(header *parser.PacketHeader, eventName string, decode parser.Decode) {
	switch header.Type {
	case parser.PacketTypeConnect:
		s.onConnect(header, decode)

	case parser.PacketTypeEvent, parser.PacketTypeBinaryEvent:
		var (
			mu   sync.Mutex
			sent bool
		)

		sendAck := func(ackID uint64, values []reflect.Value) {
			mu.Lock()
			if sent {
				mu.Unlock()
				return
			}
			sent = true
			mu.Unlock()

			s.debug.Log("Sending ack with ID", ackID)
			s.sendAckPacket(ackID, values)
		}

		for _, handler := range s.eventHandlers.getAll(eventName) {
			s.onEvent(handler, header, decode, sendAck)
		}
	case parser.PacketTypeAck, parser.PacketTypeBinaryAck:
		s.onAck(header, decode)

	case parser.PacketTypeConnectError:
		s.onConnectError(header, decode)

	case parser.PacketTypeDisconnect:
		s.onDisconnect()
	}
}

func (s *clientSocket) onConnect(_ *parser.PacketHeader, decode parser.Decode) {
	connectError := func(err error) {
		err = fmt.Errorf("sio: invalid CONNECT packet: %w: it seems you are trying to reach a Socket.IO server in v2.x with a v3.x client, but they are not compatible (more information here: https://socket.io/docs/v3/migrating-from-2-x-to-3-0/)", err)
		s.connectErrorHandlers.forEach(func(handler *ClientSocketConnectErrorFunc) { (*handler)(err) }, true)
	}

	var v *sidInfo
	vt := reflect.TypeOf(v)
	values, err := decode(vt)
	if err != nil {
		connectError(err)
		return
	} else if len(values) != 1 {
		connectError(wrapInternalError(fmt.Errorf("len(values) != 1")))
		return
	}

	v, ok := values[0].Interface().(*sidInfo)
	if !ok {
		connectError(wrapInternalError(fmt.Errorf("cast failed")))
		return
	}

	if v.SID == "" {
		connectError(wrapInternalError(fmt.Errorf("sid is empty")))
		return
	}

	if v.PID != "" {
		pid, ok := s.pid()
		if ok && pid == adapter.PrivateSessionID(v.PID) {
			s.setRecovered(true)
		}
		s.setPID(adapter.PrivateSessionID(v.PID))
	}

	s.setID(SocketID(v.SID))

	s.stateMu.Lock()
	s.state = clientSocketConnStateConnected
	s.stateMu.Unlock()

	s.debug.Log("Socket connected")

	s.emitBuffered()
	s.connectHandlers.forEach(func(handler *ClientSocketConnectFunc) { (*handler)() }, false)
	s.packetQueue.drainQueue(true)
}

type sendBufferItem struct {
	ackID  *uint64
	packet *eioparser.Packet
}

func (s *clientSocket) emitBuffered() {
	s.receiveBufferMu.Lock()
	defer s.receiveBufferMu.Unlock()

	var (
		// The reason we use this map is that we don't want to
		// send acknowledgements with same IDs.
		ackIDs = make(map[uint64]bool, len(s.receiveBuffer))
		mu     sync.Mutex
	)

	for _, event := range s.receiveBuffer {
		if event.header.ID != nil {
			ackIDs[*event.header.ID] = false
		}
	}

	for _, event := range s.receiveBuffer {
		event := event

		sendAck := func(ackID uint64, values []reflect.Value) {
			mu.Lock()
			sent, ok := ackIDs[ackID]
			if ok && sent {
				mu.Unlock()
				return
			}
			ackIDs[ackID] = true
			mu.Unlock()

			s.debug.Log("Sending ack with ID", ackID)
			s.sendAckPacket(ackID, values)
		}

		hasAckFunc := s.callEvent(event.handler, event.header, event.values, sendAck)

		if event.header.ID != nil {
			mu.Lock()
			send := !hasAckFunc
			sent, ok := ackIDs[*event.header.ID]
			if ok && sent {
				mu.Unlock()
				return
			}
			ackIDs[*event.header.ID] = true
			mu.Unlock()

			// If there is no acknowledgement function
			// and there is no response already sent,
			// then send an empty acknowledgement.
			if send {
				s.debug.Log("Sending ack with ID", *event.header.ID)
				s.sendAckPacket(*event.header.ID, nil)
			}
		}
	}
	s.receiveBuffer = nil

	s.sendBufferMu.Lock()
	defer s.sendBufferMu.Unlock()
	if len(s.sendBuffer) != 0 {
		packets := make([]*eioparser.Packet, len(s.sendBuffer))
		for i := range packets {
			packets[i] = s.sendBuffer[i].packet
		}
		s.manager.packet(packets...)
		s.sendBuffer = nil
	}
}

type connectError struct {
	Message any             `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (s *clientSocket) onConnectError(_ *parser.PacketHeader, decode parser.Decode) {
	s.destroy()

	var v *connectError
	vt := reflect.TypeOf(v)
	values, err := decode(vt)
	if err != nil {
		s.onError(wrapInternalError(err))
		return
	} else if len(values) != 1 {
		s.onError(wrapInternalError(fmt.Errorf("invalid CONNECT_ERROR packet")))
		return
	}

	v, ok := values[0].Interface().(*connectError)
	if !ok {
		s.onError(wrapInternalError(fmt.Errorf("invalid CONNECT_ERROR packet: cast failed")))
		return
	}
	s.connectErrorHandlers.forEach(func(handler *ClientSocketConnectErrorFunc) {
		if s, ok := v.Message.(string); ok {
			(*handler)(fmt.Errorf("%s", s))
		} else {
			(*handler)(v.Message)
		}
	}, false)
}

func (s *clientSocket) onDisconnect() {
	s.debug.Log("Server disconnect", s.namespace)
	s.destroy()
	s.onClose(ReasonIOServerDisconnect)
}

type clientEvent struct {
	handler *eventHandler
	header  *parser.PacketHeader
	values  []reflect.Value
}

type ackSendFunc = func(id uint64, values []reflect.Value)

func (s *clientSocket) onEvent(
	handler *eventHandler,
	header *parser.PacketHeader,
	decode parser.Decode,
	sendAck ackSendFunc,
) (hasAckFunc bool) {
	values, err := decode(handler.inputArgs...)
	if err != nil {
		s.onError(wrapInternalError(err))
		return
	}

	if len(values) == len(handler.inputArgs) {
		for i, v := range values {
			if handler.inputArgs[i].Kind() != reflect.Ptr && v.Kind() == reflect.Ptr {
				values[i] = v.Elem()
			}
		}
	} else {
		s.onError(fmt.Errorf("sio: onEvent: invalid number of arguments"))
		return
	}

	s.stateMu.RLock()
	connected := s.state == clientSocketConnStateConnected
	s.stateMu.RUnlock()
	if connected {
		return s.callEvent(handler, header, values, sendAck)
	} else {
		s.receiveBufferMu.Lock()
		defer s.receiveBufferMu.Unlock()
		s.receiveBuffer = append(s.receiveBuffer, &clientEvent{
			handler: handler,
			header:  header,
			values:  values,
		})
	}
	return
}

func (s *clientSocket) callEvent(
	handler *eventHandler,
	header *parser.PacketHeader,
	values []reflect.Value,
	sendAck ackSendFunc,
) (hasAckFunc bool) {
	// Set the lastOffset before calling the handler.
	// An error can occur when the handler gets called,
	// and we can miss setting the lastOffset.
	_, ok := s.pid()
	if ok && len(values) > 0 && values[len(values)-1].Kind() == reflect.String {
		s.setLastOffset(values[len(values)-1].String())
		values = values[:len(values)-1] // Remove offset
	}

	ack, _ := handler.ack()
	if header.ID != nil && ack {
		hasAckFunc = true

		// We already know that the last value of the handler is an ack function
		// and it doesn't have a return value. So dismantle it, and create it with reflect.MakeFunc.
		f := values[len(values)-1]
		in, variadic := dismantleAckFunc(f.Type())
		rt := reflect.FuncOf(in, nil, variadic)

		f = reflect.MakeFunc(rt, func(args []reflect.Value) (results []reflect.Value) {
			sendAck(*header.ID, args)
			return nil
		})
		values[len(values)-1] = f
	}

	_, err := handler.call(values...)
	if err != nil {
		s.onError(wrapInternalError(err))
		return
	}
	return
}

func (s *clientSocket) onAck(header *parser.PacketHeader, decode parser.Decode) {
	if header.ID == nil {
		s.onError(wrapInternalError(fmt.Errorf("header.ID is nil")))
		return
	}

	s.debug.Log("Calling ack with ID", *header.ID)

	s.acksMu.Lock()
	ack, ok := s.acks[*header.ID]
	if ok {
		delete(s.acks, *header.ID)
	}
	s.acksMu.Unlock()

	if !ok {
		s.onError(wrapInternalError(fmt.Errorf("ACK with ID %d not found", *header.ID)))
		return
	}

	inputArgs := ack.inputArgs
	if ack.hasError {
		inputArgs = ack.inputArgs[1:]
	}
	values, err := decode(inputArgs...)
	if err != nil {
		s.onError(wrapInternalError(err))
		return
	}

	if len(values) == len(inputArgs) {
		for i, v := range values {
			if inputArgs[i].Kind() != reflect.Ptr && v.Kind() == reflect.Ptr {
				values[i] = v.Elem()
			}
		}
	} else {
		s.onError(fmt.Errorf("sio: onAck: invalid number of arguments"))
		return
	}

	err = ack.call(values...)
	if err != nil {
		s.onError(wrapInternalError(err))
		return
	}
}

func (s *clientSocket) Emit(eventName string, v ...any) {
	s.emit(eventName, 0, false, false, v...)
}

func (s *clientSocket) emit(
	eventName string,
	timeout time.Duration,
	volatile, fromQueue bool,
	v ...any,
) {
	header := parser.PacketHeader{
		Type:      parser.PacketTypeEvent,
		Namespace: s.namespace,
	}

	if IsEventReservedForClient(eventName) {
		panic(fmt.Errorf("sio: Emit: attempted to emit a reserved event: `%s`", eventName))
	}

	if eventName != "" {
		v = append([]any{eventName}, v...)
	}

	if s.config.Retries > 0 && !fromQueue && !volatile {
		s.packetQueue.addToQueue(&header, v)
		return
	}

	f := v[len(v)-1]
	rt := reflect.TypeOf(f)
	if f != nil && rt.Kind() == reflect.Func {
		ackID := s.registerAckHandler(f, timeout)
		header.ID = &ackID
		v = v[:len(v)-1]
	}

	buffers, err := s.parser.Encode(&header, &v)
	if err != nil {
		s.onError(wrapInternalError(err))
		return
	}

	s.sendBuffers(volatile, false, header.ID, buffers...)
}

// 0 as the timeout argument means there is no timeout.
func (s *clientSocket) registerAckHandler(f any, timeout time.Duration) (id uint64) {
	if timeout == 0 && s.config.AckTimeout > 0 {
		timeout = s.config.AckTimeout
	}
	id = s.nextAckID()
	s.debug.Log("Registering ack with ID", id)
	if timeout == 0 {
		s.acksMu.Lock()
		h, err := newAckHandler(f, false)
		if err != nil {
			panic(err)
		}
		s.acks[id] = h
		s.acksMu.Unlock()
		return
	}

	h, err := newAckHandlerWithTimeout(f, timeout, func() {
		s.debug.Log("Timeout occured for ack with ID", id, "timeout", timeout)
		s.acksMu.Lock()
		delete(s.acks, id)
		s.acksMu.Unlock()

		remove := func(slice []sendBufferItem, s int) []sendBufferItem {
			return append(slice[:s], slice[s+1:]...)
		}

		s.sendBufferMu.Lock()
		for i, packet := range s.sendBuffer {
			if packet.ackID != nil && *packet.ackID == id {
				s.debug.Log("Removing packet with ack ID", id)
				s.sendBuffer = remove(s.sendBuffer, i)
			}
		}
		s.sendBufferMu.Unlock()
	})
	if err != nil {
		panic(err)
	}

	s.acksMu.Lock()
	s.acks[id] = h
	s.acksMu.Unlock()
	return
}

func (s *clientSocket) nextAckID() uint64 {
	s.acksMu.Lock()
	defer s.acksMu.Unlock()
	id := s.ackID
	s.ackID++
	return id
}

func (s *clientSocket) Timeout(timeout time.Duration) Emitter {
	return Emitter{
		socket:  s,
		timeout: timeout,
	}
}

func (s *clientSocket) Volatile() Emitter {
	return Emitter{
		socket:   s,
		volatile: true,
	}
}

func (s *clientSocket) sendControlPacket(typ parser.PacketType, v any) {
	header := parser.PacketHeader{
		Type:      typ,
		Namespace: s.namespace,
	}

	var (
		buffers [][]byte
		err     error
	)

	buffers, err = s.parser.Encode(&header, v)
	if err != nil {
		s.onError(wrapInternalError(err))
		return
	}
	s.sendBuffers(false, true, nil, buffers...)
}

func (s *clientSocket) sendAckPacket(id uint64, values []reflect.Value) {
	header := parser.PacketHeader{
		Type:      parser.PacketTypeAck,
		Namespace: s.namespace,
		ID:        &id,
	}

	v := make([]any, len(values))

	for i := range values {
		if values[i].CanInterface() {
			v[i] = values[i].Interface()
		} else {
			s.onError(fmt.Errorf("sio: sendAck: CanInterface must be true"))
			return
		}
	}

	buffers, err := s.parser.Encode(&header, &v)
	if err != nil {
		s.onError(wrapInternalError(err))
		return
	}

	s.sendBuffers(false, false, header.ID, buffers...)
}

func (s *clientSocket) _sendBuffers(volatile, forceSend bool, ackID *uint64, buffers ...[]byte) {
	if len(buffers) > 0 {
		packets := make([]*eioparser.Packet, len(buffers))
		buf := buffers[0]
		buffers = buffers[1:]

		var err error
		packets[0], err = eioparser.NewPacket(eioparser.PacketTypeMessage, false, buf)
		if err != nil {
			s.onError(wrapInternalError(err))
			return
		}

		for i, attachment := range buffers {
			packets[i+1], err = eioparser.NewPacket(eioparser.PacketTypeMessage, true, attachment)
			if err != nil {
				s.onError(wrapInternalError(err))
				return
			}
		}

		s.stateMu.RLock()
		sendImmediately := s.state == clientSocketConnStateConnected || s.state == clientSocketConnStateConnectPending
		s.stateMu.RUnlock()
		if sendImmediately || forceSend {
			s.manager.packet(packets...)
		} else if !volatile {
			s.sendBufferMu.Lock()
			buffers := make([]sendBufferItem, len(packets))
			for i := range buffers {
				buffers[i] = sendBufferItem{
					ackID:  ackID,
					packet: packets[i],
				}
			}
			s.sendBuffer = append(s.sendBuffer, buffers...)
			s.sendBufferMu.Unlock()
		} else {
			s.debug.Log("Packet is discarded")
		}
	}
}

func (s *clientSocket) onError(err error) {
	// In original socket.io, errors are only emitted on manager.
	s.manager.onError(err)
}

// Called upon forced client/server side disconnections,
// this method ensures the `Client` (manager on original socket.io implementation)
// stops tracking us and that reconnections don't get triggered for this.
func (s *clientSocket) destroy() {
	s.deregisterSubEvents()
	s.manager.destroy(s)
}

func (s *clientSocket) onClose(reason Reason) {
	s.debug.Log("Going to close the socket. Reason", reason)

	s.stateMu.Lock()
	s.state = clientSocketConnStateDisconnected
	s.stateMu.Unlock()
	s.setID("")
	s.disconnectHandlers.forEach(func(handler *ClientSocketDisconnectFunc) { (*handler)(reason) }, true)
}
