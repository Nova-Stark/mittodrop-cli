package cliparser

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Parse inspects CLI arguments, routes to appropriate command parser,
// and validates file arguments non-destructively (missing files skipped with warnFn).
func Parse(args []string, warnFn func(string)) (*ParsedCommand, error) {
	if len(args) == 0 {
		return &ParsedCommand{Action: ActionHelp}, nil
	}

	cmd := args[0]
	subArgs := args[1:]

	switch cmd {
	case "help", "-h", "--help":
		return &ParsedCommand{Action: ActionHelp}, nil

	case "send":
		cfg, err := parseShoutSendArgs(subArgs, warnFn)
		if err != nil {
			return nil, err
		}
		return &ParsedCommand{
			Action:    ActionShoutSend,
			ShoutSend: cfg,
		}, nil

	case "rec", "receive":
		cfg, err := parseShoutRecArgs(subArgs)
		if err != nil {
			return nil, err
		}
		return &ParsedCommand{
			Action:   ActionShoutRec,
			ShoutRec: cfg,
		}, nil

	case "shout":
		if len(subArgs) == 0 {
			return nil, errors.New("cliparser: shout requires a subcommand: 'send' or 'rec'")
		}
		switch subArgs[0] {
		case "send":
			cfg, err := parseShoutSendArgs(subArgs[1:], warnFn)
			if err != nil {
				return nil, err
			}
			return &ParsedCommand{
				Action:    ActionShoutSend,
				ShoutSend: cfg,
			}, nil
		case "rec", "receive":
			cfg, err := parseShoutRecArgs(subArgs[1:])
			if err != nil {
				return nil, err
			}
			return &ParsedCommand{
				Action:   ActionShoutRec,
				ShoutRec: cfg,
			}, nil
		default:
			return nil, fmt.Errorf("cliparser: unknown shout subcommand %q (valid: send, rec)", subArgs[0])
		}

	case "linkshare":
		if len(subArgs) == 0 {
			return nil, errors.New("cliparser: linkshare requires a subcommand: 'serve' or 'send'")
		}
		switch subArgs[0] {
		case "serve", "rec", "receive":
			cfg, err := parseLinkShareServeArgs(subArgs[1:])
			if err != nil {
				return nil, err
			}
			return &ParsedCommand{
				Action:         ActionLinkShareServe,
				LinkShareServe: cfg,
			}, nil

		case "send":
			cfg, err := parseLinkShareSendArgs(subArgs[1:], warnFn)
			if err != nil {
				return nil, err
			}
			return &ParsedCommand{
				Action:        ActionLinkShareSend,
				LinkShareSend: cfg,
			}, nil

		default:
			return nil, fmt.Errorf("cliparser: unknown linkshare subcommand %q (valid: serve, send)", subArgs[0])
		}

	case "otin":
		if len(subArgs) == 0 {
			return nil, errors.New("cliparser: otin requires a subcommand: 'send' or 'rec'")
		}
		switch subArgs[0] {
		case "send":
			cfg, err := parseOtinSendArgs(subArgs[1:], warnFn)
			if err != nil {
				return nil, err
			}
			return &ParsedCommand{
				Action:   ActionOtinSend,
				OtinSend: cfg,
			}, nil
		case "rec", "receive":
			cfg, err := parseOtinRecArgs(subArgs[1:])
			if err != nil {
				return nil, err
			}
			return &ParsedCommand{
				Action:  ActionOtinRec,
				OtinRec: cfg,
			}, nil
		case "relay":
			relayArgs := subArgs[1:]
			if len(relayArgs) > 0 && (relayArgs[0] == "serve" || relayArgs[0] == "server") {
				relayArgs = relayArgs[1:]
			}
			cfg, err := parseRelayServeArgs(relayArgs)
			if err != nil {
				return nil, err
			}
			return &ParsedCommand{
				Action:     ActionRelayServe,
				RelayServe: cfg,
			}, nil
		default:
			return nil, fmt.Errorf("cliparser: unknown otin subcommand %q (valid: send, rec, relay)", subArgs[0])
		}

	case "relay":
		relayArgs := subArgs
		if len(subArgs) > 0 && (subArgs[0] == "serve" || subArgs[0] == "server") {
			relayArgs = subArgs[1:]
		}
		cfg, err := parseRelayServeArgs(relayArgs)
		if err != nil {
			return nil, err
		}
		return &ParsedCommand{
			Action:     ActionRelayServe,
			RelayServe: cfg,
		}, nil

	case "direct", "manual":
		if len(subArgs) == 0 {
			return nil, fmt.Errorf("cliparser: %s requires a subcommand: 'send' or 'rec'", cmd)
		}
		switch subArgs[0] {
		case "send":
			cfg, err := parseDirectSendArgs(subArgs[1:], warnFn)
			if err != nil {
				return nil, err
			}
			return &ParsedCommand{
				Action:     ActionDirectSend,
				DirectSend: cfg,
			}, nil
		case "rec", "receive":
			cfg, err := parseDirectRecArgs(subArgs[1:])
			if err != nil {
				return nil, err
			}
			return &ParsedCommand{
				Action:    ActionDirectRec,
				DirectRec: cfg,
			}, nil
		default:
			return nil, fmt.Errorf("cliparser: unknown %s subcommand %q (valid: send, rec)", cmd, subArgs[0])
		}

	default:
		return nil, fmt.Errorf("cliparser: unknown command %q (run 'mitto help' for usage)", cmd)
	}
}

