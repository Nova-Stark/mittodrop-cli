package callsign

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"crypto/rand"
	"math/big"
)

//go:embed names.json
var namesData []byte

var adjectives []string
var nouns []string

func loadNames() error {
	var data struct {
		Adjectives []string `json:"adjectives"`
		Nouns      []string `json:"nouns"`
	}
	if err := json.Unmarshal(namesData, &data); err != nil {
		return fmt.Errorf("callsign: parse names.json: %w", err)
	}
	adjectives = data.Adjectives
	nouns = data.Nouns
	return nil
}

func GenerateDeviceName() (string, error) {
	if len(adjectives) == 0 {
		if err := loadNames(); err != nil {
			return "", err
		}
	}
	adjIdx, err := rand.Int(rand.Reader, big.NewInt(int64(len(adjectives))))
	if err != nil {
		return "", err
	}
	nounIdx, err := rand.Int(rand.Reader, big.NewInt(int64(len(nouns))))
	if err != nil {
		return "", err
	}
	num, err := rand.Int(rand.Reader, big.NewInt(900))
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s%s%d", adjectives[adjIdx.Int64()], nouns[nounIdx.Int64()], num.Int64()+100), nil
}