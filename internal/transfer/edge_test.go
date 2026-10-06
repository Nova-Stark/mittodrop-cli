package transfer

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

func writeTemp(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func randBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

func readAll(t *testing.T, r *Reader) []*Chunk {
	t.Helper()
	var out []*Chunk
	for {
		c, err := r.NextChunk()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatalf("NextChunk: %v", err)
		}
		out = append(out, c)
	}
}

func rawChunk(idx uint64, off int64, data []byte) *Chunk {
	return &Chunk{Index: idx, Offset: off, RawSize: uint32(len(data)), CompSize: uint32(len(data)), Data: data}
}

// ---------------- Reader ----------------

func TestEdgeReaderMissingFile(t *testing.T) {
	if _, err := NewReader(context.Background(), filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("expected error")
	}
}

func TestEdgeReaderDirectory(t *testing.T) {
	if _, err := NewReader(context.Background(), t.TempDir()); err == nil {
		t.Fatal("expected error for directory")
	}
}

func TestEdgeReaderEmptyFileChunkCountConsistent(t *testing.T) {
	p := writeTemp(t, "empty", nil)
	r, err := NewReader(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	chunks := readAll(t, r)
	meta := r.Metadata()
	if meta.Size != 0 {
		t.Errorf("size %d", meta.Size)
	}
	if uint64(len(chunks)) != meta.TotalChunks {
		t.Errorf("metadata TotalChunks=%d but reader delivered %d chunks", meta.TotalChunks, len(chunks))
	}
}

func TestEdgeReaderChunkBoundaries(t *testing.T) {
	const cs = 1024
	cases := []struct {
		name string
		size int
		want int
	}{
		{"one byte", 1, 1},
		{"cs-1", cs - 1, 1},
		{"exactly cs", cs, 1},
		{"cs+1", cs + 1, 2},
		{"exactly 3cs", 3 * cs, 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := randBytes(c.size)
			p := writeTemp(t, "f", data)
			r, err := NewReader(context.Background(), p, ReaderConfig{ChunkSize: cs})
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			chunks := readAll(t, r)
			if len(chunks) != c.want {
				t.Fatalf("got %d chunks want %d", len(chunks), c.want)
			}
			if r.Metadata().TotalChunks != uint64(c.want) {
				t.Errorf("TotalChunks=%d want %d", r.Metadata().TotalChunks, c.want)
			}
			var total int64
			for i, ch := range chunks {
				if ch.Index != uint64(i) || ch.Offset != total {
					t.Errorf("chunk %d: index=%d offset=%d", i, ch.Index, ch.Offset)
				}
				total += int64(ch.RawSize)
			}
			if total != int64(c.size) {
				t.Errorf("total raw %d want %d", total, c.size)
			}
		})
	}
}

func TestEdgeReaderNextChunkAfterEOFRepeats(t *testing.T) {
	p := writeTemp(t, "f", []byte("hi"))
	r, _ := NewReader(context.Background(), p)
	defer r.Close()
	readAll(t, r)
	for i := 0; i < 3; i++ {
		if _, err := r.NextChunk(); err != io.EOF {
			t.Fatalf("call %d: want EOF got %v", i, err)
		}
	}
}

func TestEdgeReaderCloseTwiceAndAfter(t *testing.T) {
	p := writeTemp(t, "f", randBytes(4096))
	r, _ := NewReader(context.Background(), p)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if _, err := r.NextChunk(); err == nil {
		t.Fatal("NextChunk after Close should error")
	}
}

func TestEdgeReaderContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p := writeTemp(t, "f", randBytes(64*1024))
	r, err := NewReader(ctx, p, ReaderConfig{ChunkSize: 1024, QueueSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	cancel()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("NextChunk kept succeeding after cancel")
		default:
		}
		if _, err := r.NextChunk(); err != nil {
			return
		}
	}
}

func TestEdgeReaderReleaseChunkNil(t *testing.T) {
	p := writeTemp(t, "f", []byte("x"))
	r, _ := NewReader(context.Background(), p)
	defer r.Close()
	r.ReleaseChunk(nil)
	r.ReleaseChunk(&Chunk{})
}

