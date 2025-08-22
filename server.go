package main

import (
	"crypto/tls"
	"encoding/json"
	"log"
	"math/rand"
	"net"
	"strconv"
	"sync"
)

// Server provides a server using the protocol for NVDA's Remote Access feature.
type Server struct {
	log      *log.Logger
	cfg      *tls.Config
	mu       sync.RWMutex
	channels map[string]Channel
	nextID   uint
}

// NewServer creates a server with the provided tls certificate.
func NewServer(cert tls.Certificate) *Server {
	cfg := &tls.Config{
		Certificates:             []tls.Certificate{cert},
		PreferServerCipherSuites: true,
		MinVersion:               tls.VersionTLS12,
	}

	return &Server{
		cfg:      cfg,
		log:      logger,
		channels: make(map[string]Channel),
	}
}

// Printf prints to the internal logger.
func (s *Server) Printf(format string, v ...any) {
	if s.log != nil {
		s.log.Printf(format, v...)
	}
}

// Start starts the server with the provided listen address.
// This can be called multiple times from different listen addresses.
func (s *Server) Start(sAddr string) error {
	ln, err := net.Listen("tcp", sAddr)
	if err != nil {
		s.Printf("Listener error on %s: %s\n", sAddr, err)
		return err
	}

	tcpLn, ok := ln.(*net.TCPListener)
	if !ok {
		s.Printf("listener is not a TCP listener\n")
		return ErrNotTCP
	}

	if s.cfg == nil {
		s.Printf("listener is not a TLS listener\n")
		return ErrNotTLS
	}

	ln = tls.NewListener(tcpKeepAliveListener{tcpLn}, s.cfg)
	defer ln.Close()
	s.Printf("Server started successfully on \"%s\"\n", ln.Addr())

	for {
		conn, connErr := ln.Accept()
		if connErr != nil {
			s.Printf("Server error: %s\n", connErr)
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
		s.Printf("Invalid MSG sent to channel from client: %d, %s\n", client.id, err)
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
		return
	}
	for c := range s.channels[client.channel] {
		if client != c && client.connectionType != c.connectionType {
			c.SendLine(line)
			if !sent {
				sent = true
			}
		}
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

	s.Printf("Client %s joined channel \"%s\" with connection type %s and received ID %d\n", client.conn.RemoteAddr(), client.channel, client.connectionType, client.id)
}

func (s *Server) removeClient(client *Client) {
	send := true
	s.mu.Lock()
	delete(s.channels[client.channel], client)
	if len(s.channels[client.channel]) == 0 {
		delete(s.channels, client.channel)
		send = false
	}
	s.mu.Unlock()

	if send {
		s.SendMsgToChannel(client, Msg{
			"type":     TypeClientLeft,
			TypeUserID: client.id,
			TypeClient: client.AsMap(),
		}, false)
	}

	s.Printf("Client %d left channel \"%s\"\n", client.id, client.channel)
}

func (s *Server) generateKey() (key string) {
	for {
		key = strconv.Itoa(rand.Intn(90000000) + 10000000)

		s.mu.RLock()
		_, exist := s.channels[key]
		s.mu.RUnlock()

		if !exist {
			return key
		}
	}
}

func (s *Server) getNextID() uint {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	return s.nextID
}
