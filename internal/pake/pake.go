package pake

import (
	"fmt"

	spake "github.com/schollz/pake/v3"
)

// DefaultCurve uses SIEC elliptic curve for SPAKE2.
const DefaultCurve = "siec"

// Initiator starts the PAKE handshake with a shared codephrase.
type Initiator struct {
	pake *spake.Pake
}

// Responder answers the PAKE handshake with the shared codephrase.
type Responder struct {
	pake *spake.Pake
}

// NewInitiator initializes role 0 of the SPAKE2 exchange.
// Returns initiator instance and the public message (Bytes) to send to responder.
func NewInitiator(codephrase string) (*Initiator, []byte, error) {
	if codephrase == "" {
		return nil, nil, fmt.Errorf("pake: empty codephrase")
	}
	p, err := spake.InitCurve([]byte(codephrase), 0, DefaultCurve)
	if err != nil {
		return nil, nil, fmt.Errorf("pake: init initiator: %w", err)
	}
	return &Initiator{pake: p}, p.Bytes(), nil
}

// NewResponder initializes role 1 of the SPAKE2 exchange, ingests initiator's bytes,
// and produces responder's public message (Bytes) to send back.
func NewResponder(codephrase string, initiatorBytes []byte) (*Responder, []byte, error) {
	if codephrase == "" {
		return nil, nil, fmt.Errorf("pake: empty codephrase")
	}
	if len(initiatorBytes) == 0 {
		return nil, nil, fmt.Errorf("pake: empty initiator bytes")
	}
	p, err := spake.InitCurve([]byte(codephrase), 1, DefaultCurve)
	if err != nil {
		return nil, nil, fmt.Errorf("pake: init responder: %w", err)
	}
	if err := p.Update(initiatorBytes); err != nil {
		return nil, nil, fmt.Errorf("pake: responder update: %w", err)
	}
	return &Responder{pake: p}, p.Bytes(), nil
}

// Finish completes the handshake on the initiator side using responder's bytes
// and returns the derived 256-bit symmetric session key.
func (i *Initiator) Finish(responderBytes []byte) ([]byte, error) {
	if len(responderBytes) == 0 {
		return nil, fmt.Errorf("pake: empty responder bytes")
	}
	if err := i.pake.Update(responderBytes); err != nil {
		return nil, fmt.Errorf("pake: initiator update: %w", err)
	}
	key, err := i.pake.SessionKey()
	if err != nil {
		return nil, fmt.Errorf("pake: derive initiator session key: %w", err)
	}
	return key, nil
}

// SessionKey returns the derived 256-bit symmetric session key on the responder side.
func (r *Responder) SessionKey() ([]byte, error) {
	key, err := r.pake.SessionKey()
	if err != nil {
		return nil, fmt.Errorf("pake: derive responder session key: %w", err)
	}
	return key, nil
}