func parseShoutSendArgs(args []string, warnFn func(string)) (*ShoutSendConfig, error) {
	cfg := &ShoutSendConfig{}
	var rawFiles []string

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "-f" || arg == "--file" || arg == "--files":
			i++
			for i < len(args) && !strings.HasPrefix(args[i], "-") {
				rawFiles = append(rawFiles, args[i])
				i++
			}

		case arg == "-u" || arg == "--user" || arg == "--target":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("cliparser: %s requires a receiver name or IP argument", arg)
			}
			cfg.Target = args[i+1]
			i += 2

		case strings.HasPrefix(arg, "-u=") || strings.HasPrefix(arg, "--user=") || strings.HasPrefix(arg, "--target="):
			cfg.Target = arg[strings.Index(arg, "=")+1:]
			i++

		case arg == "-t" || arg == "--token":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("cliparser: %s requires a token argument", arg)
			}
			cfg.Token = args[i+1]
			i += 2

		case strings.HasPrefix(arg, "-t=") || strings.HasPrefix(arg, "--token="):
			cfg.Token = arg[strings.Index(arg, "=")+1:]
			i++

		case !strings.HasPrefix(arg, "-"):
			rawFiles = append(rawFiles, arg)
			i++

		default:
			return nil, fmt.Errorf("cliparser: unrecognized flag %q", arg)
		}
	}

	if len(rawFiles) == 0 {
		return nil, errors.New("cliparser: no files specified to send (use -f or positional files)")
	}

	validFiles := filterValidFiles(rawFiles, warnFn)
	if len(validFiles) == 0 {
		return nil, errors.New("cliparser: no valid files to send")
	}

	cfg.Files = validFiles
	return cfg, nil
}

