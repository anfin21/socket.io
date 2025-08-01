package sio

import (
	"testing"
	"time"

	"github.com/anfin21/socket.io/adapter"
	"github.com/anfin21/socket.io/internal/sync"
	"github.com/anfin21/socket.io/internal/utils"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/stretchr/testify/assert"
)

func TestNamespace(t *testing.T) {
	t.Run("should fire a `connect` event", func(t *testing.T) {
		io, _, manager, close := newTestServerAndClient(t, nil, nil)
		socket := manager.Socket("/", nil)
		tw := utils.NewTestWaiter(1)

		io.OnConnection(func(socket ServerSocket) {
			tw.Done()
		})
		socket.Connect()
		tw.WaitTimeout(t, utils.DefaultTestWaitTimeout)
		close()
	})

	t.Run(`should be able to equivalently start with "" or "/" on server`, func(t *testing.T) {
		io, _, manager, close := newTestServerAndClient(t, nil, nil)
		socket := manager.Socket("/", nil)
		tw := utils.NewTestWaiterString()
		tw.Add("/abc")
		tw.Add("")

		io.Of("/abc").OnConnection(func(socket ServerSocket) {
			tw.Done("/abc")
		})
		io.Of("").OnConnection(func(socket ServerSocket) {
			tw.Done("")
		})

		manager.Socket("/abc", nil).Connect()
		socket.Connect()
		tw.WaitTimeout(t, utils.DefaultTestWaitTimeout)
		close()
	})

	t.Run(`should be equivalent for "" and "/" on client`, func(t *testing.T) {
		io, _, manager, close := newTestServerAndClient(t, nil, nil)
		socket := manager.Socket("", nil)
		tw := utils.NewTestWaiter(1)

		io.Of("/").OnConnection(func(socket ServerSocket) {
			tw.Done()
		})
		socket.Connect()

		tw.WaitTimeout(t, utils.DefaultTestWaitTimeout)
		close()
	})

	t.Run("should work with `of` and many sockets", func(t *testing.T) {
		io, _, manager, close := newTestServerAndClient(t, nil, nil)
		socket := manager.Socket("/", nil)
		tw := utils.NewTestWaiterString()
		tw.Add("/chat")
		tw.Add("/news")
		tw.Add("/")

		io.Of("/chat").OnConnection(func(socket ServerSocket) {
			tw.Done("/chat")
		})
		io.Of("/news").OnConnection(func(socket ServerSocket) {
			tw.Done("/news")
		})
		io.OnConnection(func(socket ServerSocket) {
			tw.Done("/")
		})
		manager.Socket("/chat", nil).Connect()
		manager.Socket("/news", nil).Connect()
		socket.Connect()

		tw.WaitTimeout(t, utils.DefaultTestWaitTimeout)
		close()
	})

	t.Run("should work with `of` second param", func(t *testing.T) {
		io, _, manager, close := newTestServerAndClient(t, nil, nil)
		socket := manager.Socket("/news", nil)
		tw := utils.NewTestWaiter(2)

		io.Of("/news").OnConnection(func(socket ServerSocket) {
			tw.Done()
		})
		io.Of("/news").OnConnection(func(socket ServerSocket) {
			tw.Done()
		})
		socket.Connect()

		tw.WaitTimeout(t, utils.DefaultTestWaitTimeout)
		close()
	})

	t.Run("should disconnect upon transport disconnection", func(t *testing.T) {
		io, _, manager, close := newTestServerAndClient(t, nil, nil)
		tw := utils.NewTestWaiter(1)

		var (
			mu              sync.Mutex
			total           = 0
			totalDisconnect = 0
			s               ServerSocket
		)
		disconnect := func() {
			mu.Lock()
			defer mu.Unlock()
			s.Disconnect(true)
		}
		io.Of("/chat").OnConnection(func(socket ServerSocket) {
			socket.OnDisconnect(func(reason Reason) {
				mu.Lock()
				totalDisconnect++
				totalDisconnect := totalDisconnect
				mu.Unlock()
				if totalDisconnect == 2 {
					tw.Done()
				}
			})
			mu.Lock()
			total++
			total := total
			mu.Unlock()
			if total == 2 {
				disconnect()
			}
		})
		io.Of("/news").OnConnection(func(socket ServerSocket) {
			socket.OnDisconnect(func(reason Reason) {
				mu.Lock()
				totalDisconnect++
				totalDisconnect := totalDisconnect
				mu.Unlock()
				if totalDisconnect == 2 {
					tw.Done()
				}
			})
			mu.Lock()
			s = socket
			total++
			total := total
			mu.Unlock()
			if total == 2 {
				disconnect()
			}
		})
		manager.Socket("/chat", nil).Connect()
		manager.Socket("/news", nil).Connect()

		tw.WaitTimeout(t, utils.DefaultTestWaitTimeout)
		close()
	})

	t.Run("should fire a `disconnecting` event just before leaving all rooms", func(t *testing.T) {
		io, _, manager, close := newTestServerAndClient(t, nil, nil)
		tw := utils.NewTestWaiter(2)

		io.OnConnection(func(socket ServerSocket) {
			socket.Join("a")
			socket.OnDisconnecting(func(reason Reason) {
				rooms := socket.Rooms()
				assert.True(t, rooms.ContainsOne("a"))
				tw.Done()
			})
			socket.OnDisconnect(func(reason Reason) {
				rooms := socket.Rooms()
				assert.False(t, rooms.ContainsOne("a"))
				tw.Done()
			})
			socket.Disconnect(true)
		})
		manager.Socket("/", nil).Connect()

		tw.WaitTimeout(t, utils.DefaultTestWaitTimeout)
		close()
	})

	t.Run("should return error connecting to non-existent namespace", func(t *testing.T) {
		_, _, manager, close := newTestServerAndClient(t, nil, nil)
		tw := utils.NewTestWaiter(1)

		socket := manager.Socket("/doesnotexist", nil)
		socket.OnConnectError(func(err any) {
			assert.NotNil(t, err)
			e := err.(error)
			assert.Contains(t, e.Error(), "namespace '/doesnotexist' was not created and AcceptAnyNamespace was not set")
			tw.Done()
		})
		socket.Connect()

		tw.WaitTimeout(t, utils.DefaultTestWaitTimeout)
		close()
	})

	t.Run("should find all clients in a namespace", func(t *testing.T) {
		io, ts, manager, close := newTestServerAndClient(t, nil, nil)
		manager2 := newTestManager(ts, nil)
		tw := utils.NewTestWaiter(1)
		chatSids := mapset.NewSet[SocketID]()
		total := 0
		mu := sync.Mutex{}

		getSockets := func() {
			sockets := io.Of("/chat").FetchSockets()
			assert.Len(t, sockets, 2)
			mu.Lock()
			for _, socket := range sockets {
				assert.True(t, chatSids.ContainsOne(socket.ID()))
			}
			mu.Unlock()
			tw.Done()
		}

		io.Of("/chat").OnConnection(func(socket ServerSocket) {
			mu.Lock()
			chatSids.Add(socket.ID())
			total++
			total := total
			mu.Unlock()
			if total == 3 {
				getSockets()
			}
		})
		io.Of("/other").OnConnection(func(socket ServerSocket) {
			mu.Lock()
			total++
			total := total
			mu.Unlock()
			if total == 3 {
				getSockets()
			}
		})

		manager.Socket("/chat", nil).Connect()
		manager2.Socket("/chat", nil).Connect()
		manager.Socket("/other", nil).Connect()

		tw.WaitTimeout(t, utils.DefaultTestWaitTimeout)
		close()
	})

	t.Run("should find all clients in a namespace room", func(t *testing.T) {
		io, ts, manager, close := newTestServerAndClient(t, nil, nil)
		manager2 := newTestManager(ts, nil)
		tw := utils.NewTestWaiter(1)
		chatFooSid := SocketID("")
		total := 0
		mu := sync.Mutex{}

		getSockets := func() {
			sockets := io.Of("/chat").In("foo").FetchSockets()
			assert.Len(t, sockets, 1)
			mu.Lock()
			assert.Equal(t, chatFooSid, sockets[0].ID())
			mu.Unlock()
			tw.Done()
		}

		io.Of("/chat").OnConnection(func(socket ServerSocket) {
			mu.Lock()
			if chatFooSid == "" {
				chatFooSid = socket.ID()
				socket.Join("foo")
			} else {
				socket.Join("bar")
			}
			total++
			total := total
			mu.Unlock()
			if total == 3 {
				getSockets()
			}
		})
		io.Of("/other").OnConnection(func(socket ServerSocket) {
			mu.Lock()
			total++
			total := total
			mu.Unlock()
			socket.Join("foo")
			if total == 3 {
				getSockets()
			}
		})

		manager.Socket("/chat", nil).Connect()
		manager2.Socket("/chat", nil).Connect()
		manager.Socket("/other", nil).Connect()

		tw.WaitTimeout(t, utils.DefaultTestWaitTimeout)
		close()
	})

	t.Run("should find all clients across namespace rooms", func(t *testing.T) {
		io, ts, manager, close := newTestServerAndClient(t, nil, nil)
		manager2 := newTestManager(ts, nil)
		tw := utils.NewTestWaiter(1)
		chatSids := mapset.NewSet[SocketID]()
		total := 0
		mu := sync.Mutex{}

		getSockets := func() {
			sockets := io.Of("/chat").FetchSockets()
			assert.Len(t, sockets, 2)
			mu.Lock()
			for _, socket := range sockets {
				assert.True(t, chatSids.ContainsOne(socket.ID()))
			}
			mu.Unlock()
			tw.Done()
		}

		io.Of("/chat").OnConnection(func(socket ServerSocket) {
			mu.Lock()
			if chatSids.Cardinality() == 0 {
				chatSids.Add(socket.ID())
				socket.Join("foo")
			} else {
				chatSids.Add(socket.ID())
				socket.Join("bar")
			}
			total++
			total := total
			mu.Unlock()
			if total == 3 {
				getSockets()
			}
		})
		io.Of("/other").OnConnection(func(socket ServerSocket) {
			mu.Lock()
			total++
			total := total
			mu.Unlock()
			socket.Join("foo")
			if total == 3 {
				getSockets()
			}
		})

		manager.Socket("/chat", nil).Connect()
		manager2.Socket("/chat", nil).Connect()
		manager.Socket("/other", nil).Connect()

		tw.WaitTimeout(t, utils.DefaultTestWaitTimeout)
		close()
	})

	t.Run("should throw on reserved event", func(t *testing.T) {
		io, _, _, close := newTestServerAndClient(t, nil, nil)
		assert.Panics(t, func() {
			io.Emit("connect")
		})
		close()
	})

	t.Run("should close a client without namespace", func(t *testing.T) {
		_, _, manager, close := newTestServerAndClient(t,
			&ServerConfig{
				AcceptAnyNamespace: true,
				ConnectTimeout:     1000 * time.Millisecond,
			},
			nil,
		)
		manager.onNewSocket = func(socket *clientSocket) {
			socket.sendBuffers = func(volatile, forceSend bool, ackID *uint64, buffers ...[]byte) {}
		}
		tw := utils.NewTestWaiter(1)

		socket := manager.Socket("/", nil)
		socket.OnDisconnect(func(reason Reason) {
			tw.Done()
		})
		socket.Connect()

		tw.WaitTimeout(t, utils.DefaultTestWaitTimeout)
		close()
	})

	t.Run("should exclude a specific socket when emitting", func(t *testing.T) {
		io, ts, manager, close := newTestServerAndClient(t,
			&ServerConfig{
				AcceptAnyNamespace: true,
			},
			nil,
		)
		manager2 := newTestManager(ts, nil)
		tw := utils.NewTestWaiter(1)

		socket1 := manager.Socket("/", nil)
		socket2 := manager2.Socket("/", nil)

		socket1.OnEvent("a", func() {
			tw.Done()
		})
		socket2.OnEvent("a", func() {
			t.Fatal("should not happen")
		})
		socket2.OnConnect(func() {
			io.Except(Room(socket2.ID())).Emit("a")
		})
		socket1.Connect()
		socket2.Connect()

		tw.WaitTimeout(t, utils.DefaultTestWaitTimeout)
		close()
	})

	t.Run("should exclude a specific socket when emitting (in a namespace)", func(t *testing.T) {
		io, ts, manager, close := newTestServerAndClient(t,
			nil,
			nil,
		)
		nsp := io.Of("/nsp")
		manager2 := newTestManager(ts, nil)
		tw := utils.NewTestWaiter(1)

		socket1 := manager.Socket("/nsp", nil)
		socket2 := manager2.Socket("/nsp", nil)

		socket1.OnEvent("a", func() {
			tw.Done()
		})
		socket2.OnEvent("a", func() {
			t.Fatal("should not happen")
		})
		socket2.OnConnect(func() {
			nsp.Except(Room(socket2.ID())).Emit("a")
		})
		socket1.Connect()
		socket2.Connect()

		tw.WaitTimeout(t, utils.DefaultTestWaitTimeout)
		close()
	})

	t.Run("should exclude a specific room when emitting", func(t *testing.T) {
		io, ts, manager, close := newTestServerAndClient(t,
			nil,
			nil,
		)
		nsp := io.Of("/nsp")
		manager2 := newTestManager(ts, nil)
		tw := utils.NewTestWaiter(1)

		socket1 := manager.Socket("/nsp", nil)
		socket2 := manager2.Socket("/nsp", nil)

		socket1.OnEvent("a", func() {
			tw.Done()
		})
		socket2.OnEvent("a", func() {
			t.Fatal("should not happen")
		})
		nsp.OnConnection(func(socket ServerSocket) {
			socket.OnEvent("broadcast", func() {
				socket.Join("room1")
				nsp.Except("room1").Emit("a")
			})
		})
		socket1.Connect()
		socket2.Connect()
		socket2.Emit("broadcast")

		tw.WaitTimeout(t, utils.DefaultTestWaitTimeout)
		close()
	})

	t.Run("should emit an 'new_namespace' event", func(t *testing.T) {
		io, _, _, close := newTestServerAndClient(t,
			nil,
			nil,
		)
		tw := utils.NewTestWaiter(1)

		io.OnNewNamespace(func(namespace *Namespace) {
			assert.Equal(t, "/nsp", namespace.Name())
			tw.Done()
		})
		io.Of("/nsp")

		tw.WaitTimeout(t, utils.DefaultTestWaitTimeout)
		close()
	})

	t.Run("emits to a namespace", func(t *testing.T) {
		io, ts, manager, close := newTestServerAndClient(t, nil, nil)
		manager2 := newTestManager(ts, nil)
		socket1 := manager.Socket("/", nil)
		socket2 := manager2.Socket("/", nil)
		socket3 := manager.Socket("/test", nil)
		tw := utils.NewTestWaiterString()
		tw.Add("socket1 a")
		tw.Add("socket2 a")

		socket1.OnEvent("a", func(a string) {
			assert.Equal(t, "b", a)
			tw.Done("socket1 a")
		})
		socket2.OnEvent("a", func(a string) {
			assert.Equal(t, "b", a)
			tw.Done("socket2 a")
		})
		socket3.OnEvent("a", func(a string) {
			t.Fatal("should not happen")
		})

		numSockets := 3
		mu := sync.Mutex{}
		emit := func() {
			io.Emit("a", "b")
		}

		io.OnConnection(func(socket ServerSocket) {
			mu.Lock()
			numSockets--
			numSockets := numSockets
			mu.Unlock()
			if numSockets == 0 {
				emit()
			}
		})
		io.Of("/test").OnConnection(func(socket ServerSocket) {
			mu.Lock()
			numSockets--
			numSockets := numSockets
			mu.Unlock()
			if numSockets == 0 {
				emit()
			}
		})

		socket1.Connect()
		socket2.Connect()
		socket3.Connect()

		tw.WaitTimeout(t, utils.DefaultTestWaitTimeout)
		close()
	})

	t.Run("emits binary data to a namespace", func(t *testing.T) {
		io, ts, manager, close := newTestServerAndClient(t, nil, nil)
		manager2 := newTestManager(ts, nil)
		socket1 := manager.Socket("/", nil)
		socket2 := manager2.Socket("/", nil)
		socket3 := manager.Socket("/test", nil)
		tw := utils.NewTestWaiterString()
		tw.Add("socket1 a")
		tw.Add("socket2 a")

		buf := Binary{0xDE, 0xAD, 0xBE, 0xEF}

		socket1.OnEvent("bin", func(a Binary) {
			assert.Equal(t, buf, a)
			tw.Done("socket1 a")
		})
		socket2.OnEvent("bin", func(a Binary) {
			assert.Equal(t, buf, a)
			tw.Done("socket2 a")
		})
		socket3.OnEvent("bin", func(a Binary) {
			t.Fatal("should not happen")
		})

		numSockets := 3
		mu := sync.Mutex{}
		emit := func() {
			io.Emit("bin", buf)
		}

		io.OnConnection(func(socket ServerSocket) {
			mu.Lock()
			numSockets--
			numSockets := numSockets
			mu.Unlock()
			if numSockets == 0 {
				emit()
			}
		})
		io.Of("/test").OnConnection(func(socket ServerSocket) {
			mu.Lock()
			numSockets--
			numSockets := numSockets
			mu.Unlock()
			if numSockets == 0 {
				emit()
			}
		})

		socket1.Connect()
		socket2.Connect()
		socket3.Connect()

		tw.WaitTimeout(t, utils.DefaultTestWaitTimeout)
		close()
	})

	t.Run("emits to the rest", func(t *testing.T) {
		io, ts, manager, close := newTestServerAndClient(t, nil, nil)
		manager2 := newTestManager(ts, nil)
		socket1 := manager.Socket("/", nil)
		socket2 := manager2.Socket("/", nil)
		socket3 := manager.Socket("/test", nil)
		tw := utils.NewTestWaiterString()
		tw.Add("socket1 a")
		tw.Add("test connection")

		socket1.OnEvent("a", func(a string) {
			assert.Equal(t, "b", a)
			tw.Done("socket1 a")
		})
		socket2.OnEvent("a", func(a string) {
			assert.Equal(t, "b", a)
			t.Fatal("should not happen")
		})
		socket2.Emit("broadcast")
		socket3.OnEvent("a", func(a string) {
			t.Fatal("should not happen")
		})

		io.OnConnection(func(socket ServerSocket) {
			socket.OnEvent("broadcast", func() {
				socket.Broadcast().Emit("a", "b")
			})
		})
		io.Of("/test").OnConnection(func(socket ServerSocket) {
			tw.Done("test connection")
		})

		socket1.Connect()
		socket2.Connect()
		socket3.Connect()

		tw.WaitTimeout(t, utils.DefaultTestWaitTimeout)
		time.Sleep(200 * time.Millisecond)
		close()
	})

	t.Run("emits to rooms", func(t *testing.T) {
		io, ts, manager, close := newTestServerAndClient(t, nil, nil)
		manager2 := newTestManager(ts, nil)
		socket1 := manager.Socket("/", nil)
		socket2 := manager2.Socket("/", nil)
		tw := utils.NewTestWaiterString()
		tw.Add("socket1 a")

		socket1.OnEvent("a", func() {
			tw.Done("socket1 a")
		})
		socket1.Emit("join", "woot")
		socket1.Emit("emit", "woot")
		socket2.OnEvent("a", func() {
			t.Fatal("should not happen")
		})

		io.OnConnection(func(socket ServerSocket) {
			socket.OnEvent("join", func(room string) {
				socket.Join(adapter.Room(room))
			})
			socket.OnEvent("emit", func(room string) {
				io.In(adapter.Room(room)).Emit("a")
			})
		})

		socket1.Connect()
		socket2.Connect()

		tw.WaitTimeout(t, utils.DefaultTestWaitTimeout)
		time.Sleep(200 * time.Millisecond)
		close()
	})

	t.Run("emits to rooms avoiding dupes", func(t *testing.T) {
		io, ts, manager, close := newTestServerAndClient(t, nil, nil)
		manager2 := newTestManager(ts, nil)
		socket1 := manager.Socket("/", nil)
		socket2 := manager2.Socket("/", nil)
		tw := utils.NewTestWaiterString()
		tw.Add("socket1 a")
		tw.Add("socket2 b")

		socket2.OnEvent("a", func() {
			t.Fatal("should not happen")
		})
		socket1.OnEvent("a", func() {
			tw.Done("socket1 a")
		})
		socket2.OnEvent("b", func() {
			tw.Done("socket2 b")
		})

		socket1.Emit("join", "woot")
		socket1.Emit("join", "test")
		socket2.Emit("join", "third", func() {
			socket2.Emit("emit")
		})

		io.OnConnection(func(socket ServerSocket) {
			socket.OnEvent("join", func(room string) {
				socket.Join(adapter.Room(room))
			})
			socket.OnEvent("join", func(room string, ack func()) {
				socket.Join(adapter.Room(room))
				ack()
			})
			socket.OnEvent("emit", func() {
				io.In("woot").In("test").Emit("a")
				io.In("third").Emit("b")
			})
		})

		socket1.Connect()
		socket2.Connect()

		tw.WaitTimeout(t, utils.DefaultTestWaitTimeout)
		time.Sleep(200 * time.Millisecond)
		close()
	})

	t.Run("broadcasts to rooms", func(t *testing.T) {
		io, ts, manager, close := newTestServerAndClient(t, nil, nil)
		manager2 := newTestManager(ts, nil)
		socket1 := manager.Socket("/", nil)
		socket2 := manager2.Socket("/", nil)
		socket3 := manager.Socket("/", nil)
		tw := utils.NewTestWaiterString()
		tw.Add("socket2 a")
		tw.Add("socket3 b")

		socket1.Emit("join", "woot")
		socket2.Emit("join", "test")
		socket3.Emit("join", "test", func() {
			socket3.Emit("broadcast")
		})

		socket1.OnEvent("a", func() {
			t.Fatal("should not happen")
		})
		socket2.OnEvent("a", func() {
			tw.Done("socket2 a")
		})
		socket3.OnEvent("a", func() {
			t.Fatal("should not happen")
		})
		socket3.OnEvent("b", func() {
			tw.Done("socket3 b")
		})

		io.OnConnection(func(socket ServerSocket) {
			socket.OnEvent("join", func(room string) {
				socket.Join(adapter.Room(room))
			})
			socket.OnEvent("join", func(room string, ack func()) {
				socket.Join(adapter.Room(room))
				ack()
			})
			socket.OnEvent("broadcast", func() {
				socket.Broadcast().To("test").Emit("a")
				socket.Emit("b")
			})
		})

		socket1.Connect()
		socket2.Connect()
		socket3.Connect()

		tw.WaitTimeout(t, utils.DefaultTestWaitTimeout)
		time.Sleep(200 * time.Millisecond)
		close()
	})

	t.Run("broadcasts binary data to rooms", func(t *testing.T) {
		io, ts, manager, close := newTestServerAndClient(t, nil, nil)
		manager2 := newTestManager(ts, nil)
		socket1 := manager.Socket("/", nil)
		socket2 := manager2.Socket("/", nil)
		socket3 := manager.Socket("/", nil)
		tw := utils.NewTestWaiterString()
		tw.Add("socket2 bin")
		tw.Add("socket3 bin2")
		buf := Binary{0xDE, 0xAD, 0xBE, 0xEF}

		socket1.Emit("join", "woot", func() {
			socket2.Emit("join", "test", func() {
				socket3.Emit("join", "test", func() {
					socket3.Emit("broadcast")
				})
			})
		})

		socket1.OnEvent("bin", func(a Binary) {
			t.Fatal("should not happen")
		})
		socket2.OnEvent("bin", func(a Binary) {
			assert.Equal(t, buf, a)
			tw.Done("socket2 bin")
		})
		socket2.OnEvent("bin2", func(a Binary) {
			t.Fatal("should not happen")
		})
		socket3.OnEvent("bin", func(a Binary) {
			t.Fatal("should not happen")
		})
		socket3.OnEvent("bin2", func(a Binary) {
			assert.Equal(t, buf, a)
			tw.Done("socket3 bin2")
		})

		io.OnConnection(func(socket ServerSocket) {
			socket.OnEvent("join", func(room string) {
				socket.Join(adapter.Room(room))
			})
			socket.OnEvent("join", func(room string, ack func()) {
				socket.Join(adapter.Room(room))
				ack()
			})
			socket.OnEvent("broadcast", func() {
				socket.Broadcast().To("test").Emit("bin", buf)
				socket.Emit("bin2", buf)
			})
		})

		socket1.Connect()
		socket2.Connect()
		socket3.Connect()

		tw.WaitTimeout(t, utils.DefaultTestWaitTimeout)
		time.Sleep(200 * time.Millisecond)
		close()
	})

	t.Run("keeps track of rooms", func(t *testing.T) {
		io, _, manager, close := newTestServerAndClient(t, nil, nil)
		socket := manager.Socket("/", nil)
		tw := utils.NewTestWaiter(1)

		io.OnConnection(func(socket ServerSocket) {
			socket.Join("a")
			assert.True(t, socket.Rooms().Contains(adapter.Room(socket.ID()), "a"))
			socket.Join("b")
			assert.True(t, socket.Rooms().Contains(adapter.Room(socket.ID()), "a", "b"))
			socket.Join("c")
			assert.True(t, socket.Rooms().Contains(adapter.Room(socket.ID()), "a", "b", "c"))
			socket.Leave("b")
			assert.True(t, socket.Rooms().Contains(adapter.Room(socket.ID()), "a", "c"))

			socket.(*serverSocket).leaveAll()
			assert.Equal(t, 0, socket.Rooms().Cardinality())
			tw.Done()
		})
		socket.Connect()

		tw.WaitTimeout(t, utils.DefaultTestWaitTimeout)
		close()
	})

	t.Run("allows to join several rooms at once", func(t *testing.T) {
		io, _, manager, close := newTestServerAndClient(t, nil, nil)
		socket := manager.Socket("/", nil)
		tw := utils.NewTestWaiter(1)

		io.OnConnection(func(socket ServerSocket) {
			socket.Join("a", "b", "c")
			assert.True(t, socket.Rooms().Contains(adapter.Room(socket.ID()), "a", "b", "c"))

			socket.(*serverSocket).leaveAll()
			assert.Equal(t, 0, socket.Rooms().Cardinality())
			tw.Done()
		})
		socket.Connect()

		tw.WaitTimeout(t, utils.DefaultTestWaitTimeout)
		close()
	})

	t.Run("should exclude specific sockets when broadcasting", func(t *testing.T) {
		io, ts, manager, close := newTestServerAndClient(t, nil, nil)
		manager2 := newTestManager(ts, nil)
		manager3 := newTestManager(ts, nil)
		socket1 := manager.Socket("/", nil)
		socket2 := manager2.Socket("/", nil)
		socket3 := manager3.Socket("/", nil)
		tw := utils.NewTestWaiter(1)

		socket2.OnEvent("a", func() {
			t.Fatal("should not happen")
		})
		socket3.OnEvent("a", func() {
			t.Fatal("should not happen")
		})
		socket1.OnEvent("a", func() {
			tw.Done()
		})

		io.OnConnection(func(socket ServerSocket) {
			socket.OnEvent("exclude", func(id string) {
				socket.Broadcast().Except(adapter.Room(id)).Emit("a")
			})
		})
		socket2.OnConnect(func() {
			socket3.Emit("exclude", socket2.ID())
		})
		socket1.Connect()
		socket2.Connect()
		socket3.Connect()

		tw.WaitTimeout(t, utils.DefaultTestWaitTimeout)
		time.Sleep(200 * time.Millisecond)
		close()
	})

	t.Run("should exclude a specific room when broadcasting", func(t *testing.T) {
		io, ts, manager, close := newTestServerAndClient(t, nil, nil)
		manager2 := newTestManager(ts, nil)
		manager3 := newTestManager(ts, nil)
		socket1 := manager.Socket("/", nil)
		socket2 := manager2.Socket("/", nil)
		socket3 := manager3.Socket("/", nil)
		tw := utils.NewTestWaiter(1)

		socket2.OnEvent("a", func() {
			t.Fatal("should not happen")
		})
		socket3.OnEvent("a", func() {
			t.Fatal("should not happen")
		})
		socket1.OnEvent("a", func() {
			tw.Done()
		})

		io.OnConnection(func(socket ServerSocket) {
			socket.OnEvent("join", func(room string, ack func()) {
				socket.Join(adapter.Room(room))
				ack()
			})
			socket.OnEvent("broadcast", func() {
				socket.Broadcast().Except("room1").Emit("a")
			})
		})
		socket2.Emit("join", "room1", func() {
			socket3.Emit("broadcast")
		})

		socket1.Connect()
		socket2.Connect()
		socket3.Connect()

		tw.WaitTimeout(t, utils.DefaultTestWaitTimeout)
		time.Sleep(200 * time.Millisecond)
		close()
	})
}
