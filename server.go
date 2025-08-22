package main

import (
	"crypto/tls"
	"encoding/json"
	"math/rand"
	"net"
	"strconv"
	"sync"
)

// Server provides a server using the protocol for NVDA's Remote Access feature.
type Server struct {
	l        *Logger
	cfg      *tls.Config
	mu       sync.RWMutex
	channels map[string]Channel
	nextID   uint
}

// NewServer creates a server with the provided tls certificate and Logger.
func NewServer(cert tls.Certificate, l *Logger) *Server {
	cfg := &tls.Config{
		Certificates:             []tls.Certificate{cert},
		PreferServerCipherSuites: true,
		MinVersion:               tls.VersionTLS12,
	}

	return &Server{
		cfg:      cfg,
		l:        l,
		channels: make(map[string]Channel),
	}
}

// Start starts the server with the provided listen address.
// This can be called multiple times from different listen addresses.
func (s *Server) Start(sAddr string) error {
	s.l.Debugf("Attempting to start server with listen address %s\n", sAddr)
	ln, err := net.Listen("tcp", sAddr)
	if err != nil {
		s.l.Errorf("Listener error on %s: %s\n", sAddr, err)
		return err
	}

	tcpLn, ok := ln.(*net.TCPListener)
	if !ok {
		s.l.Errorf("listener is not a TCP listener\n")
		return ErrNotTCP
	}

	if s.cfg == nil {
		s.l.Errorf("listener is not a TLS listener\n")
		return ErrNotTLS
	}

	ln = tls.NewListener(tcpKeepAliveListener{tcpLn}, s.cfg)
	defer ln.Close()
	defer s.l.Debugf("Server closed at listening address %s\n", ln.Addr())
	s.l.Infof("Server started successfully at listening address %s\n", ln.Addr())

	for {
		conn, connErr := ln.Accept()
		if connErr != nil {
			s.l.Errorf("Unable to accept connection at %s: %s\n", ln.Addr(), connErr)
			break
		}

		client := NewClient(conn, s)
		go client.handler()
	}
	return nil
}

// SendMsgToChannel decodes Msg and sends it to the channel assigned to the given client.
// If encOrigin is true, the origin field will be created and set to the client ID.
// The origin field is required for braille displays to function correctly over the Remote Access connection.
func (s *Server) SendMsgToChannel(client *Client, msg Msg, encOrigin bool) {
	if encOrigin {
		msg["origin"] = client.id
	}
	line, err := json.Marshal(msg)
	if err != nil {
		s.l.Errorf("Invalid Msg type sent to channel from client: %s: %s\n", client.value(), err)
	}
	line = append(line, Delimiter)
	s.SendLineToChannel(client, line, encOrigin) // encOrigin is the value of sendNotConnected in this call
}

// SendLineToChannel sends the given line to the channel assigned to the given client.
// If sendNotConnected is true and the client type is a controller, TypeNvdaNotConnected will be sent if the controller attempts to control a controlled computer while no controlled computers are connected.
func (s *Server) SendLineToChannel(client *Client, line []byte, sendNotConnected bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var sent bool
	_, exist := s.channels[client.channel]
	if !exist {
		s.l.Interceptf("Attempted to send data to non-existent channel \"%s\"\nData: %s", client.channel, line)
		return
	}
	count := 0
	for c := range s.channels[client.channel] {
		if client != c && client.connectionType != c.connectionType {
			c.SendLine(line)
			if !sent {
				sent = true
			}
		}
	}
	if count > 0 {
		s.l.Debugf("Data sent to client count %d in channel \"%s\"\n", count, client.channel)
	}
	if !sent && sendNotConnected && client.connectionType == TypeController {
		client.SendMsg(MsgNotConnected)
	}
}

func (s *Server) addClient(client *Client) {
	client.id = s.getNextID()
	s.SendMsgToChannel(client, Msg{
		"type":     TypeClientJoined,
		TypeUserID: client.id,
		TypeClient: client.AsMap(),
	}, false)

	s.mu.Lock()
	if s.channels[client.channel] == nil {
		s.channels[client.channel] = make(Channel)
		s.l.Debugf("Channel created: \"%s\"\n", client.channel)
	}
	s.channels[client.channel][client] = struct{}{}

	var clients []Msg
	var clientsID []uint

	for c := range s.channels[client.channel] {
		if c != client && c.connectionType != client.connectionType {
			clients = append(clients, c.AsMap())
			clientsID = append(clientsID, c.id)
		}
	}
	s.mu.Unlock()

	client.SendMsg(Msg{
		"type":      TypeChannelJoined,
		TypeChannel: client.channel,
		TypeUserIDs: clientsID,
		TypeClients: clients,
	})

	if s.l.level >= LogLevelDebug {
		s.l.Debugf("Client %s joined channel \"%s\" with connection type %s and received ID %d.\n", client.conn.RemoteAddr(), client.channel, client.connectionType, client.id)
	} else {
		s.l.Warnf("Client %s received ID %d.\n", client.conn.RemoteAddr(), client.id)
	}
}

func (s *Server) removeClient(client *Client) {
	send := true
	s.mu.Lock()
	delete(s.channels[client.channel], client)
	s.l.Debugf("Client %s left channel \"%s\"\n", client.value(), client.channel)
	if len(s.channels[client.channel]) == 0 {
		delete(s.channels, client.channel)
		send = false
		s.l.Debugf("Channel removed: \"%s\"\n", client.channel)
	}
	s.mu.Unlock()

	if send {
		s.SendMsgToChannel(client, Msg{
			"type":     TypeClientLeft,
			TypeUserID: client.id,
			TypeClient: client.AsMap(),
		}, false)
	}
}

func (s *Server) generateKey() (key string) {
	for {
		key = strconv.Itoa(rand.Intn(90000000) + 10000000)
		s.l.Debugf("Generated channel key: \"%s\"\n", key)
		s.mu.RLock()
		_, exist := s.channels[key]
		s.mu.RUnlock()

		if !exist {
			s.l.Debugf("Channel key does not exist, sending to client.")
			return key
		}
		s.l.Debugf("Channel key exists, generating a new key.")
	}
}

func (s *Server) getNextID() uint {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	s.l.Debugf("Next ID retrieved: %d\n", s.nextID)
	return s.nextID
}
