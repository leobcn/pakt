/*
 *  PAKT - Interlink Remote Applications
 *  Copyright (C) 2016  Roland Singer <roland.singer[at]desertbit.com>
 *
 *  This program is free software: you can redistribute it and/or modify
 *  it under the terms of the GNU General Public License as published by
 *  the Free Software Foundation, either version 3 of the License, or
 *  (at your option) any later version.
 *
 *  This program is distributed in the hope that it will be useful,
 *  but WITHOUT ANY WARRANTY; without even the implied warranty of
 *  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 *  GNU General Public License for more details.
 *
 *  You should have received a copy of the GNU General Public License
 *  along with this program.  If not, see <http://www.gnu.org/licenses/>.
 */

package pakt

import (
	"net"
	"sync"
)

const (
	socketIDLength = 20
)

//###################//
//### Server Type ###//
//###################//

type Server struct {
	ln net.Listener

	sockets      map[string]*Socket
	socketsMutex sync.Mutex

	onNewSocket func(*Socket)

	closeMutex  sync.Mutex
	isClosed    bool
	closeChan   chan struct{}
	onCloseFunc func()
}

// NewServer creates a new PAKT server.
func NewServer(ln net.Listener) *Server {
	s := &Server{
		ln:        ln,
		sockets:   make(map[string]*Socket),
		closeChan: make(chan struct{}),
	}

	return s
}

// Listen for new socket connections.
// This method is blocking.
func (s *Server) Listen() {
	// Call close on exit.
	defer s.Close()

	// Wait for new client connections.
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			// Check if the listener was closed.
			select {
			case <-s.closeChan:
				return
			default:
			}

			// Log.
			Log.Warningf("server: accept connection: %v", err)

			// Continue accepting clients.
			continue
		}

		// Handle the connection in a new goroutine.
		go s.handleConnection(conn)
	}
}

// IsClosed returns a boolean indicating if the network listener is closed.
func (s *Server) IsClosed() bool {
	return s.isClosed
}

// Returns a channel which is closed as soon as the network listener is closed.
func (s *Server) ClosedChan() ClosedChan {
	return s.closeChan
}

// OnClose triggers the function as soon as the network listener closes.
func (s *Server) OnClose(f func()) {
	s.onCloseFunc = f
}

func (s *Server) Close() {
	s.closeMutex.Lock()
	defer s.closeMutex.Unlock()

	// Check if already closed.
	if s.isClosed {
		return
	}

	// Update the flag.
	s.isClosed = true

	// Close the close channel.
	close(s.closeChan)

	// Close the network listener.
	err := s.ln.Close()
	if err != nil {
		Log.Warningf("server: failed to close network listener: %v", err)
	}

	// Close all connected sockets.
	for _, s := range s.Sockets() {
		s.Close()
	}

	// Call the on close function if defined.
	if s.onCloseFunc != nil {
		s.onCloseFunc()
	}
}

// OnNewSocket sets the function which is
// triggered if a new socket connection was made.
func (s *Server) OnNewSocket(f func(*Socket)) {
	s.onNewSocket = f
}

// GetSocket obtains a socket by its ID.
// Returns nil if not found.
func (s *Server) GetSocket(id string) *Socket {
	// Lock the mutex.
	s.socketsMutex.Lock()
	defer s.socketsMutex.Unlock()

	// Obtain the socket.
	socket, ok := s.sockets[id]
	if !ok {
		return nil
	}

	return socket
}

// Sockets returns a list of all current connected sockets.
func (s *Server) Sockets() []*Socket {
	// Lock the mutex.
	s.socketsMutex.Lock()
	defer s.socketsMutex.Unlock()

	// Create the slice.
	list := make([]*Socket, len(s.sockets))

	// Add all sockets from the map.
	i := 0
	for _, s := range s.sockets {
		list[i] = s
		i++
	}

	return list
}

//###############//
//### Private ###//
//###############//

func (s *Server) handleConnection(conn net.Conn) {
	// Catch panics.
	defer func() {
		if e := recover(); e != nil {
			Log.Errorf("server catched panic: %v", e)
		}
	}()

	// Create a new socket.
	socket := NewSocket(conn)

	// Add the new socket to the active sockets map.
	// If the ID is already present, then generate a new one.
	func() {
		socket.id = randomString(socketIDLength)

		// Lock the mutex.
		s.socketsMutex.Lock()
		defer s.socketsMutex.Unlock()

		// Be sure that the ID is unique.
		for {
			if _, ok := s.sockets[socket.id]; !ok {
				break
			}

			socket.id = randomString(socketIDLength)
		}

		// Add the socket to the map.
		s.sockets[socket.id] = socket
	}()

	// Remove the socket from the active sockets map on close.
	socket.onClosePrivateFunc = func() {
		// Lock the mutex.
		s.socketsMutex.Lock()
		defer s.socketsMutex.Unlock()

		// Remove the socket from the map.
		delete(s.sockets, socket.id)
	}

	// Call the function if defined.
	if s.onNewSocket != nil {
		s.onNewSocket(socket)
	}
}