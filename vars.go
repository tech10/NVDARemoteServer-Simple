package main

import "time"

const (
	ReadBufSize           = 65536
	WriteBufSize          = 1024
	KeepAlivePeriod       = time.Second * 15
	WriteDeadlineDuration = time.Second * 4
	Delimiter             = '\n'

	// protocol types.
	TypeJoin             = "join"
	TypeGenerateKey      = "generate_key"
	TypeProtocolVersion  = "protocol_version"
	TypeMotd             = "motd"
	TypeMotdForceDisplay = "force_display"
	TypeChannelJoined    = "channel_joined"
	TypeChannel          = "channel"
	TypeClientJoined     = "client_joined"
	TypeClientLeft       = "client_left"
	TypeUserID           = "user_id"
	TypeClient           = "client"
	TypeUserIDs          = "user_ids"
	TypeClients          = "clients"
	TypeID               = "id"
	TypeConnectionType   = "connection_type"
	TypeNvdaNotConnected = "nvda_not_connected"
	TypeController       = "master"
	TypeControlled       = "slave"
)

type (
	Msg     map[string]any
	Channel map[*Client]struct{}
)

type Handshake struct {
	Type           string `json:"type"`
	Channel        string `json:"channel,omitempty"`
	ConnectionType string `json:"connection_type,omitempty"`
}

var (
	MsgErr          = Msg{"type": "error", "error": "invalid_parameters"}
	MsgNotConnected = Msg{"type": TypeNvdaNotConnected}
)
