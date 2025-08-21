package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net"
	"runtime/debug"
	"sync"
)

type Client struct {
	Conn           net.Conn
	closed         bool
	mu             sync.RWMutex
	ID             uint
	Srv            *Server
	Channel        string
	ConnectionType string
	once           sync.Once
	w              *writech
}

func (c *Client) Close() {
	c.once.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()
		if c.Channel != "" {
			c.Srv.removeClient(c)
		}
		c.Conn.Close()
		if c.ID != 0 {
			c.Srv.Log.Printf("Client %d disconnected.\n", c.ID)
		} else {
			c.Srv.Log.Printf("Client disconnected from %s\n", c.Conn.RemoteAddr())
		}
		c.w.Close()
	})
}

func (c *Client) AsMap() Msg {
	return Msg{
		TypeID:             c.ID,
		TypeConnectionType: c.ConnectionType,
	}
}

func (c *Client) SendMsg(msg Msg) {
	line, err := json.Marshal(msg)
	if err != nil {
		if c.ID != 0 {
			c.Srv.Log.Printf("Invalid data type, failed to send MSG to client: %d. %v\n", c.ID, err)
		} else {
			c.Srv.Log.Printf("Invalid data type, failed to send MSG to client: %s. %v\n", c.Conn.RemoteAddr(), err)
		}
		return
	}
	line = append(line, Delimiter)
	c.SendLine(line)
}

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
	c.Srv.Log.Printf("Client connected from %s\n", c.Conn.RemoteAddr())
	buffer := bufio.NewReaderSize(c.Conn, ReadBufSize)
	c.w = newWritech(c)
	defer c.Close()
	defer c.panicCatch(recover())
	for {
		line, err := buffer.ReadSlice(Delimiter)
		if err != nil && !errors.Is(err, bufio.ErrBufferFull) {
			if !errors.Is(err, io.EOF) && !c.isClosed() {
				if c.ID != 0 {
					c.Srv.Log.Printf("Read error from client %d: %v\n", c.ID, err)
				} else {
					c.Srv.Log.Printf("Read error from client %s: %v\n", c.Conn.RemoteAddr(), err)
				}
			}
			return
		}

		if c.Channel != "" {
			c.handleChannel(line)
			continue
		}

		handshake := new(Handshake)
		if err := json.Unmarshal(line, handshake); err != nil {
			c.Srv.Log.Printf("Invalid JSON data from client %s: %v\nData truncated: \"%s\"\n", c.Conn.RemoteAddr(), err, truncate(line, 12))
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
			c.Srv.Log.Printf("Client %s set empty Channel or ConnectionType with %s type\n", c.Conn.RemoteAddr(), TypeJoin)
			c.SendMsg(MsgErr)
			return false
		}
		c.Channel = handshake.Channel
		c.ConnectionType = handshake.ConnectionType
		c.Srv.addClient(c)
		c.sendMotd()
		return true
	case TypeGenerateKey:
		key := c.Srv.generateKey()
		c.SendMsg(Msg{
			"type": TypeGenerateKey,
			"key":  key,
		})
		c.Srv.Log.Printf("Client %s generated key \"%s\"\n", c.Conn.RemoteAddr(), key)
		return true
	case TypeProtocolVersion:
		return true
	default:
		c.Srv.Log.Printf("Client %s sent unknown type field: \"%s\"\n", c.Conn.RemoteAddr(), handshake.Type)
		c.SendMsg(MsgErr)
		return false
	}
}

func (c *Client) handleChannel(line []byte) {
	if !sendOrigin {
		c.Srv.SendLineToChannel(c, line, false)
		return
	}
	var msgdec Msg
	if err := json.Unmarshal(line, &msgdec); err != nil {
		c.Srv.Log.Printf("Invalid JSON data from client %d: %s\nData truncated: \"%s\"\n", c.ID, err, truncate(line, 4))
		c.Srv.SendLineToChannel(c, line, true)
		return
	}
	c.Srv.SendMsgToChannel(c, msgdec, true)
}

func (c *Client) sendMotd() {
	if motd == "" {
		return
	}
	msg := Msg{
		"type":               TypeMotd,
		"motd":               motd,
		TypeMotdForceDisplay: motdAlwaysDisplay,
	}
	c.SendMsg(msg)
}

func (c *Client) panicCatch(r any) {
	if r == nil {
		return
	}
	trace := debug.Stack()
	if c.ID != 0 {
		c.Srv.Log.Printf("PANIC CAUGHT: from client %d\n%v\nStack trace:\n%s\n", c.ID, r, trace)
	} else {
		c.Srv.Log.Printf("PANIC CAUGHT: from client %s\n%v\nStack trace:\n%s\n", c.Conn.RemoteAddr(), r, trace)
	}
}