func TestEdgeReaderMetadataAndTransferInfo(t *testing.T) {
	data := []byte("hello world")
	p := writeTemp(t, "name.txt", data)
	r, _ := NewReader(context.Background(), p)
	defer r.Close()
	r.SetTransferInfo("bob", "tok", "batch", 2, 5)
	m := r.Metadata()
	if m.Name != "name.txt" || m.Size != int64(len(data)) {
		t.Errorf("meta %+v", m)
	}
	if m.Checksum != sha256.Sum256(data) {
		t.Error("checksum mismatch")
	}
	if m.SenderName != "bob" || m.Token != "tok" || m.BatchID != "batch" || m.BatchIndex != 2 || m.BatchTotal != 5 {
		t.Errorf("transfer info %+v", m)
	}
}

func TestEdgeReaderCompressibleUsesCompression(t *testing.T) {
	p := writeTemp(t, "z", bytes.Repeat([]byte("a"), 100000))
	r, _ := NewReader(context.Background(), p)
	defer r.Close()
	ch := readAll(t, r)
	if len(ch) != 1 || !ch[0].IsCompressed || ch[0].CompSize >= ch[0].RawSize {
		t.Errorf("expected compressed chunk: %+v", ch[0])
	}
}

// ---------------- Writer ----------------

func newMeta(name string, data []byte) FileMetadata {
	return FileMetadata{Name: name, Size: int64(len(data)), Checksum: sha256.Sum256(data)}
}

