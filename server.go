package main

import (
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

type Client struct {
	net.Conn
	Id              uint64
	Srv             *Server
	Key             string
	ProtocolVersion float64
	ConnectionType  string
}

func (c *Client) ClientHandler() {
	c.Srv.Log.Printf("Connected client %d %s\n", c.Id, c.RemoteAddr())
	buffer := make([]byte, 1024*32)

	for {
		n, err := c.Read(buffer)

		if err != nil {
			c.Srv.Log.Printf("Error receiving data from client %d: %s\n", c.Id, err)
			break
		}

		data := make(Msg)
		if err := json.Unmarshal(buffer[:n], &data); err != nil {
			c.Srv.Log.Printf("JSON data of client %d: %s\n", c.Id, err)
			continue
		}

		if _, ok := data["type"].(string); !ok {
			c.Srv.Log.Printf("No \"Type\" key for client %d\n", c.Id)
			break
		}

		if c.Key != "" {
			if _, ok := data["origin"]; !ok {
				data["origin"] = c.Id
			}

			c.Srv.SendToChannel(c, data)
			continue
		}

		switch data["type"] {
		case "protocol_version":
			c.ProtocolVersion, _ = data["version"].(float64)
		case "join":
			c.Srv.AddClient(c, data)
		case "generate_key":
			c.Generate_key()
		}
	}

	if c.Key != "" {
		c.Srv.RemoveClient(c)
	}

	c.Close()
	c.Srv.Log.Printf("Disconnected client %d %s\n", c.Id, c.RemoteAddr())
}

func (c *Client) Generate_key() {
	var key string

	for key == "" || c.Srv.ChannelExist(key) {
		rand.Seed(time.Now().UnixNano())
		key = strconv.Itoa(rand.Intn(89999999) + 10000000)
	}

	c.Send(Msg{
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

func (c *Client) Send(data Msg) {
	if c.ProtocolVersion <= 1 {
		delete(data, "origin")
		delete(data, "clients")
		delete(data, "client")
	}

	buffer, err := json.Marshal(data)
	if err != nil {
		c.Srv.Log.Println("JSON error:", err)
		return
	}

	buffer = append(buffer, '\n')
	if _, err := c.Write(buffer); err != nil {
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

		go client.ClientHandler()
	}
}

func (s *Server) SendToChannel(client *Client, data Msg) {
	s.RLock()
	for r := range s.Channels[client.Key] {
		if client != r {
			r.Send(data)
		}
	}
	s.RUnlock()
}

func (s *Server) AddClient(client *Client, data Msg) {
	client.Key, _ = data["channel"].(string)
	if client.Key == "" {
		return
	}

	if connection_type, ok := data["connection_type"].(string); ok {
		client.ConnectionType = connection_type
	}

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

	client.Send(Msg{
		"type":     "channel_joined",
		"channel":  client.Key,
		"user_ids": clientsID,
		"clients":  clients,
	})

	s.SendToChannel(client, Msg{
		"type":    "client_joined",
		"user_id": client.Id,
		"client":  client.AsMap(),
	})

	s.Log.Printf("Client %d joined to channel %s as %s\n", client.Id, client.Key, client.ConnectionType)
}

func (s *Server) RemoveClient(client *Client) {
	s.SendToChannel(client, Msg{
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
