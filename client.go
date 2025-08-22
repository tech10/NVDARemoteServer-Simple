package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net"
	"runtime/debug"
	"sync"
	"time"
)

// Client is a connected client for the NVDA Remote Access server.
type Client struct {
	conn           net.Conn
	closed         bool
	mu             sync.RWMutex
	writeDuration  time.Duration
	connectedTime  time.Time
	id             uint
	srv            *Server
	channel        string
	connectionType string
	version        int
	once           sync.Once
	w              *writech
}

// Close closes the client connection and any associated goroutines.
func (c *Client) Close() {
	c.once.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()
		if c.channel != "" {
			c.srv.removeClient(c)
		}
		c.conn.Close()
		if c.id != 0 {
			c.srv.Printf("Client %d disconnected. Longest write duration was %s. Client connected for %s\n", c.id, c.readWriteDuration(), c.connectedDuration())
		} else {
			c.srv.Printf("Client disconnected from %s. Longest write duration was %s. Client connected for %s\n", c.conn.RemoteAddr(), c.readWriteDuration(), c.connectedDuration())
		}
		c.w.Close()
	})
}

// AsMap returns the client id and connection type as an Msg type for encoding to a JSON value.
func (c *Client) AsMap() Msg {
	return Msg{
		TypeID:             c.id,
		TypeConnectionType: c.connectionType,
	}
}

// SendMsg decodes Msg and sends it to the client if it's valid JSON.
// If the decoded Msg is not valid JSON, an error will be returned and no data will be sent.
func (c *Client) SendMsg(msg Msg) {
	line, err := json.Marshal(msg)
	if err != nil {
		if c.id != 0 {
			c.srv.Printf("Invalid data type, failed to send MSG to client: %d. %v\n", c.id, err)
		} else {
			c.srv.Printf("Invalid data type, failed to send MSG to client: %s. %v\n", c.conn.RemoteAddr(), err)
		}
		return
	}
	line = append(line, Delimiter)
	c.SendLine(line)
}

// SendLine sends the given line to the client.
// Upon an encountered error, the clients connection is closed, disconnecting it from the server.
func (c *Client) SendLine(line []byte) {
	if err := c.w.Write(line); err != nil {
		c.Close()
	}
}

func (c *Client) isClosed() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.closed
}

func (c *Client) handler() {
	c.srv.Printf("Client connected from %s\n", c.conn.RemoteAddr())
	buffer := bufio.NewReaderSize(c.conn, ReadBufSize)
	c.w = newWritech(c)
	defer c.Close()
	defer c.panicCatch(recover())
	for {
		line, err := buffer.ReadSlice(Delimiter)
		if err != nil && !errors.Is(err, bufio.ErrBufferFull) {
			if !errors.Is(err, io.EOF) && !c.isClosed() {
				if c.id != 0 {
					c.srv.Printf("Read error from client %d: %v\n", c.id, err)
				} else {
					c.srv.Printf("Read error from client %s: %v\n", c.conn.RemoteAddr(), err)
				}
			}
			return
		}

		if c.channel != "" {
			c.handleChannel(line)
			continue
		}

		handshake := new(Handshake)
		if err := json.Unmarshal(line, handshake); err != nil {
			c.srv.Printf("Invalid JSON data from client %s: %v\nData truncated: \"%s\"\n", c.conn.RemoteAddr(), err, truncate(line, 12))
			return
		}
		if !c.handleHandshake(handshake) {
			return
		}
	}
}

func (c *Client) handleHandshake(handshake *Handshake) bool {
	switch handshake.Type {
	case TypeJoin:
		if handshake.Channel == "" || handshake.ConnectionType == "" {
			c.srv.Printf("Client %s set empty Channel or ConnectionType with %s type\n", c.conn.RemoteAddr(), TypeJoin)
			c.SendMsg(MsgErr)
			return false
		}
		c.channel = handshake.Channel
		c.connectionType = handshake.ConnectionType
		c.srv.addClient(c)
		c.sendMotd()
		return true
	case TypeGenerateKey:
		key := c.srv.generateKey()
		c.SendMsg(Msg{
			"type": TypeGenerateKey,
			"key":  key,
		})
		c.srv.Printf("Client %s generated key \"%s\"\n", c.conn.RemoteAddr(), key)
		return true
	case TypeProtocolVersion:
		if handshake.Version <= 0 {
			c.srv.Printf("Client %s is using invalid protocol version %d\n", c.conn.RemoteAddr(), handshake.Version)
			c.SendMsg(MsgErr)
			return false
		}
		c.srv.Printf("Client %s is using valid protocol version %d\n", c.conn.RemoteAddr(), handshake.Version)
		c.version = handshake.Version
		return true
	default:
		c.srv.Printf("Client %s sent unknown type field: \"%s\"\n", c.conn.RemoteAddr(), handshake.Type)
		c.SendMsg(MsgErr)
		return false
	}
}

func (c *Client) handleChannel(line []byte) {
	if !sendOrigin {
		c.srv.SendLineToChannel(c, line, true)
		return
	}
	var msgdec Msg
	if err := json.Unmarshal(line, &msgdec); err != nil {
		c.srv.Printf("Invalid JSON data from client %d: %s\nData truncated: \"%s\"\n", c.id, err, truncate(line, 4))
		c.srv.SendLineToChannel(c, line, true)
		return
	}
	c.srv.SendMsgToChannel(c, msgdec, true)
}

func (c *Client) sendMotd() {
	if motd == "" {
		return
	}

	c.SendMsg(Msg{
		"type":               TypeMotd,
		"motd":               motd,
		TypeMotdForceDisplay: motdAlwaysDisplay,
	})
}

func (c *Client) panicCatch(r any) {
	if r == nil {
		return
	}
	trace := debug.Stack()
	if c.id != 0 {
		c.srv.Printf("PANIC CAUGHT: from client %d\n%v\nStack trace:\n%s\n", c.id, r, trace)
	} else {
		c.srv.Printf("PANIC CAUGHT: from client %s\n%v\nStack trace:\n%s\n", c.conn.RemoteAddr(), r, trace)
	}
}

// storeDuration stores the elapsed duration if it’s greater.
func (c *Client) storeWriteDuration(start time.Time) {
	if c.isClosed() {
		return
	}

	elapsed := time.Since(start)

	c.mu.Lock()
	defer c.mu.Unlock()
	if elapsed > c.writeDuration {
		c.writeDuration = elapsed
	}
}

// readWriteDuration reads the current writeDuration.
func (c *Client) readWriteDuration() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.writeDuration
}

func (c *Client) connectedDuration() time.Duration {
	return time.Since(c.connectedTime)
}
