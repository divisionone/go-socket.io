package socketio

import (
	"log"
	"net/http"
	"sync"
	"time"

	engineio "github.com/divisionone/go-engine.io"
	"github.com/steveoc64/memdebug"
)

// Socket is the socket object of socket.io.
type Socket interface {

	// Id returns the session id of socket.
	Id() string

	// Rooms returns the rooms name joined now.
	Rooms() []string

	// Request returns the first http request when established connection.
	Request() *http.Request

	// On registers the function f to handle an event.
	On(event string, f interface{}) error

	// Emit emits an event with given args.
	Emit(event string, args ...interface{}) error

	// Join joins the room.
	Join(room string) error

	// Leave leaves the room.
	Leave(room string) error

	// Disconnect disconnect the socket.
	Disconnect()

	// BroadcastTo broadcasts an event to the room with given args.
	BroadcastTo(room, event string, args ...interface{}) error
}

type socket struct {
	*socketHandler
	conn      engineio.Conn
	namespace string
	id        int
	mu        sync.Mutex
}

func newSocket(conn engineio.Conn, base *baseHandler) *socket {
	ret := &socket{
		conn: conn,
	}
	ret.socketHandler = newSocketHandler(ret, base)
	return ret
}

func (s *socket) Id() string {
	return s.conn.Id()
}

func (s *socket) Request() *http.Request {
	return s.conn.Request()
}

func (s *socket) Emit(event string, args ...interface{}) error {
	if err := s.socketHandler.Emit(event, args...); err != nil {
		return err
	}
	if event == "disconnect" {
		s.conn.Close()
	}
	return nil
}

func (s *socket) Disconnect() {
	s.conn.Close()
}

func (s *socket) send(args []interface{}) error {
	packet := packet{
		Type: _EVENT,
		Id:   -1,
		NSP:  s.namespace,
		Data: args,
	}
	encoder := newEncoder(s.conn)
	return encoder.Encode(packet)
}

func (s *socket) sendConnect() error {
	packet := packet{
		Type: _CONNECT,
		Id:   -1,
		NSP:  s.namespace,
	}
	encoder := newEncoder(s.conn)
	return encoder.Encode(packet)
}

func (s *socket) sendId(args []interface{}) (int, error) {
	s.mu.Lock()
	packet := packet{
		Type: _EVENT,
		Id:   s.id,
		NSP:  s.namespace,
		Data: args,
	}
	s.id++
	if s.id < 0 {
		s.id = 0
	}
	s.mu.Unlock()

	encoder := newEncoder(s.conn)
	err := encoder.Encode(packet)
	if err != nil {
		return -1, nil
	}
	return packet.Id, nil
}

func (s *socket) loop() error {
	defer func() {
		log.Println("end of socket loop")
		s.LeaveAll()
		p := packet{
			Type: _DISCONNECT,
			Id:   -1,
		}
		s.socketHandler.onPacket(nil, &p)
	}()

	p := packet{
		Type: _CONNECT,
		Id:   -1,
	}
	encoder := newEncoder(s.conn)
	if err := encoder.Encode(p); err != nil {
		return err
	}
	s.socketHandler.onPacket(nil, &p)
	for {
		t1 := time.Now()
		log.Println("start decode socket")
		decoder := newDecoder(s.conn)
		memdebug.Print(t1, "decoding socket", s.conn)
		var p packet
		if err := decoder.Decode(&p); err != nil {
			memdebug.Print(t1, "failed to decode from socket", err)
			return err
		}
		memdebug.Print(t1, "packet", p)
		ret, err := s.socketHandler.onPacket(decoder, &p)
		if err != nil {
			return err
		}
		switch p.Type {
		case _CONNECT:
			s.namespace = p.NSP
			s.sendConnect()
		case _BINARY_EVENT:
			fallthrough
		case _EVENT:
			if p.Id >= 0 {
				p := packet{
					Type: _ACK,
					Id:   p.Id,
					NSP:  s.namespace,
					Data: ret,
				}
				encoder := newEncoder(s.conn)
				if err := encoder.Encode(p); err != nil {
					return err
				}
			}
		case _DISCONNECT:
			return nil
		case _ERROR:
			memdebug.Print(t1, "got an error it seems")
		case _BINARY_ACK, _ACK:
			// do nothing
		default:
			memdebug.Print(t1, "unknown type of socket event", p.Type.String())
		}
	}
}
