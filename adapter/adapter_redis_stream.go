package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/hhuuson97/socket.io-go/parser"
	"github.com/karagenc/yeast"
	"github.com/redis/go-redis/v9"
	"log"
	"strconv"
	"sync"
	"time"
)

const (
	DEFAULT_STREAM_NAME        = "socket.io"
	DEFAULT_MAX_LEN            = 10000
	DEFAULT_READ_COUNT         = 100
	DEFAULT_SESSION_KEY_PREFIX = "sio:session:"
	DEFAULT_MESSAGE_ID_PREFIX  = "sio:message:"
)

type RedisStreamsAdapterOptions struct {
	StreamName            string
	MaxLength             int64
	ReadCount             int64
	SessionKeyPrefix      string
	MaxDisconnectDuration time.Duration
}

type RedisStreamAdapter struct {
	mu                    sync.Mutex
	rooms                 map[Room]mapset.Set[SocketID]
	sids                  map[SocketID]mapset.Set[Room]
	maxDisconnectDuration time.Duration

	yeaster *yeast.Yeaster

	ctx context.Context

	redisClient redis.Cmdable
	opts        *RedisStreamsAdapterOptions

	sockets SocketStore

	parser parser.Parser
}

type RedisStreamBuffer [][]byte

func (b RedisStreamBuffer) MarshalBinary() ([]byte, error) {
	newBuffers := make([]string, len(b))
	for i, b := range b {
		newBuffers[i] = string(b)
	}
	buffersMarshaled, err := json.Marshal(newBuffers)
	if err != nil {
		log.Fatalf("Error marshaling data to JSON: %v", err)
		return nil, err
	}

	return buffersMarshaled, nil
}

func (b *RedisStreamBuffer) UnmarshalJSON(data []byte) error {
	var buffers []string
	err := json.Unmarshal(data, &buffers)
	if err != nil {
		log.Fatalf("Error marshaling data to JSON: %v", err)
		return err
	}

	buffersUnmarshalled := make([][]byte, len(buffers))
	for i, b := range buffers {
		buffersUnmarshalled[i] = []byte(b)
	}
	*b = buffersUnmarshalled

	return nil
}

type RedisStreamMessage struct {
	SessionId string
	Header    *parser.PacketHeader
	Buffers   [][]byte
	Opts      *BroadcastOptions
	EmittedAt time.Time
}

func (m *RedisStreamMessage) Parse(msg redis.XMessage) error {
	m.SessionId = msg.Values["sessionId"].(string)

	err := json.Unmarshal([]byte(msg.Values["header"].(string)), &m.Header)
	if err != nil {
		log.Fatalf("Error marshaling data to JSON: %v", err)
		return err
	}

	m.Buffers = make([][]byte, 0)
	tmp := []string{}
	err = json.Unmarshal([]byte(msg.Values["buffers"].(string)), &tmp)
	if err != nil {
		log.Fatalf("Error marshaling data to JSON: %v", err)
	}
	for _, d := range tmp {
		m.Buffers = append(m.Buffers, []byte(d))
	}

	err = json.Unmarshal([]byte(msg.Values["opts"].(string)), &m.Opts)
	if err != nil {
		log.Fatalf("Error marshaling data to JSON: %v", err)
		return err
	}

	unixTs, err := strconv.ParseInt(msg.Values["emittedAt"].(string), 10, 64)
	if err != nil {
		log.Fatalf("Error parsing emittedAt: %v", err)
		return err
	}

	m.EmittedAt = time.Unix(unixTs, 0)

	return nil
}

func (m RedisStreamMessage) ToStreamData() map[string]interface{} {
	b := make([]string, 0)
	for _, t := range m.Buffers {
		b = append(b, string(t))
	}

	buffers, err := json.Marshal(b)
	if err != nil {
		fmt.Printf("Error marshaling data to JSON: %v", err)
	}

	return map[string]interface{}{
		"sessionId": m.SessionId,
		"header":    m.Header,
		"buffers":   buffers,
		"opts":      m.Opts,
		"emittedAt": m.EmittedAt.UTC().Unix(),
	}
}

