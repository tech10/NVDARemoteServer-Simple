package main

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"flag"
	"log"
	"math/rand"
	"net"
	"os"
	"strconv"
	"sync"
	"time"
)

const (
	BufSize         = 2048
	KeepAlivePeriod = time.Second * 30
	Delimiter       = '\n'
)

type (
	Msg     map[string]interface{}
	Channel map[*Client]struct{}
)

type Handshake struct {
	Type            string
	Channel         string
	Connection_type string
}

type Client struct {
	Conn            net.Conn
	ID              uint
	Srv             *Server
	Channel         string
	Connection_type string
}

func (c *Client) Handler() {
	c.Srv.Log.Printf("Connected client %d %s\n", c.ID, c.Conn.RemoteAddr())
	buffer := bufio.NewReaderSize(c.Conn, BufSize)
	defer c.Close()

	for {
		line, err := buffer.ReadSlice(Delimiter)
		if err != nil && err != bufio.ErrBufferFull {
			c.Srv.Log.Printf("Getting data error from client %d: %s\n", c.ID, err)
			return
		}

		if c.Channel != "" {
			c.Srv.SendLineToChannel(c, line)
			continue
		}

		handshake := new(Handshake)
		if err := json.Unmarshal(line, handshake); err != nil {
			c.Srv.Log.Printf("JSON data of client %d: %s\n", c.ID, err)
			return
		}

		switch handshake.Type {
		case "join":
			if handshake.Channel == "" || handshake.Connection_type == "" {
				c.Srv.Log.Printf("Client %d set empty Channel or Connection_type when join to channel\n", c.ID)
				return
			}
			c.Channel = handshake.Channel
			c.Connection_type = handshake.Connection_type
			c.Srv.AddClient(c)
		case "generate_key":
			key := c.Srv.GenerateKey()
			c.SendMsg(Msg{
				"type": "generate_key",
				"key":  key,
			})
			c.Srv.Log.Printf("For client %d generated key \"%s\"\n", c.ID, key)
		default:
			c.Srv.Log.Printf("Unknown Type field from client %d: \"%s\"\n", c.ID, handshake.Type)
		}
	}
}

func (c *Client) Close() {
	if c.Channel != "" {
		c.Srv.RemoveClient(c)
	}
	c.Conn.Close()
	c.Srv.Log.Printf("Disconnected client %d %s\n", c.ID, c.Conn.RemoteAddr())
}

func (c *Client) AsMap() Msg {
	return Msg{
		"id":              c.ID,
		"connection_type": c.Connection_type,
	}
}

func (c *Client) SendMsg(msg Msg) {
	line, err := json.Marshal(msg)
	if err != nil {
		panic(err)
	}
	line = append(line, Delimiter)
	c.SendLine(line)
}

func (c *Client) SendLine(line []byte) {
	if _, err := c.Conn.Write(line); err != nil {
		c.Srv.Log.Printf("Sending data error to client %d: %s\n", c.ID, err)
	}
}

type tcpKeepAliveListener struct {
	*net.TCPListener
}

func (ln tcpKeepAliveListener) Accept() (net.Conn, error) {
	tc, err := ln.AcceptTCP()
	if err != nil {
		return nil, err
	}
	tc.SetKeepAlive(true)
	tc.SetKeepAlivePeriod(KeepAlivePeriod)
	return tc, nil
}

type Server struct {
	Addr        string
	Certificate tls.Certificate
	Log         *log.Logger
	sync.RWMutex
	Channels map[string]Channel
}

func (s *Server) Start() {
	var clientID uint

	config := &tls.Config{
		Certificates:             []tls.Certificate{s.Certificate},
		PreferServerCipherSuites: true,
		MinVersion:               tls.VersionTLS12,
	}

	ln, err := net.Listen("tcp", s.Addr)
	if err != nil {
		s.Log.Fatalf("Listener error on %s: %s\n", s.Addr, err)
	}

	ln = tls.NewListener(tcpKeepAliveListener{ln.(*net.TCPListener)}, config)
	defer ln.Close()
	s.Log.Printf("Server started successfully on \"%s\"\n", s.Addr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			s.Log.Printf("Server error: %s\n", err)
			break
		}

		clientID++

		client := &Client{
			Conn: conn,
			ID:   clientID,
			Srv:  s,
		}

		go client.Handler()
	}
}

func (s *Server) SendMsgToChannel(client *Client, msg Msg) {
	line, err := json.Marshal(msg)
	if err != nil {
		panic(err)
	}
	line = append(line, Delimiter)
	s.SendLineToChannel(client, line)
}

func (s *Server) SendLineToChannel(client *Client, line []byte) {
	s.RLock()
	for r := range s.Channels[client.Channel] {
		if client != r {
			r.SendLine(line)
		}
	}
	s.RUnlock()
}

func (s *Server) AddClient(client *Client) {
	s.Lock()
	if s.Channels[client.Channel] == nil {
		s.Channels[client.Channel] = make(Channel)
	}
	s.Channels[client.Channel][client] = struct{}{}
	s.Unlock()

	var clients []Msg
	var clientsID []uint

	s.RLock()
	for c := range s.Channels[client.Channel] {
		if c != client {
			clients = append(clients, c.AsMap())
			clientsID = append(clientsID, c.ID)
		}
	}
	s.RUnlock()

	client.SendMsg(Msg{
		"type":     "channel_joined",
		"channel":  client.Channel,
		"user_ids": clientsID,
		"clients":  clients,
	})

	s.SendMsgToChannel(client, Msg{
		"type":    "client_joined",
		"user_id": client.ID,
		"client":  client.AsMap(),
	})

	s.Log.Printf("Client %d joined to \"%s\" channel as %s\n", client.ID, client.Channel, client.Connection_type)
}

func (s *Server) RemoveClient(client *Client) {
	s.SendMsgToChannel(client, Msg{
		"type":    "client_left",
		"user_id": client.ID,
		"client":  client.AsMap(),
	})

	s.Lock()
	delete(s.Channels[client.Channel], client)
	if len(s.Channels[client.Channel]) == 0 {
		delete(s.Channels, client.Channel)
	}
	s.Unlock()

	s.Log.Printf("Client %d removed from \"%s\" channel\n", client.ID, client.Channel)
}

func (s *Server) GenerateKey() (key string) {
	for {
		rand.Seed(time.Now().UnixNano())
		key = strconv.Itoa(rand.Intn(89999999) + 10000000)

		s.RLock()
		_, exist := s.Channels[key]
		s.RUnlock()

		if !exist {
			return key
		}
	}
}

func main() {
	addr := flag.String("addr", ":6837", "")
	certificatePath := flag.String("cert", "server.pem", "")
	flag.Parse()

	certificate, err := tls.LoadX509KeyPair(*certificatePath, *certificatePath)
	if err != nil {
		log.Fatalf("Certificate loading error: %s\n", err)
	}

	server := &Server{
		Channels:    make(map[string]Channel),
		Addr:        *addr,
		Certificate: certificate,
		Log:         log.New(os.Stdout, "", log.Ltime),
	}

	server.Start()
}
