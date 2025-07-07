package main

import (
	"fmt"
	"math"
	"sync"

	sio "github.com/anfin21/socket.io"
	"github.com/gookit/color"
)

type api struct {
	numUsers   int
	numUsersMu sync.Mutex

	usernames   map[sio.ServerSocket]string
	usernamesMu sync.Mutex
}

func newAPI() *api {
	return &api{usernames: make(map[sio.ServerSocket]string)}
}

func (a *api) setup(root *sio.Namespace) {
	root.OnConnection(func(socket sio.ServerSocket) {
		socket.OnEvent("new message", func(data string) {
			socket.Broadcast().Emit("new message", struct {
				Username string `json:"username"`
				Message  string `json:"message"`
			}{
				Username: a.username(socket),
				Message:  data,
			})
		})

		socket.OnEvent("add user", func(username string) {
			a.usernamesMu.Lock()
			defer a.usernamesMu.Unlock()
			_, ok := a.usernames[socket]
			if ok {
				return
			}
			a.usernames[socket] = username
			a.numUsersMu.Lock()
			a.numUsers++
			numUsers := a.numUsers
			a.numUsersMu.Unlock()
			usernameColor := getUsernameColor(username)
			fmt.Printf("New user: %s\n", usernameColor.Sprint(username))

			socket.Emit("login", struct {
				NumUsers int `json:"numUsers"`
			}{
				NumUsers: numUsers,
			})

			socket.Broadcast().Emit("user joined", struct {
				Username string `json:"username"`
				NumUsers int    `json:"numUsers"`
			}{
				Username: username,
				NumUsers: numUsers,
			})
		})

		socket.OnEvent("typing", func() {
			socket.Broadcast().Emit("typing", struct {
				Username string `json:"username"`
			}{
				Username: a.username(socket),
			})
		})
		socket.OnEvent("stop typing", func() {
			socket.Broadcast().Emit("stop typing", struct {
				Username string `json:"username"`
			}{
				Username: a.username(socket),
			})
		})

		socket.OnDisconnect(func(reason sio.Reason) {
			a.usernamesMu.Lock()
			defer a.usernamesMu.Unlock()
			username, ok := a.usernames[socket]
			if !ok {
				return
			}

			delete(a.usernames, socket)
			a.numUsersMu.Lock()
			a.numUsers--
			numUsers := a.numUsers
			a.numUsersMu.Unlock()

			socket.Broadcast().Emit("user left", struct {
				Username string `json:"username"`
				NumUsers int    `json:"numUsers"`
			}{
				Username: username,
				NumUsers: numUsers,
			})
		})
	})
}

func (a *api) username(socket sio.ServerSocket) (username string) {
	a.usernamesMu.Lock()
	username = a.usernames[socket]
	a.usernamesMu.Unlock()
	return
}

var colors = []string{
	"#e21400", "#91580f", "#f8a700", "#f78b00",
	"#58dc00", "#287b00", "#a8f07a", "#4ae8c4",
	"#3b88eb", "#3824aa", "#a700ff", "#d300e7",
}

func getUsernameColor(username string) color.RGBColor {
	hash := 7
	for _, r := range username {
		hash = int(r) + (hash << 5) - hash
	}
	index := int(math.Abs(float64(hash % len(colors))))
	return color.Hex(colors[index])
}
