package cliparser

// ActionType identifies the CLI command to execute.
type ActionType string

const (
	ActionHelp           ActionType = "help"
	ActionShoutSend      ActionType = "shout_send"
	ActionShoutRec       ActionType = "shout_rec"
	ActionLinkShareServe ActionType = "linkshare_serve"
	ActionLinkShareSend  ActionType = "linkshare_send"
)

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
}
