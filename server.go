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

type Server struct {
	Addr        string
	Certificate tls.Certificate
	Log         *log.Logger
	mu          sync.RWMutex
	channels    map[string]Channel
	nextID      uint
}

func (s *Server) Start() {
	config := &tls.Config{
		Certificates:             []tls.Certificate{s.Certificate},
		PreferServerCipherSuites: true,
		MinVersion:               tls.VersionTLS12,
	}

	ln, err := net.Listen("tcp", s.Addr)
	if err != nil {
		s.Log.Fatalf("Listener error on %s: %s\n", s.Addr, err)
	}

	tcpLn, ok := ln.(*net.TCPListener)
	if !ok {
		s.Log.Fatal("listener is not a TCP listener\n")
	}
	ln = tls.NewListener(tcpKeepAliveListener{tcpLn}, config)
	defer ln.Close()
	s.Log.Printf("Server started successfully on \"%s\"\n", ln.Addr())

	for {
		conn, err := ln.Accept()
		if err != nil {
			s.Log.Printf("Server error: %s\n", err)
			break
		}

		client := &Client{
			Conn: conn,
			Srv:  s,
		}

		go client.handler()
	}
}

func (s *Server) SendMsgToChannel(client *Client, msg Msg, encOrigin bool) {
	if encOrigin {
		msg["origin"] = client.ID
	}
	line, err := json.Marshal(msg)
	if err != nil {
		s.Log.Printf("Invalid MSG sent to channel from client: %d, %s\n", client.ID, err)
	}
	line = append(line, Delimiter)
	s.SendLineToChannel(client, line, encOrigin) // encOrigin is the value of sendNotConnected in this call
}

func (s *Server) SendLineToChannel(client *Client, line []byte, sendNotConnected bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var sent bool
	_, exist := s.channels[client.Channel]
	if !exist {
		return
	}
	for c := range s.channels[client.Channel] {
		if client != c && client.ConnectionType != c.ConnectionType {
			c.SendLine(line)
			if !sent {
				sent = true
			}
		}
	}
	if !sent && sendNotConnected && client.ConnectionType == TypeController {
		client.SendMsg(MsgNotConnected)
	}
}

func (s *Server) addClient(client *Client) {
	client.ID = s.getNextID()
	s.SendMsgToChannel(client, Msg{
		"type":     TypeClientJoined,
		TypeUserID: client.ID,
		TypeClient: client.AsMap(),
	}, false)

	s.mu.Lock()
	if s.channels[client.Channel] == nil {
		s.channels[client.Channel] = make(Channel)
	}
	s.channels[client.Channel][client] = struct{}{}

	var clients []Msg
	var clientsID []uint

	for c := range s.channels[client.Channel] {
		if c != client && c.ConnectionType != client.ConnectionType {
			clients = append(clients, c.AsMap())
			clientsID = append(clientsID, c.ID)
		}
	}
	s.mu.Unlock()

	client.SendMsg(Msg{
		"type":      TypeChannelJoined,
		TypeChannel: client.Channel,
		TypeUserIDs: clientsID,
		TypeClients: clients,
	})

	s.Log.Printf("Client %s joined channel \"%s\" with connection type %s and received ID %d\n", client.Conn.RemoteAddr(), client.Channel, client.ConnectionType, client.ID)
}

func (s *Server) removeClient(client *Client) {
	send := true
	s.mu.Lock()
	delete(s.channels[client.Channel], client)
	if len(s.channels[client.Channel]) == 0 {
		delete(s.channels, client.Channel)
		send = false
	}
	s.mu.Unlock()

	if send {
		s.SendMsgToChannel(client, Msg{
			"type":     TypeClientLeft,
			TypeUserID: client.ID,
			TypeClient: client.AsMap(),
		}, false)
	}

	s.Log.Printf("Client %d left channel \"%s\"\n", client.ID, client.Channel)
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
