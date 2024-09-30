package adapter

import (
	"encoding/json"
	"time"

	mapset "github.com/deckarep/golang-set/v2"
	"github.com/karagenc/socket.io-go/parser"
)

type (
	// A public ID, sent by the server at the beginning of
	// the Socket.IO session and which can be used for private messaging.
	SocketID string

	Room string

	// A private ID, sent by the server at the beginning of
	// the Socket.IO session and used for connection state recovery upon reconnection.
	PrivateSessionID string
)

type (
	Creator func(socketStore SocketStore, parserCreator parser.Creator) Adapter

	Adapter interface {
		ServerCount() int
		Close()

		AddAll(sid SocketID, rooms []Room)
		Delete(sid SocketID, room Room)
		DeleteAll(sid SocketID)

		Broadcast(header *parser.PacketHeader, v []any, opts *BroadcastOptions)

		// The return value 'sids' is a thread safe mapset.Set.
		Sockets(rooms mapset.Set[Room]) (sids mapset.Set[SocketID])
		// The return value 'rooms' is a thread safe mapset.Set.
		SocketRooms(sid SocketID) (rooms mapset.Set[Room], ok bool)

		FetchSockets(opts *BroadcastOptions) (sockets []Socket)

		AddSockets(opts *BroadcastOptions, rooms ...Room)
		DelSockets(opts *BroadcastOptions, rooms ...Room)
		DisconnectSockets(opts *BroadcastOptions, close bool)

		ServerSideEmit(header *parser.PacketHeader, v []any)

		// Save the client session in order to restore it upon reconnection.
		PersistSession(session *SessionToPersist)

		// Restore the session and find the packets that were missed by the client.
		//
		// ok returns false when there is no session or the session has expired.
		RestoreSession(pid PrivateSessionID, offset string) (session *SessionToPersist, ok bool)
	}
)

type (
	SessionToPersist struct {
		SID SocketID
		PID PrivateSessionID

		Rooms []Room

		MissedPackets []*PersistedPacket
	}

	PersistedPacket struct {
		ID        string
		EmittedAt time.Time
		Opts      *BroadcastOptions

		Header      *parser.PacketHeader
		Data        []any
		EncodedData [][]byte
	}
)

func (p *PersistedPacket) HasExpired(maxDisconnectDuration time.Duration) bool {
	return time.Now().Before(p.EmittedAt.Add(maxDisconnectDuration))
}

func (s SessionToPersist) MarshalBinary() ([]byte, error) {
	return json.Marshal(struct {
		SID   SocketID
		PID   PrivateSessionID
		Rooms []Room
	}{
		SID:   s.SID,
		PID:   s.PID,
		Rooms: s.Rooms,
	})
}

func (s *SessionToPersist) UnmarshalBinary(data []byte) error {
	var tmp struct {
		SID   SocketID
		PID   PrivateSessionID
		Rooms []Room
	}
	err := json.Unmarshal(data, &tmp)
	if err != nil {
		return err
	}

	s.SID = tmp.SID
	s.PID = tmp.PID
	s.Rooms = tmp.Rooms
	return nil
}
