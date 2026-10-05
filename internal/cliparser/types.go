package cliparser

import "time"

// ActionType identifies the CLI command to execute.
type ActionType string

const (
	ActionHelp           ActionType = "help"
	ActionShoutSend      ActionType = "shout_send"
	ActionShoutRec       ActionType = "shout_rec"
	ActionLinkShareServe ActionType = "linkshare_serve"
	ActionLinkShareSend  ActionType = "linkshare_send"
	ActionOtinSend       ActionType = "otin_send"
	ActionOtinRec        ActionType = "otin_rec"
	ActionDirectSend     ActionType = "direct_send"
	ActionDirectRec      ActionType = "direct_rec"
	ActionRelayServe     ActionType = "relay_serve"
)

// DirectSendConfig holds parsed arguments for 'mitto direct send' / 'mitto manual send'.
type DirectSendConfig struct {
	Files      []string // Validated local file paths
	Addr       string   // Target receiver address or comma-separated candidate addresses (-a / --addr)
	Codephrase string   // Shared 3-word PAKE codephrase (-c / --code / --codephrase)
}

// DirectRecConfig holds parsed arguments for 'mitto direct rec' / 'mitto manual rec'.
type DirectRecConfig struct {
	Dir        string // Target directory to store incoming files (-d / --dir, default: ".")
	Port       int    // Preferred listening port (-p / --port, default: 0 for candidate pool)
	Codephrase string // Preset codephrase (-c / --code / --codephrase, empty for auto-gen)
	NoUPnP     bool   // Disable UPnP router port mapping (--no-upnp)
}

// ShoutSendConfig holds parsed arguments for 'mitto send' / 'mitto shout send'.
type ShoutSendConfig struct {
	Files  []string // Validated local file paths
	Target string   // Target receiver device name or IP (-u / --target)
	Token  string   // Optional receiver token (-t / --token)
}

// ShoutRecConfig holds parsed arguments for 'mitto rec' / 'mitto shout rec'.
type ShoutRecConfig struct {
	Dir   string // Target directory to store incoming files (-d / --dir, default: ".")
	Port  int    // Transfer port to listen on (-p / --port, default: 0)
	Token string // Preset token (-t / --token)
}

// OtinSendConfig holds parsed arguments for 'mitto otin send'.
type OtinSendConfig struct {
	Files         []string // Validated local file paths
	Addr          string   // Receiver's Tailcat address string (-a / --addr)
	Codephrase    string   // Shared 3-word PAKE codephrase (-c / --code / --codephrase)
	RelayAddr     string   // Relay server host:port (-r / --relay)
	RelayPassword string   // Optional relay server password (--relay-pass / --relay-password)
}

// OtinRecConfig holds parsed arguments for 'mitto otin rec'.
type OtinRecConfig struct {
	Dir           string // Target directory to store incoming files (-d / --dir, default: ".")
	Port          int    // Virtual tunnel port to listen on (-p / --port, default: 42201)
	Codephrase    string // Preset codephrase (-c / --code / --codephrase, empty for auto-gen)
	RelayAddr     string // Relay server host:port (-r / --relay)
	RelayPassword string // Optional relay server password (--relay-pass / --relay-password)
}

// RelayServeConfig holds parsed arguments for 'mitto relay serve'.
type RelayServeConfig struct {
	Host            string
	Port            int
	Password        string
	Banner          string
	RoomTTL         time.Duration
	MaxWaitingRooms int
	RateLimit       int
	RateWindow      time.Duration
}

// LinkShareTarget represents a destination URL, token, and file list for LinkShare upload.
type LinkShareTarget struct {
	URL   string   // Normalized base URL (e.g. http://192.168.1.5:8080)
	Token string   // Optional access token
	Files []string // Validated local file paths
}

// LinkShareServeConfig holds parsed arguments for 'mitto linkshare serve'.
type LinkShareServeConfig struct {
	Dir   string // Target directory to store uploads (-d / --dir, default: ".")
	Port  int    // HTTP port to listen on (-p / --port, default: 0)
	Token string // Preset token (-t / --token)
}

// LinkShareSendConfig holds parsed arguments for 'mitto linkshare send'.
type LinkShareSendConfig struct {
	Targets  []LinkShareTarget // One or more destination target groups
	Compress string            // Compression: "zstd", "gzip", "none" (default: "zstd")
}

// ParsedCommand represents the structured result of parsing CLI arguments.
type ParsedCommand struct {
	Action         ActionType
	ShoutSend      *ShoutSendConfig
	ShoutRec       *ShoutRecConfig
	LinkShareServe *LinkShareServeConfig
	LinkShareSend  *LinkShareSendConfig
	OtinSend       *OtinSendConfig
	OtinRec        *OtinRecConfig
	DirectSend     *DirectSendConfig
	DirectRec      *DirectRecConfig
	RelayServe     *RelayServeConfig
}