func parseShoutRecArgs(args []string) (*ShoutRecConfig, error) {
	cfg := &ShoutRecConfig{
		Dir: ".",
	}

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "-d" || arg == "--dir" || arg == "--output":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("cliparser: %s requires a directory path", arg)
			}
			cfg.Dir = args[i+1]
			i += 2

		case strings.HasPrefix(arg, "-d=") || strings.HasPrefix(arg, "--dir=") || strings.HasPrefix(arg, "--output="):
			cfg.Dir = arg[strings.Index(arg, "=")+1:]
			i++

		case arg == "-p" || arg == "--port":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("cliparser: %s requires a port number", arg)
			}
			port, err := strconv.Atoi(args[i+1])
			if err != nil || port < 0 || port > 65535 {
				return nil, fmt.Errorf("cliparser: invalid port %q", args[i+1])
			}
			cfg.Port = port
			i += 2

		case strings.HasPrefix(arg, "-p=") || strings.HasPrefix(arg, "--port="):
			val := arg[strings.Index(arg, "=")+1:]
			port, err := strconv.Atoi(val)
			if err != nil || port < 0 || port > 65535 {
				return nil, fmt.Errorf("cliparser: invalid port %q", val)
			}
			cfg.Port = port
			i++

		case arg == "-t" || arg == "--token":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("cliparser: %s requires a token argument", arg)
			}
			cfg.Token = args[i+1]
			i += 2

		case strings.HasPrefix(arg, "-t=") || strings.HasPrefix(arg, "--token="):
			cfg.Token = arg[strings.Index(arg, "=")+1:]
			i++

		default:
			return nil, fmt.Errorf("cliparser: unrecognized flag %q", arg)
		}
	}

	return cfg, nil
}

func parseLinkShareServeArgs(args []string) (*LinkShareServeConfig, error) {
	cfg := &LinkShareServeConfig{
		Dir: ".",
	}

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "-d" || arg == "--dir" || arg == "--output":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("cliparser: %s requires a directory path", arg)
			}
			cfg.Dir = args[i+1]
			i += 2

		case strings.HasPrefix(arg, "-d=") || strings.HasPrefix(arg, "--dir=") || strings.HasPrefix(arg, "--output="):
			cfg.Dir = arg[strings.Index(arg, "=")+1:]
			i++

		case arg == "-p" || arg == "--port":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("cliparser: %s requires a port number", arg)
			}
			port, err := strconv.Atoi(args[i+1])
			if err != nil || port < 0 || port > 65535 {
				return nil, fmt.Errorf("cliparser: invalid port %q", args[i+1])
			}
			cfg.Port = port
			i += 2

		case strings.HasPrefix(arg, "-p=") || strings.HasPrefix(arg, "--port="):
			val := arg[strings.Index(arg, "=")+1:]
			port, err := strconv.Atoi(val)
			if err != nil || port < 0 || port > 65535 {
				return nil, fmt.Errorf("cliparser: invalid port %q", val)
			}
			cfg.Port = port
			i++

		case arg == "-t" || arg == "--token":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("cliparser: %s requires a token argument", arg)
			}
			cfg.Token = args[i+1]
			i += 2

		case strings.HasPrefix(arg, "-t=") || strings.HasPrefix(arg, "--token="):
			cfg.Token = arg[strings.Index(arg, "=")+1:]
			i++

		default:
			return nil, fmt.Errorf("cliparser: unrecognized flag %q", arg)
		}
	}

	return cfg, nil
}

