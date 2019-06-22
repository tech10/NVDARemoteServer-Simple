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

type Msg map[string]interface{}
type Channel map[*Client]struct{}

type Handshake struct {
	Type            string
	Version         float64
	Channel         string
	Connection_type string
}

type Client struct {
	Conn            net.Conn
	Id              uint64
	Srv             *Server
	Key             string
	ProtocolVersion float64
	ConnectionType  string
}

func (c *Client) Handler() {
	c.Srv.Log.Printf("Connected client %d %s\n", c.Id, c.Conn.RemoteAddr())
	buffer := bufio.NewReaderSize(c.Conn, 1024*32)
	defer c.Close()

	for {
		line, err := buffer.ReadSlice('\n')
		if err != nil && err != bufio.ErrBufferFull {
			c.Srv.Log.Printf("Error receiving data from client %d: %s\n", c.Id, err)
			return
		}

		if c.Key != "" {
			c.Srv.SendLineToChannel(c, line)
			continue
		}

		handshake := new(Handshake)
		if err := json.Unmarshal(line, handshake); err != nil {
			c.Srv.Log.Printf("JSON data of client %d: %s\n", c.Id, err)
			return
		}

		switch handshake.Type {
		case "protocol_version":
			c.ProtocolVersion = handshake.Version
		case "join":
			c.Srv.AddClient(c, handshake)
		case "generate_key":
			c.Generate_key()
		default:
			return
		}
	}
}

func (c *Client) Close() {
	if c.Key != "" {
		c.Srv.RemoveClient(c)
	}

	c.Conn.Close()
	c.Srv.Log.Printf("Disconnected client %d %s\n", c.Id, c.Conn.RemoteAddr())
}

func (c *Client) Generate_key() {
	var key string

	for key == "" || c.Srv.ChannelExist(key) {
		rand.Seed(time.Now().UnixNano())
		key = strconv.Itoa(rand.Intn(89999999) + 10000000)
	}

	c.SendMsg(Msg{
		"type": "generate_key",
		"key":  key,
	})

	c.Srv.Log.Println("For client", c.Id, "generated key", key)
}

func (c *Client) AsMap() Msg {
	return Msg{
		"id":              c.Id,
		"connection_type": c.ConnectionType,
	}
}

func (c *Client) SendMsg(msg Msg) {
	line, err := json.Marshal(msg)
	if err != nil {
		panic(err)
	}

	line = append(line, '\n')
	c.SendLine(line)
}

func (c *Client) SendLine(line []byte) {
	if _, err := c.Conn.Write(line); err != nil {
		c.Srv.Log.Println("Error when sending data to client", c.Id, "-", err)
	}
}

type Server struct {
	Addr            string
	CertificatePath string
	Log             *log.Logger
	sync.RWMutex
	Channels map[string]Channel
}

func (s *Server) Start() {
	ticker := time.NewTicker(time.Minute * 5)
	go s.Pinger(ticker)
	var clientID uint64

	certificate, err := tls.LoadX509KeyPair(s.CertificatePath, s.CertificatePath)
	if err != nil {
		s.Log.Fatalf("Certificate load error: %s\n", err)
	}

	TLSConfig := &tls.Config{
		Certificates:             []tls.Certificate{certificate},
		PreferServerCipherSuites: true,
		MinVersion:               tls.VersionTLS12,
	}

	listener, err := tls.Listen("tcp", s.Addr, TLSConfig)
	if err != nil {
		s.Log.Fatalf("Listener error on %s: %s\n", s.Addr, err)
	}
	defer listener.Close()
	s.Log.Printf("Server successfully started on \"%s\"\n", s.Addr)

	for {
		conn, err := listener.Accept()
		if err != nil {
			s.Log.Printf("Client connection error: %s\n", err)
			continue
		}

		clientID++

		client := &Client{
			Conn: conn,
			Id:   clientID,
			Srv:  s,
		}

		go client.Handler()
	}

	ticker.Stop()
}

func (s *Server) Pinger(ticker *time.Ticker) {
	for _ = range ticker.C {
		for _, channel := range s.Channels {
			for client := range channel {
				client.SendMsg(Msg{
					"type": "ping",
				})
			}
		}
	}
}

func (s *Server) SendMsgToChannel(client *Client, msg Msg) {
	line, err := json.Marshal(msg)
	if err != nil {
		panic(err)
	}

	line = append(line, '\n')
	s.SendLineToChannel(client, line)
}

func (s *Server) SendLineToChannel(client *Client, line []byte) {
	s.RLock()
	for r := range s.Channels[client.Key] {
		if client != r {
			r.SendLine(line)
		}
	}
	s.RUnlock()
}

func (s *Server) AddClient(client *Client, handshake *Handshake) {
	if handshake.Channel == "" {
		return
	}

	client.Key = handshake.Channel
	client.ConnectionType = handshake.Connection_type

	s.Lock()
	if s.Channels[client.Key] == nil {
		s.Channels[client.Key] = make(Channel)
	}
	s.Channels[client.Key][client] = struct{}{}
	s.Unlock()

	var clients []Msg
	var clientsID []uint64

	s.RLock()
	for c := range s.Channels[client.Key] {
		clients = append(clients, c.AsMap())
		clientsID = append(clientsID, c.Id)
	}
	s.RUnlock()

	client.SendMsg(Msg{
		"type":     "channel_joined",
		"channel":  client.Key,
		"user_ids": clientsID,
		"clients":  clients,
	})

	s.SendMsgToChannel(client, Msg{
		"type":    "client_joined",
		"user_id": client.Id,
		"client":  client.AsMap(),
	})

	s.Log.Printf("Client %d joined to channel %s as %s\n", client.Id, client.Key, client.ConnectionType)
}

func (s *Server) RemoveClient(client *Client) {
	s.SendMsgToChannel(client, Msg{
		"type":    "client_left",
		"user_id": client.Id,
		"client":  client.AsMap(),
	})

	s.Lock()
	delete(s.Channels[client.Key], client)
	if len(s.Channels[client.Key]) == 0 {
		delete(s.Channels, client.Key)
	}
	s.Unlock()

	s.Log.Printf("Client %d removed from channel %s\n", client.Id, client.Key)
}

func (s *Server) ChannelExist(channel string) bool {
	s.RLock()
	_, exist := s.Channels[channel]
	s.RUnlock()
	return exist
}

func main() {
	addr := flag.String("addr", ":6837", "")
	certificatePath := flag.String("cert", "server.pem", "")
	flag.Parse()

	server := &Server{
		Channels:        make(map[string]Channel),
		Addr:            *addr,
		CertificatePath: *certificatePath,
		Log:             log.New(os.Stdout, "", log.Ltime),
	}

	server.Start()
}
