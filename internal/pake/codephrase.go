package pake

import (
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
)

//go:embed words.txt
var wordsRaw string

var words []string

func init() {
	lines := strings.Split(wordsRaw, "\n")
	for _, l := range lines {
		w := strings.TrimSpace(l)
		if w != "" {
			words = append(words, w)
		}
	}
}

func GenerateCodephrase() (string, error) {
	if len(words) == 0 {
		return "", fmt.Errorf("pake: empty wordlist")
	}

	num, err := rand.Int(rand.Reader, big.NewInt(90))
	if err != nil {
		return "", fmt.Errorf("pake: rand int: %w", err)
	}
	prefix := num.Int64() + 10

	w1Idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(words))))
	if err != nil {
		return "", fmt.Errorf("pake: rand word1: %w", err)
	}
	w2Idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(words))))
	if err != nil {
		return "", fmt.Errorf("pake: rand word2: %w", err)
	}

	return fmt.Sprintf("%d-%s-%s", prefix, words[w1Idx.Int64()], words[w2Idx.Int64()]), nil
}

func RoomID(codephrase string) string {
	clean := strings.TrimSpace(strings.ToLower(codephrase))
	if clean == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(clean))
	return hex.EncodeToString(sum[:8])
}