func parseLinkShareSendArgs(args []string, warnFn func(string)) (*LinkShareSendConfig, error) {
	var ufStrings []string
	var urls []string
	var files []string
	token := ""
	compress := "zstd"

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "-uf":
			if i+1 >= len(args) {
				return nil, errors.New("cliparser: -uf flag requires a quoted string argument")
			}
			ufStrings = append(ufStrings, args[i+1])
			i += 2

		case strings.HasPrefix(arg, "-uf="):
			ufStrings = append(ufStrings, strings.TrimPrefix(arg, "-uf="))
			i++

		case arg == "-u" || arg == "--url" || arg == "-ip":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("cliparser: %s requires an address or URL argument", arg)
			}
			for _, part := range strings.Split(args[i+1], ",") {
				part = strings.TrimSpace(part)
				if part != "" {
					urls = append(urls, part)
				}
			}
			i += 2

		case strings.HasPrefix(arg, "-u=") || strings.HasPrefix(arg, "--url=") || strings.HasPrefix(arg, "-ip="):
			val := arg[strings.Index(arg, "=")+1:]
			for _, part := range strings.Split(val, ",") {
				part = strings.TrimSpace(part)
				if part != "" {
					urls = append(urls, part)
				}
			}
			i++

		case arg == "-t" || arg == "--token":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("cliparser: %s requires a token argument", arg)
			}
			token = args[i+1]
			i += 2

		case strings.HasPrefix(arg, "-t=") || strings.HasPrefix(arg, "--token="):
			token = arg[strings.Index(arg, "=")+1:]
			i++

		case arg == "-c" || arg == "--compress":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("cliparser: %s requires a compression algorithm (zstd|gzip|none)", arg)
			}
			compress = args[i+1]
			i += 2

		case strings.HasPrefix(arg, "-c=") || strings.HasPrefix(arg, "--compress="):
			compress = arg[strings.Index(arg, "=")+1:]
			i++

		case arg == "-f" || arg == "--files":
			i++
			for i < len(args) && !strings.HasPrefix(args[i], "-") {
				files = append(files, args[i])
				i++
			}

		case !strings.HasPrefix(arg, "-"):
			files = append(files, arg)
			i++

		default:
			return nil, fmt.Errorf("cliparser: unrecognized flag %q", arg)
		}
	}

	cfg := &LinkShareSendConfig{
		Compress: compress,
	}

	// 1. If -uf is present, prioritize -uf and ignore other file/url flags
	if len(ufStrings) > 0 {
		for _, ufStr := range ufStrings {
			target, err := parseUFString(ufStr, warnFn)
			if err != nil {
				return nil, err
			}
			cfg.Targets = append(cfg.Targets, *target)
		}
		return cfg, nil
	}

	// 2. Standard fan-out mode (-u / -ip + files)
	if len(urls) == 0 {
		return nil, errors.New("cliparser: receiver address required (use -u, -ip, or -uf)")
	}
	if len(files) == 0 {
		return nil, errors.New("cliparser: no files specified to send")
	}

	validFiles := filterValidFiles(files, warnFn)
	if len(validFiles) == 0 {
		return nil, errors.New("cliparser: no valid files to send")
	}

	for _, u := range urls {
		normURL, urlToken, err := ExtractAndNormalizeURL(u)
		if err != nil {
			return nil, err
		}
		finalToken := token
		if finalToken == "" {
			finalToken = urlToken
		}

		cfg.Targets = append(cfg.Targets, LinkShareTarget{
			URL:   normURL,
			Token: finalToken,
			Files: validFiles,
		})
	}

	return cfg, nil
}

func parseUFString(s string, warnFn func(string)) (*LinkShareTarget, error) {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return nil, errors.New("cliparser: empty -uf string")
	}

	rawURL := fields[0]
	var rawFiles []string
	token := ""

	i := 1
	for i < len(fields) {
		tokenArg := fields[i]
		switch {
		case tokenArg == "-t" || tokenArg == "--token":
			if i+1 >= len(fields) {
				return nil, errors.New("cliparser: -t inside -uf requires a token value")
			}
			token = fields[i+1]
			i += 2
		case strings.HasPrefix(tokenArg, "-t=") || strings.HasPrefix(tokenArg, "--token="):
			token = tokenArg[strings.Index(tokenArg, "=")+1:]
			i++
		case strings.HasPrefix(tokenArg, "token="):
			token = strings.TrimPrefix(tokenArg, "token=")
			i++
		default:
			rawFiles = append(rawFiles, tokenArg)
			i++
		}
	}

	if len(rawFiles) == 0 {
		return nil, fmt.Errorf("cliparser: no files specified in -uf for target %s", rawURL)
	}

	normURL, urlToken, err := ExtractAndNormalizeURL(rawURL)
	if err != nil {
		return nil, err
	}
	if token == "" {
		token = urlToken
	}

	validFiles := filterValidFiles(rawFiles, warnFn)
	if len(validFiles) == 0 {
		return nil, fmt.Errorf("cliparser: no valid files in -uf for target %s", rawURL)
	}

	return &LinkShareTarget{
		URL:   normURL,
		Token: token,
		Files: validFiles,
	}, nil
}

