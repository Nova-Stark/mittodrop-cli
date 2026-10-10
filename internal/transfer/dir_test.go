package transfer_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"mittodrop/internal/transfer"
)

func TestTransfer_DirectoryRecursiveStream(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// Build a nested directory tree
	// srcDir/
	//   file1.txt
	//   empty_sub/
	//   nested/
	//     deep/
	//       doc.pdf
	//     photo.png
	file1 := filepath.Join(srcDir, "file1.txt")
	_ = os.WriteFile(file1, []byte("root file content"), 0644)

	emptySub := filepath.Join(srcDir, "empty_sub")
	_ = os.MkdirAll(emptySub, 0755)

	deepDir := filepath.Join(srcDir, "nested", "deep")
	_ = os.MkdirAll(deepDir, 0755)

	docFile := filepath.Join(deepDir, "doc.pdf")
	_ = os.WriteFile(docFile, []byte("deep nested doc content"), 0644)

	photoFile := filepath.Join(srcDir, "nested", "photo.png")
	_ = os.WriteFile(photoFile, []byte("nested photo content"), 0644)

	ctx := context.Background()

	// 1. Initialize DirReader
	dr, err := transfer.NewDirReader(ctx, srcDir)
	if err != nil {
		t.Fatalf("NewDirReader: %v", err)
	}
	defer dr.Close()

	meta := dr.Metadata()
	if !meta.IsDir {
		t.Fatal("expected meta.IsDir to be true")
	}
	if meta.Size <= 0 {
		t.Fatalf("expected positive meta.Size, got %d", meta.Size)
	}

	// 2. Initialize Writer for directory extraction
	writer, err := transfer.NewWriter(dstDir, meta)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	defer writer.Close()

	// 3. Pump chunks from DirReader to Writer
	for {
		chunk, err := dr.NextChunk()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("NextChunk: %v", err)
		}

		if err := writer.WriteChunk(chunk); err != nil {
			t.Fatalf("WriteChunk: %v", err)
		}
		dr.ReleaseChunk(chunk)
	}

	// 4. Finish and verify checksum
	if err := writer.Finish(dr.Checksum()); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	// 5. Verify all extracted files and directories in dstDir
	baseName := filepath.Base(srcDir)
	extractedRoot := filepath.Join(dstDir, baseName)

	checkFiles := []struct {
		relPath  string
		expected string
		isDir    bool
	}{
		{relPath: "file1.txt", expected: "root file content", isDir: false},
		{relPath: "empty_sub", isDir: true},
		{relPath: filepath.Join("nested", "deep", "doc.pdf"), expected: "deep nested doc content", isDir: false},
		{relPath: filepath.Join("nested", "photo.png"), expected: "nested photo content", isDir: false},
	}

	for _, cf := range checkFiles {
		fullPath := filepath.Join(extractedRoot, cf.relPath)
		stat, err := os.Stat(fullPath)
		if err != nil {
			t.Errorf("missing extracted path %s: %v", cf.relPath, err)
			continue
		}
		if cf.isDir && !stat.IsDir() {
			t.Errorf("expected %s to be directory", cf.relPath)
		}
		if !cf.isDir {
			data, err := os.ReadFile(fullPath)
			if err != nil {
				t.Errorf("read file %s: %v", cf.relPath, err)
				continue
			}
			if string(data) != cf.expected {
				t.Errorf("%s content mismatch: got %q, want %q", cf.relPath, string(data), cf.expected)
			}
		}
	}
}

