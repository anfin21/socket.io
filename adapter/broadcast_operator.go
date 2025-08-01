package adapter

import (
	"encoding/json"
	"fmt"
	"reflect"

	mapset "github.com/deckarep/golang-set/v2"

	"github.com/anfin21/socket.io/parser"
)

type (
	BroadcastOperator struct {
		nsp     string
		adapter Adapter

		rooms       mapset.Set[Room]
		exceptRooms mapset.Set[Room]
		flags       BroadcastFlags

		isEventReserved func(string) bool
	}

	BroadcastOptions struct {
		Rooms  mapset.Set[Room]
		Except mapset.Set[Room]
		Flags  BroadcastFlags
	}

	BroadcastFlags struct {
		// This flag is unused at the moment, but for compatibility with the socket.io API, it stays here.
		Compress bool
		Local    bool
	}
)

func NewBroadcastOptions() *BroadcastOptions {
	return &BroadcastOptions{
		Rooms:  mapset.NewSet[Room](),
		Except: mapset.NewSet[Room](),
	}
}

func NewBroadcastOperator(
	nsp string,
	adapter Adapter,
	isEventReserved func(string) bool,
) *BroadcastOperator {
	return &BroadcastOperator{
		nsp:             nsp,
		adapter:         adapter,
		rooms:           mapset.NewSet[Room](),
		exceptRooms:     mapset.NewSet[Room](),
		isEventReserved: isEventReserved,
	}
}

// Emits an event to all choosen clients.
func (b *BroadcastOperator) Emit(eventName string, _v ...any) {
	header := &parser.PacketHeader{
		Type:      parser.PacketTypeEvent,
		Namespace: b.nsp,
	}

	if b.isEventReserved(eventName) {
		panic(fmt.Errorf("sio: BroadcastOperator.Emit: attempted to emit a reserved event: `%s`", eventName))
	}

	// One extra space for eventName,
	// the other for ID (see the Broadcast method of sessionAwareAdapter)
	v := make([]any, 0, len(_v)+2)
	v = append(v, eventName)
	v = append(v, _v...)

	f := v[len(v)-1]
	rt := reflect.TypeOf(f)
	if f != nil && rt.Kind() == reflect.Func {
		panic(fmt.Errorf("sio: BroadcastOperator.Emit: callbacks are not supported when broadcasting"))
	}

	opts := NewBroadcastOptions()
	opts.Rooms = b.rooms
	opts.Except = b.exceptRooms
	opts.Flags = b.flags
	b.adapter.Broadcast(header, v, opts)
}

// Sets a modifier for a subsequent event emission that the event
// will only be broadcast to clients that have joined the given room.
//
// To emit to multiple rooms, you can call To several times.
func (b *BroadcastOperator) To(room ...Room) *BroadcastOperator {
	n := *b
	n.rooms = b.rooms.Clone()
	for _, r := range room {
		n.rooms.Add(Room(r))
	}
	return &n
}

// Alias of To(...)
func (b *BroadcastOperator) In(room ...Room) *BroadcastOperator {
	return b.To(room...)
}

// Sets a modifier for a subsequent event emission that the event
// will only be broadcast to clients that have not joined the given rooms.
func (b *BroadcastOperator) Except(room ...Room) *BroadcastOperator {
	n := *b
	n.exceptRooms = b.exceptRooms.Clone()
	for _, r := range room {
		n.exceptRooms.Add(Room(r))
	}
	return &n
}

// Compression flag is unused at the moment, thus setting this will have no effect on compression.
func (b *BroadcastOperator) Compress(compress bool) *BroadcastOperator {
	n := *b
	n.flags.Compress = compress
	return &n
}

// Sets a modifier for a subsequent event emission that the event data will only be broadcast to the current node (when scaling to multiple nodes).
//
// See: https://socket.io/docs/v4/using-multiple-nodes
func (b *BroadcastOperator) Local() *BroadcastOperator {
	n := *b
	n.flags.Local = true
	return &n
}

// Returns the matching socket instances. This method works across a cluster of several Socket.IO servers.
func (b *BroadcastOperator) FetchSockets() []Socket {
	opts := NewBroadcastOptions()
	opts.Rooms = b.rooms.Clone()
	opts.Except = b.exceptRooms.Clone()
	opts.Flags = b.flags
	return b.adapter.FetchSockets(opts)
}

// Makes the matching socket instances join the specified rooms.
func (b *BroadcastOperator) SocketsJoin(room ...Room) {
	opts := NewBroadcastOptions()
	opts.Rooms = b.rooms.Clone()
	opts.Except = b.exceptRooms.Clone()
	opts.Flags = b.flags

	b.adapter.AddSockets(opts, room...)
}

// Makes the matching socket instances leave the specified rooms.
func (b *BroadcastOperator) SocketsLeave(room ...Room) {
	opts := NewBroadcastOptions()
	opts.Rooms = b.rooms.Clone()
	opts.Except = b.exceptRooms.Clone()
	opts.Flags = b.flags

	b.adapter.DelSockets(opts, room...)
}

// Makes the matching socket instances disconnect from the namespace.
//
// If value of close is true, closes the underlying connection. Otherwise, it just disconnects the namespace.
func (b *BroadcastOperator) DisconnectSockets(close bool) {
	opts := NewBroadcastOptions()
	opts.Rooms = b.rooms.Clone()
	opts.Except = b.exceptRooms.Clone()
	opts.Flags = b.flags

	b.adapter.DisconnectSockets(opts, close)
}

func (b BroadcastOptions) MarshalBinary() ([]byte, error) {
	return json.Marshal(b)
}

func (b *BroadcastOptions) UnmarshalJSON(data []byte) error {
	var optsJson struct {
		Rooms  []Room
		Except []Room
		Flags  BroadcastFlags
	}
	err := json.Unmarshal(data, &optsJson)
	if err != nil {
		return err
	}

	b.Flags = optsJson.Flags
	b.Rooms = mapset.NewSet[Room](optsJson.Rooms...)
	b.Except = mapset.NewSet[Room](optsJson.Except...)

	return nil
}