func filterValidFiles(files []string, warnFn func(string)) []string {
	var valid []string
	for _, f := range files {
		if _, err := os.Stat(f); err != nil {
			if warnFn != nil {
				warnFn(fmt.Sprintf("File not found: %q, skipping...", f))
			}
			continue
		}
		valid = append(valid, f)
	}
	return valid
}

// ExtractAndNormalizeURL ensures http scheme and extracts any embedded ?token= query parameter.
func ExtractAndNormalizeURL(raw string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", errors.New("cliparser: empty URL")
	}

	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		raw = "http://" + raw
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("cliparser: invalid URL %q: %w", raw, err)
	}

	token := parsed.Query().Get("token")
	cleanURL := fmt.Sprintf("%s://%s%s", parsed.Scheme, parsed.Host, parsed.Path)
	cleanURL = strings.TrimRight(cleanURL, "/")

	return cleanURL, token, nil
}

func parseOtinSendArgs(args []string, warnFn func(string)) (*OtinSendConfig, error) {
	cfg := &OtinSendConfig{}
	var rawFiles []string

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "-a" || arg == "--addr" || arg == "--address" || arg == "-u" || arg == "--target":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("cliparser: %s requires a receiver address", arg)
			}
			cfg.Addr = args[i+1]
			i += 2

		case strings.HasPrefix(arg, "-a=") || strings.HasPrefix(arg, "--addr=") || strings.HasPrefix(arg, "--address=") ||
			strings.HasPrefix(arg, "-u=") || strings.HasPrefix(arg, "--target="):
			cfg.Addr = arg[strings.Index(arg, "=")+1:]
			i++

		case arg == "-c" || arg == "--code" || arg == "--codephrase":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("cliparser: %s requires a codephrase", arg)
			}
			cfg.Codephrase = args[i+1]
			i += 2

		case strings.HasPrefix(arg, "-c=") || strings.HasPrefix(arg, "--code=") || strings.HasPrefix(arg, "--codephrase="):
			cfg.Codephrase = arg[strings.Index(arg, "=")+1:]
			i++

		case arg == "-r" || arg == "--relay":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("cliparser: %s requires a relay server address (host:port)", arg)
			}
			cfg.RelayAddr = args[i+1]
			i += 2

		case strings.HasPrefix(arg, "-r=") || strings.HasPrefix(arg, "--relay="):
			cfg.RelayAddr = arg[strings.Index(arg, "=")+1:]
			i++

		case arg == "--relay-pass" || arg == "--relay-password":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("cliparser: %s requires a password string", arg)
			}
			cfg.RelayPassword = args[i+1]
			i += 2

		case strings.HasPrefix(arg, "--relay-pass=") || strings.HasPrefix(arg, "--relay-password="):
			cfg.RelayPassword = arg[strings.Index(arg, "=")+1:]
			i++

		case arg == "-f" || arg == "--file" || arg == "--files":
			i++
			for i < len(args) && !strings.HasPrefix(args[i], "-") {
				rawFiles = append(rawFiles, args[i])
				i++
			}

		case !strings.HasPrefix(arg, "-"):
			rawFiles = append(rawFiles, arg)
			i++

		default:
			return nil, fmt.Errorf("cliparser: unrecognized flag %q", arg)
		}
	}

	if len(rawFiles) == 0 {
		return nil, errors.New("cliparser: no files specified to send (use -f or positional files)")
	}

	validFiles := filterValidFiles(rawFiles, warnFn)
	if len(validFiles) == 0 {
		return nil, errors.New("cliparser: no valid files to send")
	}

	cfg.Files = validFiles
	return cfg, nil
}

