package conn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"mittodrop/internal/manual"
	"mittodrop/internal/relay"
	"mittodrop/internal/utils"
)

// ProbeTimeout is the per-candidate connection timeout.
const ProbeTimeout = 500 * time.Millisecond

// ProbeCandidates tries dialing the sender's candidate endpoints in priority order:
// IPv6 direct -> Local LAN -> UPnP public port.
// Returns the first successfully authenticated direct connection, its derived session key, and its path type.
func ProbeCandidates(ctx context.Context, candidates []manual.Endpoint, codephrase string, local ...utils.PeerIdentity) (net.Conn, [32]byte, string, error) {
	if len(candidates) == 0 {
		return nil, [32]byte{}, "", errors.New("conn: no candidates to probe")
	}

	for _, ep := range candidates {
		probeCtx, cancel := context.WithTimeout(ctx, ProbeTimeout)
		conn, key, _, err := manual.Dial(probeCtx, ep.Address, codephrase, local...)
		cancel()

		if err == nil {
			pathType := "direct-" + ep.Type
			return conn, key, pathType, nil
		}
	}

	return nil, [32]byte{}, "", errors.New("conn: all candidate endpoints failed")
}

// SendCandidates serializes and sends candidates over a control stream.
func SendCandidates(w net.Conn, candidates []manual.Endpoint) error {
	data, err := json.Marshal(CandidatePayload{Candidates: candidates})
	if err != nil {
		return fmt.Errorf("conn: marshal candidates: %w", err)
	}
	return relay.WriteFrame(w, data)
}

// ReceiveCandidates receives and parses candidates from a control stream.
func ReceiveCandidates(r net.Conn) ([]manual.Endpoint, error) {
	data, err := relay.ReadFrame(r)
	if err != nil {
		return nil, fmt.Errorf("conn: read candidates: %w", err)
	}
	var payload CandidatePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("conn: unmarshal candidates: %w", err)
	}
	return payload.Candidates, nil
}