func TestEdgeWriterRoundTripViaReader(t *testing.T) {
	data := randBytes(10*1024 + 7)
	p := writeTemp(t, "src.bin", data)
	r, _ := NewReader(context.Background(), p, ReaderConfig{ChunkSize: 1024})
	defer r.Close()
	meta := r.Metadata()
	dir := t.TempDir()
	w, err := NewWriter(dir, meta)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range readAll(t, r) {
		if err := w.WriteChunk(c); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Finish(meta.Checksum); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(w.TargetPath())
	if !bytes.Equal(got, data) {
		t.Error("content mismatch")
	}
}

func TestEdgeWriterEmptyName(t *testing.T) {
	if _, err := NewWriter(t.TempDir(), FileMetadata{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestEdgeWriterTraversalNames(t *testing.T) {
	for _, name := range []string{"..", ".", "../evil.txt", "a/../../evil.txt", "..\\evil.txt", "/abs/evil.txt", "sub/dir/file.txt"} {
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			dir := filepath.Join(parent, "save")
			data := []byte("x")
			w, err := NewWriter(dir, newMeta(name, data))
			if err != nil {
				return // rejecting is fine
			}
			defer w.Close()
			abs, _ := filepath.Abs(dir)
			if filepath.Dir(w.TargetPath()) != abs {
				t.Errorf("target escaped save dir: %s", w.TargetPath())
			}
		})
	}
}

func TestEdgeWriterSaveDirCreated(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b", "c")
	w, err := NewWriter(dir, newMeta("f", []byte("x")))
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
}

func TestEdgeWriterSaveDirIsFile(t *testing.T) {
	f := writeTemp(t, "afile", []byte("x"))
	if _, err := NewWriter(f, newMeta("f", []byte("x"))); err == nil {
		t.Fatal("expected error when save dir is a file")
	}
}

func TestEdgeWriterHugeSizeFailsCleanly(t *testing.T) {
	dir := t.TempDir()
	meta := FileMetadata{Name: "big", Size: 1 << 50}
	w, err := NewWriter(dir, meta)
	if err == nil {
		w.Close()
		t.Skip("filesystem accepted sparse 1PB allocation")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("staging file leaked: %v", entries)
	}
}

func TestEdgeWriterOutOfOrderChunks(t *testing.T) {
	data := []byte("AAAABBBBCCCC")
	w, _ := NewWriter(t.TempDir(), newMeta("f", data))
	defer w.Close()
	_ = w.WriteChunk(rawChunk(2, 8, []byte("CCCC")))
	_ = w.WriteChunk(rawChunk(0, 0, []byte("AAAA")))
	_ = w.WriteChunk(rawChunk(1, 4, []byte("BBBB")))
	if err := w.Finish(sha256.Sum256(data)); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(w.TargetPath())
	if !bytes.Equal(got, data) {
		t.Errorf("got %q", got)
	}
}

func TestEdgeWriterDuplicateChunkDoesNotCorrupt(t *testing.T) {
	data := []byte("AAAABBBB")
	w, _ := NewWriter(t.TempDir(), newMeta("f", data))
	defer w.Close()
	_ = w.WriteChunk(rawChunk(0, 0, []byte("AAAA")))
	_ = w.WriteChunk(rawChunk(0, 0, []byte("AAAA")))
	_ = w.WriteChunk(rawChunk(1, 4, []byte("BBBB")))
	if err := w.Finish(sha256.Sum256(data)); err != nil {
		t.Fatalf("duplicate chunk broke finish: %v", err)
	}
	got, _ := os.ReadFile(w.TargetPath())
	if !bytes.Equal(got, data) {
		t.Errorf("got %q want %q", got, data)
	}
}

func TestEdgeWriterChunkBeyondDeclaredSizeRejected(t *testing.T) {
	data := []byte("AAAA")
	dir := t.TempDir()
	w, _ := NewWriter(dir, newMeta("f", data))
	defer w.Close()
	if err := w.WriteChunk(rawChunk(1, 1<<20, []byte("ZZZZ"))); err == nil {
		t.Error("chunk written far beyond declared file size was accepted")
	}
}

func TestEdgeWriterNegativeOffset(t *testing.T) {
	w, _ := NewWriter(t.TempDir(), newMeta("f", []byte("AAAA")))
	defer w.Close()
	if err := w.WriteChunk(rawChunk(0, -1, []byte("AAAA"))); err == nil {
		t.Error("negative offset accepted")
	}
}

func TestEdgeWriterRawSizeMismatch(t *testing.T) {
	w, _ := NewWriter(t.TempDir(), newMeta("f", []byte("AAAA")))
	defer w.Close()
	c := rawChunk(0, 0, []byte("AAAA"))
	c.RawSize = 9
	if err := w.WriteChunk(c); err == nil {
		t.Error("expected size mismatch error")
	}
}

func TestEdgeWriterCorruptCompressedChunk(t *testing.T) {
	w, _ := NewWriter(t.TempDir(), newMeta("f", []byte("AAAA")))
	defer w.Close()
	c := &Chunk{Index: 0, RawSize: 4, IsCompressed: true, Data: []byte("not zstd data")}
	if err := w.WriteChunk(c); err == nil {
		t.Error("expected decompress error")
	}
}

func TestEdgeWriterCompressedSizeLie(t *testing.T) {
	enc, _ := zstd.NewWriter(nil)
	comp := enc.EncodeAll([]byte("AAAAAAAA"), nil)
	w, _ := NewWriter(t.TempDir(), newMeta("f", []byte("AAAAAAAA")))
	defer w.Close()
	c := &Chunk{Index: 0, RawSize: 3, IsCompressed: true, Data: comp}
	if err := w.WriteChunk(c); err == nil {
		t.Error("decompressed size != RawSize should error")
	}
}

func TestEdgeWriterChecksumMismatchCleansStaging(t *testing.T) {
	dir := t.TempDir()
	data := []byte("AAAA")
	w, _ := NewWriter(dir, newMeta("f", data))
	_ = w.WriteChunk(rawChunk(0, 0, data))
	var bad [32]byte
	bad[0] = 1
	if err := w.Finish(bad); err == nil {
		t.Fatal("expected integrity error")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("leftover files: %v", entries)
	}
}

func TestEdgeWriterMissingChunksDetected(t *testing.T) {
	data := []byte("AAAABBBB")
	w, _ := NewWriter(t.TempDir(), newMeta("f", data))
	defer w.Close()
	_ = w.WriteChunk(rawChunk(0, 0, []byte("AAAA")))
	if err := w.Finish(sha256.Sum256(data)); err == nil {
		t.Error("Finish succeeded although second chunk never written")
	}
}

func TestEdgeWriterFinishTwice(t *testing.T) {
	data := []byte("AAAA")
	w, _ := NewWriter(t.TempDir(), newMeta("f", data))
	_ = w.WriteChunk(rawChunk(0, 0, data))
	if err := w.Finish(sha256.Sum256(data)); err != nil {
		t.Fatal(err)
	}
	if err := w.Finish(sha256.Sum256(data)); err == nil {
		t.Error("second Finish should error")
	}
}

func TestEdgeWriterWriteAfterFinish(t *testing.T) {
	data := []byte("AAAA")
	w, _ := NewWriter(t.TempDir(), newMeta("f", data))
	_ = w.WriteChunk(rawChunk(0, 0, data))
	_ = w.Finish(sha256.Sum256(data))
	if err := w.WriteChunk(rawChunk(0, 0, data)); err == nil {
		t.Error("WriteChunk after Finish should error")
	}
}

func TestEdgeWriterCloseAfterFinishKeepsFile(t *testing.T) {
	data := []byte("AAAA")
	w, _ := NewWriter(t.TempDir(), newMeta("f", data))
	_ = w.WriteChunk(rawChunk(0, 0, data))
	_ = w.Finish(sha256.Sum256(data))
	_ = w.Close()
	if _, err := os.Stat(w.TargetPath()); err != nil {
		t.Errorf("final file removed by Close: %v", err)
	}
}

func TestEdgeWriterCloseWithoutFinishRemovesStaging(t *testing.T) {
	dir := t.TempDir()
	w, _ := NewWriter(dir, newMeta("f", []byte("AAAA")))
	_ = w.Close()
	_ = w.Abort()
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("leftover: %v", entries)
	}
}

func TestEdgeWriterEmptyFile(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWriter(dir, FileMetadata{Name: "empty", Size: 0, Checksum: sha256.Sum256(nil)})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Finish(sha256.Sum256(nil)); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(w.TargetPath())
	if err != nil || st.Size() != 0 {
		t.Errorf("stat: %v %v", st, err)
	}
}

func TestEdgeWriterOverwrite(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "f"), []byte("old"), 0644)
	data := []byte("newdata")
	w, _ := NewWriter(dir, newMeta("f", data), WithOverwrite(true))
	_ = w.WriteChunk(rawChunk(0, 0, data))
	if err := w.Finish(sha256.Sum256(data)); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "f"))
	if !bytes.Equal(got, data) {
		t.Errorf("got %q", got)
	}
}