func parseOtinRecArgs(args []string) (*OtinRecConfig, error) {
	cfg := &OtinRecConfig{
		Dir:  ".",
		Port: 42201,
	}

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "-r" || arg == "--relay":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("cliparser: %s requires a relay server address (host:port)", arg)
			}
			cfg.RelayAddr = args[i+1]
			i += 2

		case strings.HasPrefix(arg, "-r=") || strings.HasPrefix(arg, "--relay="):
			cfg.RelayAddr = arg[strings.Index(arg, "=")+1:]
			i++

		case arg == "--relay-pass" || arg == "--relay-password":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("cliparser: %s requires a password string", arg)
			}
			cfg.RelayPassword = args[i+1]
			i += 2

		case strings.HasPrefix(arg, "--relay-pass=") || strings.HasPrefix(arg, "--relay-password="):
			cfg.RelayPassword = arg[strings.Index(arg, "=")+1:]
			i++

		case arg == "-d" || arg == "--dir" || arg == "--output":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("cliparser: %s requires a directory path", arg)
			}
			cfg.Dir = args[i+1]
			i += 2

		case strings.HasPrefix(arg, "-d=") || strings.HasPrefix(arg, "--dir=") || strings.HasPrefix(arg, "--output="):
			cfg.Dir = arg[strings.Index(arg, "=")+1:]
			i++

		case arg == "-p" || arg == "--port":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("cliparser: %s requires a port number", arg)
			}
			port, err := strconv.Atoi(args[i+1])
			if err != nil || port < 0 || port > 65535 {
				return nil, fmt.Errorf("cliparser: invalid port %q", args[i+1])
			}
			cfg.Port = port
			i += 2

		case strings.HasPrefix(arg, "-p=") || strings.HasPrefix(arg, "--port="):
			val := arg[strings.Index(arg, "=")+1:]
			port, err := strconv.Atoi(val)
			if err != nil || port < 0 || port > 65535 {
				return nil, fmt.Errorf("cliparser: invalid port %q", val)
			}
			cfg.Port = port
			i++

		case arg == "-c" || arg == "--code" || arg == "--codephrase":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("cliparser: %s requires a codephrase", arg)
			}
			cfg.Codephrase = args[i+1]
			i += 2

		case strings.HasPrefix(arg, "-c=") || strings.HasPrefix(arg, "--code=") || strings.HasPrefix(arg, "--codephrase="):
			cfg.Codephrase = arg[strings.Index(arg, "=")+1:]
			i++

		default:
			return nil, fmt.Errorf("cliparser: unrecognized flag %q", arg)
		}
	}

	return cfg, nil
}

