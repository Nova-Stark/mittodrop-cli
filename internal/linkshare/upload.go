package linkshare

import (
	"compress/gzip"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/klauspost/compress/zstd"
	"mittodrop/internal/transfer"
)

// UploadResult contains summary of saved file.
type UploadResult struct {
	Filename string `json:"filename"`
	Bytes    int64  `json:"bytes"`
}

func (r *Receiver) handleUpload(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 1. Verify token if configured
	senderToken := req.Header.Get("X-LinkShare-Token")
	if senderToken == "" {
		senderToken = req.URL.Query().Get("token")
	}
	if senderToken == "" {
		authHeader := req.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			senderToken = strings.TrimPrefix(authHeader, "Bearer ")
		}
	}

	authed := (r.token != "" && senderToken == r.token)
	if r.token != "" && !authed {
		http.Error(w, "unauthorized: invalid or missing linkshare token", http.StatusUnauthorized)
		return
	}

	clientAddr := req.RemoteAddr
	senderName := req.Header.Get("X-Sender-Name")
	if senderName == "" {
		senderName = "Client"
	}
	if r.cfg.OnConnect != nil {
		r.cfg.OnConnect(senderName, clientAddr, authed)
	}

	contentType := req.Header.Get("Content-Type")

	var results []UploadResult
	var err error

	if strings.HasPrefix(contentType, "multipart/form-data") {
		results, err = r.saveMultipartStream(req)
	} else {
		// Raw octet stream (browser fetch streaming or CLI app)
		results, err = r.saveRawStream(req)
	}

	if err != nil {
		http.Error(w, fmt.Sprintf("upload failed: %v", err), http.StatusBadRequest)
		return
	}

	// Terminal success log (pure ASCII)
	for _, res := range results {
		savePath := filepath.Join(r.cfg.SaveDir, res.Filename)
		if r.cfg.OnComplete != nil {
			r.cfg.OnComplete(senderName, res.Filename, savePath)
		} else {
			fmt.Printf("[COMPLETE] [%s] %s (%s) -> %s\n", senderName, res.Filename, formatByteSize(res.Bytes), savePath)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(results)
}

func formatByteSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func (r *Receiver) saveStreamToWriter(body io.Reader, filename string, declaredSize int64, encoding string, checksumStr string, isEncrypted bool) (UploadResult, error) {
	cleanName := sanitizeFilename(filename)

	var reader io.Reader = body

	// 1. Decrypt stream if encrypted
	if isEncrypted {
		var zeroKey [32]byte
		if r.cfg.SessionKey == zeroKey {
			return UploadResult{}, fmt.Errorf("encrypted upload received, but receiver has no session key configured")
		}
		decReader, err := NewDecryptReader(body, r.cfg.SessionKey)
		if err != nil {
			return UploadResult{}, fmt.Errorf("init decrypt reader: %w", err)
		}
		reader = decReader
	} else {
		var zeroKey [32]byte
		if r.cfg.SessionKey != zeroKey {
			return UploadResult{}, fmt.Errorf("unencrypted upload rejected: receiver requires encryption")
		}
	}

	// 2. Decompress stream if compressed
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "gzip":
		gz, err := gzip.NewReader(reader)
		if err != nil {
			return UploadResult{}, fmt.Errorf("open gzip reader: %w", err)
		}
		defer gz.Close()
		reader = gz
	case "zstd":
		dec, err := zstd.NewReader(reader)
		if err != nil {
			return UploadResult{}, fmt.Errorf("open zstd reader: %w", err)
		}
		defer dec.Close()
		reader = dec
	}

	var expectedChecksum [32]byte
	checksumStr = strings.TrimSpace(checksumStr)
	if checksumStr != "" {
		raw, err := hex.DecodeString(checksumStr)
		if err == nil && len(raw) == 32 {
			copy(expectedChecksum[:], raw)
		}
	}

	meta := transfer.FileMetadata{
		Name:     cleanName,
		Size:     declaredSize,
		Checksum: expectedChecksum,
	}

	writer, err := transfer.NewWriter(r.cfg.SaveDir, meta, transfer.WithOverwrite(r.cfg.Overwrite))
	if err != nil {
		return UploadResult{}, fmt.Errorf("init transfer writer: %w", err)
	}
	defer writer.Close()

	buf := make([]byte, transfer.DefaultChunkSize)
	var offset int64
	var chunkIdx uint64
	var totalWritten int64

	for {
		n, readErr := io.ReadFull(reader, buf)
		if n > 0 {
			chunk := &transfer.Chunk{
				Index:        chunkIdx,
				Offset:       offset,
				RawSize:      uint32(n),
				IsCompressed: false, // decompressed before staging
				Data:         buf[:n],
			}
			if err := writer.WriteChunk(chunk); err != nil {
				return UploadResult{}, fmt.Errorf("write chunk %d: %w", chunkIdx, err)
			}
			offset += int64(n)
			totalWritten += int64(n)
			chunkIdx++
		}
		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}
		if readErr != nil {
			return UploadResult{}, fmt.Errorf("read stream: %w", readErr)
		}
	}

	if err := writer.Finish(expectedChecksum); err != nil {
		return UploadResult{}, fmt.Errorf("verify and commit: %w", err)
	}

	return UploadResult{
		Filename: filepath.Base(writer.TargetPath()),
		Bytes:    totalWritten,
	}, nil
}

func (r *Receiver) saveMultipartStream(req *http.Request) ([]UploadResult, error) {
	reader, err := req.MultipartReader()
	if err != nil {
		return nil, fmt.Errorf("read multipart: %w", err)
	}

	var results []UploadResult

	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read part: %w", err)
		}

		filename := part.FileName()
		if filename == "" {
			part.Close()
			continue
		}

		encoding := part.Header.Get("Content-Encoding")
		checksumStr := part.Header.Get("X-File-Checksum")

		res, err := r.saveStreamToWriter(part, filename, 0, encoding, checksumStr, false)
		part.Close()
		if err != nil {
			return nil, fmt.Errorf("save part %q: %w", filename, err)
		}

		results = append(results, res)
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("no file parts found in multipart request")
	}

	return results, nil
}

func (r *Receiver) saveRawStream(req *http.Request) ([]UploadResult, error) {
	filename := req.Header.Get("X-File-Name")
	if filename == "" {
		filename = "uploaded.bin"
	} else if unescaped, err := url.PathUnescape(filename); err == nil && unescaped != "" {
		filename = unescaped
	}

	encoding := req.Header.Get("Content-Encoding")
	if encoding == "" {
		encoding = req.Header.Get("X-Content-Encoding")
	}
	checksumStr := req.Header.Get("X-File-Checksum")
	isEncrypted := strings.EqualFold(req.Header.Get("X-Mittodrop-Encrypted"), "true")

	var declaredSize int64
	if szStr := req.Header.Get("X-File-Size"); szStr != "" {
		declaredSize, _ = strconv.ParseInt(szStr, 10, 64)
	} else if req.ContentLength > 0 && encoding == "" && !isEncrypted {
		declaredSize = req.ContentLength
	}

	res, err := r.saveStreamToWriter(req.Body, filename, declaredSize, encoding, checksumStr, isEncrypted)
	if err != nil {
		return nil, fmt.Errorf("stream write %q: %w", filename, err)
	}

	return []UploadResult{res}, nil
}

func sanitizeFilename(name string) string {
	base := filepath.Base(name)
	base = strings.ReplaceAll(base, "..", "")
	base = strings.TrimSpace(base)
	if base == "" || base == "." || base == "/" || base == "\\" {
		return "unnamed_file"
	}
	return base
}
