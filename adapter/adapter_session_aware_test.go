package adapter

import (
	"regexp"
	"testing"
	"time"

	"github.com/anfin21/socket.io/parser"
	jsonparser "github.com/anfin21/socket.io/parser/json"
	"github.com/anfin21/socket.io/parser/json/serializer/stdjson"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPersistAndRestoreSession(t *testing.T) {
	adapter := newTestSessionAwareAdapter(100*time.Second, 0)
	adapter.AddAll("s1", []Room{"r1"})
	store := adapter.sockets.(*TestSocketStore)
	store.Set(NewTestSocket("s1"))

	adapter.PersistSession(&SessionToPersist{
		SID:   "s1",
		PID:   "p1",
		Rooms: []Room{"r1", "r2"},
	})

	header := parser.PacketHeader{
		Namespace: "/",
		Type:      parser.PacketTypeEvent,
	}
	opts := NewBroadcastOptions()
	offset := ""
	v := []any{"123"}

	store.sendBuffers = func(sid SocketID, buffers [][]byte) (ok bool) {
		assert.Equal(t, SocketID("s1"), sid)

		// Yank the offset with a regex.
		re := regexp.MustCompile(`.*".*".*"(.*)"`)
		_offset := re.FindStringSubmatch(string(buffers[0]))
		t.Logf("'%s'", _offset[1])
		offset = _offset[1]
		return true
	}

	adapter.Broadcast(&header, v, opts)

	session, ok := adapter.RestoreSession("p1", offset)
	require.True(t, ok)
	require.Equal(t, SocketID("s1"), session.SID)
	require.Equal(t, PrivateSessionID("p1"), session.PID)
	require.Equal(t, 0, len(session.MissedPackets))
}

func TestRestoreMissedPackets(t *testing.T) {
	adapter := newTestSessionAwareAdapter(100*time.Second, 0)
	adapter.AddAll("s1", []Room{"r1"})
	store := adapter.sockets.(*TestSocketStore)
	store.Set(NewTestSocket("s1"))

	adapter.PersistSession(&SessionToPersist{
		SID:   "s1",
		PID:   "p1",
		Rooms: []Room{"r1", "r2"},
	})

	offset := ""
	store.sendBuffers = func(sid SocketID, buffers [][]byte) (ok bool) {
		assert.Equal(t, SocketID("s1"), sid)

		// Do this if this is the first broadcasted packet.
		if offset == "" {
			// Yank the offset with a regex.
			re := regexp.MustCompile(`.*".*".*"(.*)"`)
			_offset := re.FindStringSubmatch(string(buffers[0]))
			t.Logf("'%s'", _offset[1])
			offset = _offset[1]
		}
		return true
	}

	header := parser.PacketHeader{
		Namespace: "/",
		Type:      parser.PacketTypeEvent,
	}
	opts := NewBroadcastOptions()
	v := []any{"hello"}
	adapter.Broadcast(&header, v, opts)

	header = parser.PacketHeader{
		Namespace: "/",
		Type:      parser.PacketTypeEvent,
	}
	opts = NewBroadcastOptions()
	v = []any{"all"}
	adapter.Broadcast(&header, v, opts)

	header = parser.PacketHeader{
		Namespace: "/",
		Type:      parser.PacketTypeEvent,
	}
	opts = NewBroadcastOptions()
	opts.Rooms.Add("r1")
	v = []any{"room"}
	adapter.Broadcast(&header, v, opts)

	header = parser.PacketHeader{
		Namespace: "/",
		Type:      parser.PacketTypeEvent,
	}
	opts = NewBroadcastOptions()
	opts.Except.Add("r2")
	v = []any{"except"}
	adapter.Broadcast(&header, v, opts)

	header = parser.PacketHeader{
		Namespace: "/",
		Type:      parser.PacketTypeEvent,
	}
	opts = NewBroadcastOptions()
	opts.Except.Add("r3")
	v = []any{"no except"}
	adapter.Broadcast(&header, v, opts)

	var id uint64 = 1234
	header = parser.PacketHeader{
		Namespace: "/",
		Type:      parser.PacketTypeEvent,
		ID:        &id,
	}
	opts = NewBroadcastOptions()
	v = []any{"with ack"}
	adapter.Broadcast(&header, v, opts)

	header = parser.PacketHeader{
		Namespace: "/",
		Type:      parser.PacketTypeAck,
	}
	opts = NewBroadcastOptions()
	v = []any{"ack type"}
	adapter.Broadcast(&header, v, opts)

	session, ok := adapter.RestoreSession("p1", offset)
	require.True(t, ok)
	require.Equal(t, SocketID("s1"), session.SID)
	require.Equal(t, PrivateSessionID("p1"), session.PID)

	require.Equal(t, 3, len(session.MissedPackets))
	require.Equal(t, 2, len(session.MissedPackets[0].Data))

	require.Equal(t, "all", session.MissedPackets[0].Data[0])
	require.Equal(t, "room", session.MissedPackets[1].Data[0])
	require.Equal(t, "no except", session.MissedPackets[2].Data[0])
}

func TestUnknownSession(t *testing.T) {
	adapter := newTestSessionAwareAdapter(100*time.Second, 0)
	adapter.AddAll("s1", []Room{"r1"})
	store := adapter.sockets.(*TestSocketStore)
	store.Set(NewTestSocket("s1"))

	_, ok := adapter.RestoreSession("p1", "snfskjfnekwjnfw")
	require.False(t, ok)
}

func TestUnknownOffset(t *testing.T) {
	adapter := newTestSessionAwareAdapter(100*time.Second, 0)
	adapter.AddAll("s1", []Room{"r1"})
	store := adapter.sockets.(*TestSocketStore)
	store.Set(NewTestSocket("s1"))

	adapter.PersistSession(&SessionToPersist{
		SID:   "s1",
		PID:   "p1",
		Rooms: []Room{"r1", "r2"},
	})

	_, ok := adapter.RestoreSession("p1", "snfskjfnekwjnfw")
	require.False(t, ok)
}

func TestCleaner(t *testing.T) {
	adapter := newTestSessionAwareAdapter(500*time.Millisecond, 50*time.Millisecond)
	adapter.AddAll("s1", []Room{"r1"})
	store := adapter.sockets.(*TestSocketStore)
	store.Set(NewTestSocket("s1"))

	adapter.PersistSession(&SessionToPersist{
		SID:   "s1",
		PID:   "p1",
		Rooms: []Room{"r1", "r2"},
	})

	header := parser.PacketHeader{
		Namespace: "/",
		Type:      parser.PacketTypeEvent,
	}
	opts := NewBroadcastOptions()
	offset := ""
	v := []any{"123"}

	store.sendBuffers = func(sid SocketID, buffers [][]byte) (ok bool) {
		assert.Equal(t, SocketID("s1"), sid)

		// Yank the offset with a regex.
		re := regexp.MustCompile(`.*".*".*"(.*)"`)
		_offset := re.FindStringSubmatch(string(buffers[0]))
		t.Logf("'%s'", _offset[1])
		offset = _offset[1]
		return true
	}

	adapter.Broadcast(&header, v, opts)

	session, ok := adapter.RestoreSession("p1", offset)
	require.True(t, ok)
	require.Equal(t, SocketID("s1"), session.SID)
	require.Equal(t, PrivateSessionID("p1"), session.PID)
	require.Equal(t, 0, len(session.MissedPackets))

	// Ensure at least 500 millisecond passes
	time.Sleep(600 * time.Millisecond)
	_, ok = adapter.RestoreSession("p1", offset)
	require.False(t, ok)
}

func TestSessionExpiration(t *testing.T) {
	adapter := newTestSessionAwareAdapter(1*time.Millisecond, 0)
	adapter.AddAll("s1", []Room{"r1"})
	store := adapter.sockets.(*TestSocketStore)
	store.Set(NewTestSocket("s1"))

	adapter.PersistSession(&SessionToPersist{
		SID:   "s1",
		PID:   "p1",
		Rooms: []Room{"r1", "r2"},
	})

	header := parser.PacketHeader{
		Namespace: "/",
		Type:      parser.PacketTypeEvent,
	}
	opts := NewBroadcastOptions()
	offset := ""
	v := []any{"123"}

	store.sendBuffers = func(sid SocketID, buffers [][]byte) (ok bool) {
		assert.Equal(t, SocketID("s1"), sid)

		// Yank the offset with a regex.
		re := regexp.MustCompile(`.*".*".*"(.*)"`)
		_offset := re.FindStringSubmatch(string(buffers[0]))
		t.Logf("'%s'", _offset[1])
		offset = _offset[1]
		return true
	}

	adapter.Broadcast(&header, v, opts)

	// Ensure at least 1 millisecond passes
	time.Sleep(time.Millisecond * 2)
	_, ok := adapter.RestoreSession("p1", offset)
	require.False(t, ok)
}

func TestSessionCopy(t *testing.T) {
	adapter := newTestSessionAwareAdapter(100*time.Second, 0)
	adapter.AddAll("s1", []Room{"r1"})
	store := adapter.sockets.(*TestSocketStore)
	store.Set(NewTestSocket("s1"))

	originalSession := &SessionToPersist{
		SID:   "s1",
		PID:   "p1",
		Rooms: []Room{"r1", "r2"},
	}
	adapter.PersistSession(originalSession)

	header := parser.PacketHeader{
		Namespace: "/",
		Type:      parser.PacketTypeEvent,
	}
	opts := NewBroadcastOptions()
	offset := ""
	v := []any{"123"}

	store.sendBuffers = func(sid SocketID, buffers [][]byte) (ok bool) {
		assert.Equal(t, SocketID("s1"), sid)

		// Yank the offset with a regex.
		re := regexp.MustCompile(`.*".*".*"(.*)"`)
		_offset := re.FindStringSubmatch(string(buffers[0]))
		t.Logf("'%s'", _offset[1])
		offset = _offset[1]
		return true
	}

	adapter.Broadcast(&header, v, opts)

	persistedSession, ok := adapter.RestoreSession("p1", offset)
	require.True(t, ok)
	// Session should be copied.
	require.True(t, originalSession != persistedSession)
}

func newTestSessionAwareAdapter(maxDisconnectionDuration, cleanerDuration time.Duration) *sessionAwareAdapter {
	socketStore := NewTestSocketStore()
	parserCreator := jsonparser.NewCreator(0, stdjson.New())
	inMemoryAdapter := NewInMemoryAdapterCreator()(socketStore, parserCreator).(*inMemoryAdapter)

	return newSessionAwareAdapter(
		inMemoryAdapter,
		maxDisconnectionDuration,
		cleanerDuration,
	)
}