func parseRelayServeArgs(args []string) (*RelayServeConfig, error) {
	cfg := &RelayServeConfig{
		Host:            "0.0.0.0",
		Port:            9007,
		Banner:          "mittodrop-relay",
		RoomTTL:         30 * time.Minute,
		MaxWaitingRooms: 1000,
		RateLimit:       60,
		RateWindow:      1 * time.Minute,
	}

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "-p" || arg == "--port":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("cliparser: %s requires a port number", arg)
			}
			p, err := strconv.Atoi(args[i+1])
			if err != nil || p <= 0 || p > 65535 {
				return nil, fmt.Errorf("cliparser: invalid port %q", args[i+1])
			}
			cfg.Port = p
			i += 2

		case strings.HasPrefix(arg, "-p=") || strings.HasPrefix(arg, "--port="):
			val := arg[strings.Index(arg, "=")+1:]
			p, err := strconv.Atoi(val)
			if err != nil || p <= 0 || p > 65535 {
				return nil, fmt.Errorf("cliparser: invalid port %q", val)
			}
			cfg.Port = p
			i++

		case arg == "-h" || arg == "--host" || arg == "--bind":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("cliparser: %s requires a host address", arg)
			}
			cfg.Host = args[i+1]
			i += 2

		case strings.HasPrefix(arg, "-h=") || strings.HasPrefix(arg, "--host=") || strings.HasPrefix(arg, "--bind="):
			cfg.Host = arg[strings.Index(arg, "=")+1:]
			i++

		case arg == "--pass" || arg == "--password":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("cliparser: %s requires a password string", arg)
			}
			cfg.Password = args[i+1]
			i += 2

		case strings.HasPrefix(arg, "--pass=") || strings.HasPrefix(arg, "--password="):
			cfg.Password = arg[strings.Index(arg, "=")+1:]
			i++

		case arg == "--banner":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("cliparser: %s requires a banner string", arg)
			}
			cfg.Banner = args[i+1]
			i += 2

		case strings.HasPrefix(arg, "--banner="):
			cfg.Banner = arg[strings.Index(arg, "=")+1:]
			i++

		case arg == "--ttl":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("cliparser: %s requires a duration (e.g. 30m, 1h)", arg)
			}
			d, err := time.ParseDuration(args[i+1])
			if err != nil || d <= 0 {
				return nil, fmt.Errorf("cliparser: invalid ttl duration %q", args[i+1])
			}
			cfg.RoomTTL = d
			i += 2

		case strings.HasPrefix(arg, "--ttl="):
			val := arg[strings.Index(arg, "=")+1:]
			d, err := time.ParseDuration(val)
			if err != nil || d <= 0 {
				return nil, fmt.Errorf("cliparser: invalid ttl duration %q", val)
			}
			cfg.RoomTTL = d
			i++

		case arg == "--max-rooms":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("cliparser: %s requires a number", arg)
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil || n <= 0 {
				return nil, fmt.Errorf("cliparser: invalid max-rooms %q", args[i+1])
			}
			cfg.MaxWaitingRooms = n
			i += 2

		case strings.HasPrefix(arg, "--max-rooms="):
			val := arg[strings.Index(arg, "=")+1:]
			n, err := strconv.Atoi(val)
			if err != nil || n <= 0 {
				return nil, fmt.Errorf("cliparser: invalid max-rooms %q", val)
			}
			cfg.MaxWaitingRooms = n
			i++

		case arg == "--rate-limit":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("cliparser: %s requires a number", arg)
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil || n <= 0 {
				return nil, fmt.Errorf("cliparser: invalid rate-limit %q", args[i+1])
			}
			cfg.RateLimit = n
			i += 2

		case strings.HasPrefix(arg, "--rate-limit="):
			val := arg[strings.Index(arg, "=")+1:]
			n, err := strconv.Atoi(val)
			if err != nil || n <= 0 {
				return nil, fmt.Errorf("cliparser: invalid rate-limit %q", val)
			}
			cfg.RateLimit = n
			i++

		case arg == "--rate-window":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("cliparser: %s requires a duration (e.g. 1m, 10s)", arg)
			}
			d, err := time.ParseDuration(args[i+1])
			if err != nil || d <= 0 {
				return nil, fmt.Errorf("cliparser: invalid rate-window duration %q", args[i+1])
			}
			cfg.RateWindow = d
			i += 2

		case strings.HasPrefix(arg, "--rate-window="):
			val := arg[strings.Index(arg, "=")+1:]
			d, err := time.ParseDuration(val)
			if err != nil || d <= 0 {
				return nil, fmt.Errorf("cliparser: invalid rate-window duration %q", val)
			}
			cfg.RateWindow = d
			i++

		default:
			return nil, fmt.Errorf("cliparser: unrecognized flag %q", arg)
		}
	}

	return cfg, nil
}