func NewRedisStreamAdapterCreator(redisClient redis.Cmdable, opts *RedisStreamsAdapterOptions) Creator {
	return func(socketStore SocketStore, parserCreator parser.Creator) Adapter {
		if opts == nil {
			opts = &RedisStreamsAdapterOptions{
				StreamName:       DEFAULT_STREAM_NAME,
				MaxLength:        DEFAULT_MAX_LEN,
				ReadCount:        DEFAULT_READ_COUNT,
				SessionKeyPrefix: DEFAULT_SESSION_KEY_PREFIX,
			}
		}
		if opts.StreamName == "" {
			opts.StreamName = DEFAULT_STREAM_NAME
		}
		if opts.SessionKeyPrefix == "" {
			opts.SessionKeyPrefix = DEFAULT_SESSION_KEY_PREFIX
		}
		if opts.MaxLength == 0 {
			opts.MaxLength = DEFAULT_MAX_LEN
		}
		if opts.ReadCount == 0 {
			opts.ReadCount = DEFAULT_READ_COUNT
		}
		if opts.MaxDisconnectDuration == 0 {
			opts.MaxDisconnectDuration = 2 * time.Minute
		}

		ctx := context.Background()
		packageParser := parserCreator()

		redisStreamAdapter := &RedisStreamAdapter{
			mu:                    sync.Mutex{},
			rooms:                 make(map[Room]mapset.Set[SocketID]),
			sids:                  make(map[SocketID]mapset.Set[Room]),
			maxDisconnectDuration: opts.MaxDisconnectDuration,
			yeaster:               yeast.New(),
			ctx:                   ctx,
			redisClient:           redisClient,
			opts:                  opts,
			sockets:               socketStore,
			parser:                packageParser,
		}

		go func() {
			// Get from beginning
			offset := "$"

			for {
				result, err := redisClient.XRead(ctx, &redis.XReadArgs{
					Streams: []string{opts.StreamName},
					Count:   opts.ReadCount,
					ID:      offset,
				}).Result()
				if err != nil {
					log.Fatalf("Error reading from stream: %v", err)
				}

				for _, stream := range result {
					fmt.Printf("Stream: %s\n", stream.Stream)
					for _, message := range stream.Messages {
						offset = message.ID
						msg := &RedisStreamMessage{}
						err = msg.Parse(message)
						if err != nil {
							log.Fatalf("Error parsing message: %v", err)
						}

						redisStreamAdapter.apply(msg.Opts, func(socket Socket) {
							redisStreamAdapter.sockets.SendBuffers(socket.ID(), msg.Buffers)
						})
					}
				}
			}
		}()

		return redisStreamAdapter
	}
}

func (a *RedisStreamAdapter) ServerCount() int {
	return 1
}

func (a *RedisStreamAdapter) Close() {

}

func (a *RedisStreamAdapter) AddAll(sid SocketID, rooms []Room) {
	a.mu.Lock()
	defer a.mu.Unlock()

	_, ok := a.sids[sid]
	if !ok {
		a.sids[sid] = mapset.NewThreadUnsafeSet[Room]()
	}

	for _, room := range rooms {
		s := a.sids[sid]
		s.Add(room)

		r, ok := a.rooms[room]
		if !ok {
			r = mapset.NewThreadUnsafeSet[SocketID]()
			a.rooms[room] = r
		}
		if !r.Contains(sid) {
			r.Add(sid)
		}
	}
}

func (a *RedisStreamAdapter) Delete(sid SocketID, room Room) {
	a.mu.Lock()
	defer a.mu.Unlock()

	s, ok := a.sids[sid]
	if ok {
		s.Remove(room)
	}

	a.delete(sid, room)
}

