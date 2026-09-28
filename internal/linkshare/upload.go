package linkshare

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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

	contentType := req.Header.Get("Content-Type")

	var results []UploadResult
	var err error

	if strings.HasPrefix(contentType, "multipart/form-data") {
		results, err = r.saveMultipartStream(req)
	} else {
		// Default to raw stream (CLI / curl application/octet-stream)
		results, err = r.saveRawStream(req)
	}

	if err != nil {
		http.Error(w, fmt.Sprintf("upload failed: %v", err), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(results)
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

		cleanName := sanitizeFilename(filename)
		destPath := filepath.Join(r.cfg.SaveDir, cleanName)

		outFile, err := os.Create(destPath)
		if err != nil {
			part.Close()
			return nil, fmt.Errorf("create file %q: %w", cleanName, err)
		}

		written, err := io.Copy(outFile, part)
		outFile.Close()
		part.Close()
		if err != nil {
			return nil, fmt.Errorf("write file %q: %w", cleanName, err)
		}

		results = append(results, UploadResult{
			Filename: cleanName,
			Bytes:    written,
		})
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
	}

	cleanName := sanitizeFilename(filename)
	destPath := filepath.Join(r.cfg.SaveDir, cleanName)

	outFile, err := os.Create(destPath)
	if err != nil {
		return nil, fmt.Errorf("create file %q: %w", cleanName, err)
	}
	defer outFile.Close()

	written, err := io.Copy(outFile, req.Body)
	if err != nil {
		return nil, fmt.Errorf("stream write %q: %w", cleanName, err)
	}

	return []UploadResult{{
		Filename: cleanName,
		Bytes:    written,
	}}, nil
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