func TestEdgeWriterDisambiguationSequence(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 3; i++ {
		data := []byte(strings.Repeat("x", i+1))
		w, _ := NewWriter(dir, newMeta("f.txt", data))
		_ = w.WriteChunk(rawChunk(0, 0, data))
		if err := w.Finish(sha256.Sum256(data)); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range []string{"f.txt", "f (1).txt", "f (2).txt"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			t.Errorf("missing %s", n)
		}
	}
}

func TestEdgeWriterNoExtensionDisambiguation(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 2; i++ {
		data := []byte(strings.Repeat("y", i+1))
		w, _ := NewWriter(dir, newMeta("README", data))
		_ = w.WriteChunk(rawChunk(0, 0, data))
		_ = w.Finish(sha256.Sum256(data))
	}
	if _, err := os.Stat(filepath.Join(dir, "README (1)")); err != nil {
		t.Error("README (1) missing")
	}
}

func TestEdgeWriterConcurrentSameName(t *testing.T) {
	dir := t.TempDir()
	a, _ := NewWriter(dir, newMeta("f", []byte("A")))
	b, _ := NewWriter(dir, newMeta("f", []byte("B")))
	if a.StagingPath() == b.StagingPath() {
		t.Fatal("staging paths collide")
	}
	a.Close()
	b.Close()
}

func TestEdgeWriterWindowsReservedName(t *testing.T) {
	dir := t.TempDir()
	data := []byte("x")
	w, err := NewWriter(dir, newMeta("CON", data))
	if err != nil {
		return
	}
	_ = w.WriteChunk(rawChunk(0, 0, data))
	_ = w.Finish(sha256.Sum256(data))
	w.Close()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".mittodrop-") {
			t.Errorf("staging file leaked: %s", e.Name())
		}
	}
}

// ---------------- Checksum ----------------

func TestEdgeCalculateChecksumEmptyAndSeek(t *testing.T) {
	p := writeTemp(t, "e", nil)
	f, _ := os.Open(p)
	defer f.Close()
	got, err := CalculateChecksum(f)
	if err != nil || got != sha256.Sum256(nil) {
		t.Errorf("empty: %v %x", err, got)
	}
	p2 := writeTemp(t, "d", []byte("abc"))
	f2, _ := os.Open(p2)
	defer f2.Close()
	_, _ = f2.Seek(2, io.SeekStart)
	got, _ = CalculateChecksum(f2)
	if got != sha256.Sum256([]byte("abc")) {
		t.Error("checksum should hash from start regardless of current offset")
	}
}

func TestEdgeCalculateChecksumClosedFile(t *testing.T) {
	p := writeTemp(t, "c", []byte("abc"))
	f, _ := os.Open(p)
	f.Close()
	if _, err := CalculateChecksum(f); err == nil {
		t.Error("expected error on closed file")
	}
}