func (a *RedisStreamAdapter) delete(sid SocketID, room Room) {
	r, ok := a.rooms[room]
	if ok {
		r.Remove(sid)
		if r.Cardinality() == 0 {
			delete(a.rooms, room)
		}
	}
}

func (a *RedisStreamAdapter) DeleteAll(sid SocketID) {
	a.mu.Lock()
	defer a.mu.Unlock()

	s, ok := a.sids[sid]
	if !ok {
		return
	}

	s.Each(func(room Room) bool {
		a.delete(sid, room)
		return false
	})

	delete(a.sids, sid)
}

func (a *RedisStreamAdapter) Broadcast(header *parser.PacketHeader, v []any, opts *BroadcastOptions) {
	isEventPacket := header.Type == parser.PacketTypeEvent
	withoutAcknowledgement := header.ID == nil
	isIncludedSession := isEventPacket && withoutAcknowledgement
	var sessionId string
	if isIncludedSession {
		a.mu.Lock()
		sessionId = a.yeaster.Yeast()
		v = append(v, sessionId)
		a.mu.Unlock()
	}

	buffers, err := a.parser.Encode(header, &v)
	if err != nil {
		panic(fmt.Errorf("sio: %w", err))
	}

	buf := bytes.Buffer{}
	e := a.parser.JSONSerializer().NewEncoder(&buf)
	err = e.Encode(&v)
	if err != nil {
		panic(fmt.Errorf("sio: %w", err))
	}

	values := RedisStreamMessage{
		SessionId: sessionId,
		Header:    header,
		Buffers:   buffers,
		Opts:      opts,
		EmittedAt: time.Now(),
	}.ToStreamData()

	messageID, err := a.redisClient.XAdd(a.ctx, &redis.XAddArgs{
		Stream: a.opts.StreamName,
		Values: values,
		MaxLen: a.opts.MaxLength,
	}).Result()
	if err != nil {
		log.Fatalf("Error adding message to Redis: %v", err)
	} else {
		log.Printf("Added message to Redis: %v", messageID)
	}

	if isIncludedSession {
		err = a.redisClient.Set(a.ctx, fmt.Sprintf("%s%s", DEFAULT_MESSAGE_ID_PREFIX, sessionId), messageID, a.opts.MaxDisconnectDuration).Err()
		if err != nil {
			log.Fatalf("Error setting message id: %v", err)
		}
	}
}

// The return value 'sids' must be a thread safe mapset.Set.
func (a *RedisStreamAdapter) Sockets(rooms mapset.Set[Room]) (sids mapset.Set[SocketID]) {
	a.mu.Lock()
	sids = mapset.NewSet[SocketID]()
	opts := NewBroadcastOptions()
	opts.Rooms = rooms
	a.mu.Unlock()

	a.apply(opts, func(socket Socket) {
		sids.Add(socket.ID())
	})
	return
}

// The return value 'rooms' must be a thread safe mapset.Set.
func (a *RedisStreamAdapter) SocketRooms(sid SocketID) (rooms mapset.Set[Room], ok bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	s, ok := a.sids[sid]
	if !ok {
		return nil, false
	}

	rooms = mapset.NewSet[Room]()
	s.Each(func(room Room) bool {
		rooms.Add(room)
		return false
	})
	return
}

func (a *RedisStreamAdapter) FetchSockets(opts *BroadcastOptions) (sockets []Socket) {
	a.apply(opts, func(socket Socket) {
		sockets = append(sockets, socket)
	})
	return
}

func (a *RedisStreamAdapter) AddSockets(opts *BroadcastOptions, rooms ...Room) {
	a.apply(opts, func(socket Socket) {
		socket.Join(rooms...)
	})
}

func (a *RedisStreamAdapter) DelSockets(opts *BroadcastOptions, rooms ...Room) {
	a.apply(opts, func(socket Socket) {
		for _, room := range rooms {
			socket.Leave(room)
		}
	})
}