func TestTransfer_EmptyDirectoryStream(t *testing.T) {
	srcDir := t.TempDir()
	emptyFolder := filepath.Join(srcDir, "empty_folder")
	if err := os.MkdirAll(emptyFolder, 0755); err != nil {
		t.Fatalf("mkdir empty: %v", err)
	}

	dstDir := t.TempDir()
	ctx := context.Background()

	dr, err := transfer.NewDirReader(ctx, emptyFolder)
	if err != nil {
		t.Fatalf("NewDirReader empty dir: %v", err)
	}
	defer dr.Close()

	meta := dr.Metadata()
	if !meta.IsDir {
		t.Errorf("expected meta.IsDir = true")
	}

	writer, err := transfer.NewWriter(dstDir, meta)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	defer writer.Close()

	for {
		chunk, err := dr.NextChunk()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("NextChunk: %v", err)
		}
		if err := writer.WriteChunk(chunk); err != nil {
			t.Fatalf("WriteChunk: %v", err)
		}
		dr.ReleaseChunk(chunk)
	}

	if err := writer.Finish(dr.Checksum()); err != nil {
		t.Fatalf("Finish empty dir: %v", err)
	}

	extractedFolder := filepath.Join(dstDir, "empty_folder")
	stat, err := os.Stat(extractedFolder)
	if err != nil {
		t.Fatalf("empty directory not extracted: %v", err)
	}
	if !stat.IsDir() {
		t.Errorf("expected %s to be directory", extractedFolder)
	}
}

func TestTransfer_DirectoryNonExistentPath(t *testing.T) {
	ctx := context.Background()
	_, err := transfer.NewDirReader(ctx, "non_existent_folder_xyz_999")
	if err == nil {
		t.Fatal("expected error creating DirReader for nonexistent path, got nil")
	}
}

func TestTransfer_DirectoryFilePathRejected(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "sample.txt")
	_ = os.WriteFile(tmpFile, []byte("regular file"), 0644)

	ctx := context.Background()
	_, err := transfer.NewDirReader(ctx, tmpFile)
	if err == nil {
		t.Fatal("expected error when passing regular file to NewDirReader, got nil")
	}
}

func TestTransfer_DirectoryAbortMidStream(t *testing.T) {
	srcDir := t.TempDir()
	folder := filepath.Join(srcDir, "abort_dir")
	_ = os.MkdirAll(folder, 0755)
	for i := 0; i < 5; i++ {
		fPath := filepath.Join(folder, fmt.Sprintf("data_%d.bin", i))
		_ = os.WriteFile(fPath, make([]byte, 100*1024), 0644)
	}

	dstDir := t.TempDir()
	ctx := context.Background()

	dr, err := transfer.NewDirReader(ctx, folder)
	if err != nil {
		t.Fatalf("NewDirReader: %v", err)
	}
	defer dr.Close()

	meta := dr.Metadata()
	writer, err := transfer.NewWriter(dstDir, meta)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}

	// Write only the first chunk
	chunk, err := dr.NextChunk()
	if err != nil {
		t.Fatalf("NextChunk: %v", err)
	}
	if err := writer.WriteChunk(chunk); err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}
	dr.ReleaseChunk(chunk)

	// Abort midstream
	if err := writer.Abort(); err != nil {
		t.Fatalf("Abort: %v", err)
	}

	// Double abort/close should be idempotent
	if err := writer.Close(); err != nil {
		t.Fatalf("Close after Abort: %v", err)
	}
}

func TestTransfer_DirectoryTamperedChecksumRejected(t *testing.T) {
	srcDir := t.TempDir()
	folder := filepath.Join(srcDir, "tamper_dir")
	_ = os.MkdirAll(folder, 0755)
	_ = os.WriteFile(filepath.Join(folder, "file.txt"), []byte("legit content"), 0644)

	dstDir := t.TempDir()
	ctx := context.Background()

	dr, err := transfer.NewDirReader(ctx, folder)
	if err != nil {
		t.Fatalf("NewDirReader: %v", err)
	}
	defer dr.Close()

	meta := dr.Metadata()
	writer, err := transfer.NewWriter(dstDir, meta)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	defer writer.Close()

	for {
		chunk, err := dr.NextChunk()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("NextChunk: %v", err)
		}
		if err := writer.WriteChunk(chunk); err != nil {
			t.Fatalf("WriteChunk: %v", err)
		}
		dr.ReleaseChunk(chunk)
	}

	// Pass incorrect checksum
	fakeChecksum := [32]byte{0xDE, 0xAD, 0xBE, 0xEF}
	err = writer.Finish(fakeChecksum)
	if err == nil {
		t.Fatal("expected Finish to fail with mismatched checksum, got nil")
	}
}
