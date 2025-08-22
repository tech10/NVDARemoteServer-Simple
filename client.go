package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net"
	"runtime/debug"
	"strconv"
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

// NewClient creates a new client with the given net.Conn interface and server.
func NewClient(conn net.Conn, s *Server) *Client {
	s.l.Warnf("Client %s connected.\n", conn.RemoteAddr())
	return &Client{
		conn:          conn,
		srv:           s,
		connectedTime: time.Now(),
	}
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
		c.w.Close()
		c.srv.l.Warnf("Client %s disconnected. Longest write duration was %s. Client was connected for %s\n", c.value(), c.readWriteDuration(), c.connectedDuration())
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
		c.srv.l.Errorf("Invalid data type, failed to send Msg type to client %s: %v\n", c.value(), err)
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
	buffer := bufio.NewReaderSize(c.conn, ReadBufSize)
	c.srv.l.Debugf("Read buffer created for client %s: size %d.\n", c.value(), ReadBufSize)
	c.w = newWritech(c)
	defer c.Close()
	defer c.panicCatch(recover())
	for {
		line, err := buffer.ReadSlice(Delimiter)
		if err != nil && !errors.Is(err, bufio.ErrBufferFull) {
			if !errors.Is(err, io.EOF) && !c.isClosed() {
				c.srv.l.Errorf("Read error from client %s: %v\n", c.value(), err)
			}
			return
		}

		c.srv.l.Interceptf("Received data from client %s\n%s\n", c.value(), line)

		if c.channel != "" {
			c.handleChannel(line)
			continue
		}

		handshake := new(Handshake)
		if err := json.Unmarshal(line, handshake); err != nil {
			c.srv.l.Debugf("Invalid JSON data from client %s: %v\nData truncated: \"%s\"\n", c.value(), err, truncate(line, 12))
			return
		}
		if !c.handleHandshake(handshake) {
			c.srv.l.Debugf("Invalid handshake from client %s\n", c.value())
			return
		}
	}
}

func (c *Client) handleHandshake(handshake *Handshake) bool {
	switch handshake.Type {
	case TypeJoin:
		if handshake.Channel == "" || handshake.ConnectionType == "" {
			c.srv.l.Errorf("Client %s set empty Channel or connection type with %s type.\n", c.value(), TypeJoin)
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
		c.srv.l.Debugf("Client %s generated key \"%s\"\n", c.value(), key)
		c.SendMsg(Msg{
			"type": TypeGenerateKey,
			"key":  key,
		})
		return true
	case TypeProtocolVersion:
		if handshake.Version <= 0 {
			c.srv.l.Debugf("Client %s is using invalid protocol version %d\n", c.conn.RemoteAddr(), handshake.Version)
			c.SendMsg(MsgErr)
			return false
		}
		c.srv.l.Debugf("Client %s is using valid protocol version %d\n", c.conn.RemoteAddr(), handshake.Version)
		c.version = handshake.Version
		return true
	default:
		c.srv.l.Errorf("Client %s sent unknown type field: \"%s\"\n", c.value(), handshake.Type)
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
		c.srv.l.Debugf("Invalid JSON data from client %s: %s\nData truncated: \"%s\"\n", c.value(), err, truncate(line, 4))
		c.srv.SendLineToChannel(c, line, true)
		return
	}
	c.srv.SendMsgToChannel(c, msgdec, true)
}

func (c *Client) sendMotd() {
	var fmotd string
	level := c.srv.l.level
	display := motdAlwaysDisplay
	if level >= LogLevelDebug {
		display = true
		fmotd = "This server is running with its log level set to " + level.String() + ". Channel information "
		if level >= LogLevelIntercept {
			fmotd += "and protocol data"
		}
		fmotd += " is being intercepted."
		if motd != "" {
			fmotd += "\n" + motd
		}
	} else {
		fmotd = motd
	}

	if fmotd == "" {
		return
	}

	c.SendMsg(Msg{
		"type":               TypeMotd,
		"motd":               fmotd,
		TypeMotdForceDisplay: display,
	})
}

func (c *Client) panicCatch(r any) {
	if r == nil {
		return
	}
	trace := debug.Stack()
	c.srv.l.Errorf("PANIC CAUGHT: from client %s\n%v\nStack trace:\n%s\n", c.value(), r, trace)
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
		c.srv.l.Debugf("New write duration stored for client %s: %s. Previous duration: %s\n", c.value(), elapsed, c.writeDuration)
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

func (c *Client) value() string {
	if c.id != 0 {
		return strconv.FormatUint(uint64(c.id), 10)
	}
	return c.conn.RemoteAddr().String()
}