func (a *RedisStreamAdapter) DisconnectSockets(opts *BroadcastOptions, close bool) {
	a.apply(opts, func(socket Socket) {
		socket.Disconnect(close)
	})
}

func (a *RedisStreamAdapter) apply(opts *BroadcastOptions, callback func(socket Socket)) {
	a.mu.Lock()

	exceptSids := a.computeExceptSids(opts.Except)

	// If a room was specificed in opts.Rooms,
	// we only use sockets in those rooms.
	// Otherwise (within else), any socket will be used.
	if opts.Rooms.Cardinality() > 0 {
		ids := mapset.NewThreadUnsafeSet[SocketID]()
		opts.Rooms.Each(func(room Room) bool {
			r, ok := a.rooms[room]
			if !ok {
				return false
			}

			r.Each(func(sid SocketID) bool {
				if ids.Contains(sid) || exceptSids.Contains(sid) {
					return false
				}
				socket, ok := a.sockets.Get(sid)
				if ok {
					a.mu.Unlock()
					callback(socket)
					a.mu.Lock()
					ids.Add(sid)
				}
				return false
			})
			return false
		})
	} else {
		for sid := range a.sids {
			if exceptSids.Contains(sid) {
				continue
			}
			socket, ok := a.sockets.Get(sid)
			if ok {
				a.mu.Unlock()
				callback(socket)
				a.mu.Lock()
			}
		}
	}
	a.mu.Unlock()
}

// Beware that the return value 'exceptSids' is thread unsafe.
func (a *RedisStreamAdapter) computeExceptSids(exceptRooms mapset.Set[Room]) (exceptSids mapset.Set[SocketID]) {
	exceptSids = mapset.NewThreadUnsafeSet[SocketID]()

	if exceptRooms.Cardinality() > 0 {
		exceptRooms.Each(func(room Room) bool {
			r, ok := a.rooms[room]
			if ok {
				r.Each(func(sid SocketID) bool {
					exceptSids.Add(sid)
					return false
				})
			}
			return false
		})
	}
	return
}

func (a *RedisStreamAdapter) ServerSideEmit(header *parser.PacketHeader, v []any) {}

func (a *RedisStreamAdapter) PersistSession(session *SessionToPersist) {
	err := a.redisClient.Set(a.ctx, fmt.Sprintf("%s%s", DEFAULT_SESSION_KEY_PREFIX, session.PID), session, a.maxDisconnectDuration).Err()
	if err != nil {
		log.Fatalf("Error persisting session: %v", err)
	}
}

func (a *RedisStreamAdapter) RestoreSession(pid PrivateSessionID, offset string) (session *SessionToPersist, ok bool) {
	session = &SessionToPersist{}
	err := a.redisClient.Get(a.ctx, fmt.Sprintf("%s%s", DEFAULT_SESSION_KEY_PREFIX, pid)).Scan(session)
	if err != nil {
		return nil, false
	}

	var messageID string
	err = a.redisClient.Get(a.ctx, fmt.Sprintf("%s%s", DEFAULT_MESSAGE_ID_PREFIX, offset)).Scan(&messageID)
	if err != nil || messageID == "" {
		return nil, false
	}

	messages, err := a.redisClient.XRange(a.ctx, a.opts.StreamName, messageID, "+").Result()
	if err != nil {
		log.Fatalf("Error reading from stream: %v", err)
	}

	var missedPackets []*PersistedPacket
	for _, message := range messages {
		offset = message.ID
		msg := &RedisStreamMessage{}
		err = msg.Parse(message)
		if err != nil {
			log.Fatalf("Error parsing message: %v", err)
		}

		if shouldIncludePacket(session.Rooms, msg.Opts) {
			missedPackets = append(missedPackets, &PersistedPacket{
				ID:          msg.SessionId,
				EmittedAt:   msg.EmittedAt,
				Opts:        msg.Opts,
				Header:      msg.Header,
				EncodedData: msg.Buffers,
			})
		}
	}

	session.MissedPackets = missedPackets

	return session, true
}