func parseDirectSendArgs(args []string, warnFn func(string)) (*DirectSendConfig, error) {
	cfg := &DirectSendConfig{}
	var rawFiles []string

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "-a" || arg == "--addr" || arg == "--address" || arg == "-u" || arg == "--target":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("cliparser: %s requires a receiver address", arg)
			}
			cfg.Addr = args[i+1]
			i += 2

		case strings.HasPrefix(arg, "-a=") || strings.HasPrefix(arg, "--addr=") || strings.HasPrefix(arg, "--address=") ||
			strings.HasPrefix(arg, "-u=") || strings.HasPrefix(arg, "--target="):
			cfg.Addr = arg[strings.Index(arg, "=")+1:]
			i++

		case arg == "-c" || arg == "--code" || arg == "--codephrase":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("cliparser: %s requires a codephrase", arg)
			}
			cfg.Codephrase = args[i+1]
			i += 2

		case strings.HasPrefix(arg, "-c=") || strings.HasPrefix(arg, "--code=") || strings.HasPrefix(arg, "--codephrase="):
			cfg.Codephrase = arg[strings.Index(arg, "=")+1:]
			i++

		case arg == "-f" || arg == "--file" || arg == "--files":
			i++
			for i < len(args) && !strings.HasPrefix(args[i], "-") {
				rawFiles = append(rawFiles, args[i])
				i++
			}

		case !strings.HasPrefix(arg, "-"):
			rawFiles = append(rawFiles, arg)
			i++

		default:
			return nil, fmt.Errorf("cliparser: unrecognized flag %q", arg)
		}
	}

	if cfg.Addr == "" {
		return nil, errors.New("cliparser: direct send requires a receiver address (-a/--addr)")
	}
	if cfg.Codephrase == "" {
		return nil, errors.New("cliparser: direct send requires a codephrase (-c/--code)")
	}

	if len(rawFiles) == 0 {
		return nil, errors.New("cliparser: no files specified to send (use -f or positional files)")
	}

	validFiles := filterValidFiles(rawFiles, warnFn)
	if len(validFiles) == 0 {
		return nil, errors.New("cliparser: no valid files to send")
	}

	cfg.Files = validFiles
	return cfg, nil
}

func parseDirectRecArgs(args []string) (*DirectRecConfig, error) {
	cfg := &DirectRecConfig{
		Dir:  ".",
		Port: 0,
	}

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "-d" || arg == "--dir" || arg == "--output":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("cliparser: %s requires a directory path", arg)
			}
			cfg.Dir = args[i+1]
			i += 2

		case strings.HasPrefix(arg, "-d=") || strings.HasPrefix(arg, "--dir=") || strings.HasPrefix(arg, "--output="):
			cfg.Dir = arg[strings.Index(arg, "=")+1:]
			i++

		case arg == "-p" || arg == "--port":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("cliparser: %s requires a port number", arg)
			}
			port, err := strconv.Atoi(args[i+1])
			if err != nil || port < 0 || port > 65535 {
				return nil, fmt.Errorf("cliparser: invalid port %q", args[i+1])
			}
			cfg.Port = port
			i += 2

		case strings.HasPrefix(arg, "-p=") || strings.HasPrefix(arg, "--port="):
			val := arg[strings.Index(arg, "=")+1:]
			port, err := strconv.Atoi(val)
			if err != nil || port < 0 || port > 65535 {
				return nil, fmt.Errorf("cliparser: invalid port %q", val)
			}
			cfg.Port = port
			i++

		case arg == "-c" || arg == "--code" || arg == "--codephrase":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("cliparser: %s requires a codephrase string", arg)
			}
			cfg.Codephrase = args[i+1]
			i += 2

		case strings.HasPrefix(arg, "-c=") || strings.HasPrefix(arg, "--code=") || strings.HasPrefix(arg, "--codephrase="):
			cfg.Codephrase = arg[strings.Index(arg, "=")+1:]
			i++

		case arg == "--no-upnp":
			cfg.NoUPnP = true
			i++

		default:
			return nil, fmt.Errorf("cliparser: unrecognized flag %q", arg)
		}
	}

	return cfg, nil
}

