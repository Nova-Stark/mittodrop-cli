package transport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"

	"mittodrop/internal/conn"
	"mittodrop/internal/transfer"
)

var edgeKey = [32]byte{1, 2, 3, 4, 5, 6, 7, 8, 9}

type edgeResult struct {
	meta *transfer.FileMetadata
	err  error
}

// startReceiver runs ReceiveFileStream on one end of a pipe and returns the peer end.
func startReceiver(t *testing.T, dir string, onAccept ...func(transfer.FileMetadata) bool) (*Framer, net.Conn, chan edgeResult) {
	t.Helper()
	a, b := net.Pipe()
	_ = b.SetDeadline(time.Now().Add(5 * time.Second))
	res := make(chan edgeResult, 1)
	go func() {
		m, err := ReceiveFileStream(context.Background(), a, edgeKey, dir, nil, onAccept...)
		a.Close()
		res <- edgeResult{m, err}
	}()
	f, err := NewFramer(b, edgeKey)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close() })
	return f, b, res
}

// drain reads and returns the frame types the receiver sends until the peer closes.
func drain(peer *Framer, c net.Conn) []MsgType {
	var types []MsgType
	for {
		mt, _, err := peer.ReadFrame()
		if err != nil {
			return types
		}
		types = append(types, mt)
	}
}

func waitResult(t *testing.T, res chan edgeResult) edgeResult {
	t.Helper()
	select {
	case r := <-res:
		return r
	case <-time.After(6 * time.Second):
		t.Fatal("receiver did not finish")
		return edgeResult{}
	}
}

func metaFor(name string, data []byte) transfer.FileMetadata {
	return transfer.FileMetadata{Name: name, Size: int64(len(data)), Checksum: sha256.Sum256(data)}
}

func sendMeta(t *testing.T, peer *Framer, m transfer.FileMetadata) {
	t.Helper()
	b, _ := json.Marshal(m)
	if err := peer.WriteFrame(MsgFileMeta, b); err != nil {
		t.Fatal(err)
	}
}

func awaitAck(t *testing.T, peer *Framer) {
	t.Helper()
	mt, p, err := peer.ReadFrame()
	if err != nil || mt != MsgFileAck {
		t.Fatalf("expected ack, got type=%d payload=%q err=%v", mt, p, err)
	}
}

func contains(types []MsgType, want MsgType) bool {
	for _, t := range types {
		if t == want {
			return true
		}
	}
	return false
}

// ---------------- Framer ----------------

func newFramerPair(t *testing.T) (*Framer, *Framer, net.Conn, net.Conn) {
	t.Helper()
	a, b := net.Pipe()
	_ = a.SetDeadline(time.Now().Add(5 * time.Second))
	_ = b.SetDeadline(time.Now().Add(5 * time.Second))
	fa, _ := NewFramer(a, edgeKey)
	fb, _ := NewFramer(b, edgeKey)
	t.Cleanup(func() { a.Close(); b.Close() })
	return fa, fb, a, b
}

func TestEdgeFramerRoundTripEmptyAndLarge(t *testing.T) {
	fa, fb, _, _ := newFramerPair(t)
	for _, size := range []int{0, 1, 1 << 20} {
		payload := bytes.Repeat([]byte{7}, size)
		go fa.WriteFrame(MsgChunk, payload)
		mt, got, err := fb.ReadFrame()
		if err != nil || mt != MsgChunk || !bytes.Equal(got, payload) {
			t.Fatalf("size %d: type=%d err=%v len=%d", size, mt, err, len(got))
		}
	}
}

func TestEdgeFramerZeroKey(t *testing.T) {
	a, _ := net.Pipe()
	defer a.Close()
	if _, err := NewFramer(a, [32]byte{}); err != nil {
		t.Errorf("zero key: %v", err)
	}
}

func TestEdgeFramerNonceUnique(t *testing.T) {
	f, _ := NewFramer(nil, edgeKey)
	seen := map[[GCMNonceSize]byte]bool{}
	for i := 0; i < 10000; i++ {
		n := f.nextNonce()
		if seen[n] {
			t.Fatalf("nonce repeated at %d", i)
		}
		seen[n] = true
	}
}

func TestEdgeFramerOversizedPayloadRejectedOnWrite(t *testing.T) {
	a, _ := net.Pipe()
	defer a.Close()
	f, _ := NewFramer(a, edgeKey)
	if err := f.WriteFrame(MsgChunk, make([]byte, MaxFramePayload+1)); err == nil {
		t.Error("expected error")
	}
}

func rawHeader(magic0, magic1 byte, mt MsgType, length uint32) []byte {
	h := make([]byte, FrameHeaderSize)
	h[0], h[1], h[2] = magic0, magic1, byte(mt)
	binary.BigEndian.PutUint32(h[3:7], length)
	return h
}

func TestEdgeFramerBadMagic(t *testing.T) {
	_, fb, a, _ := newFramerPair(t)
	go a.Write(rawHeader('X', 'Y', MsgChunk, 0))
	if _, _, err := fb.ReadFrame(); err == nil {
		t.Error("expected invalid magic error")
	}
}

func TestEdgeFramerOversizedLengthHeader(t *testing.T) {
	_, fb, a, _ := newFramerPair(t)
	go a.Write(rawHeader(FrameMagic0, FrameMagic1, MsgChunk, 0xFFFFFFFF))
	if _, _, err := fb.ReadFrame(); err == nil {
		t.Error("expected too-large error")
	}
}

func TestEdgeFramerZeroLengthCiphertext(t *testing.T) {
	_, fb, a, _ := newFramerPair(t)
	go a.Write(rawHeader(FrameMagic0, FrameMagic1, MsgChunk, 0))
	if _, _, err := fb.ReadFrame(); err == nil {
		t.Error("ciphertext shorter than GCM tag must fail auth")
	}
}

func TestEdgeFramerTruncatedHeaderAndBody(t *testing.T) {
	_, fb, a, _ := newFramerPair(t)
	go func() { a.Write([]byte{FrameMagic0, FrameMagic1}); a.Close() }()
	if _, _, err := fb.ReadFrame(); err == nil {
		t.Error("truncated header should error")
	}
	_, fb2, a2, _ := newFramerPair(t)
	go func() {
		a2.Write(rawHeader(FrameMagic0, FrameMagic1, MsgChunk, 100))
		a2.Write([]byte("short"))
		a2.Close()
	}()
	if _, _, err := fb2.ReadFrame(); err == nil {
		t.Error("truncated body should error")
	}
}

// capture grabs the raw bytes of one frame written by the sender.
func capture(t *testing.T, mt MsgType, payload []byte) []byte {
	t.Helper()
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	_ = b.SetDeadline(time.Now().Add(5 * time.Second))
	f, _ := NewFramer(a, edgeKey)
	go f.WriteFrame(mt, payload)
	buf := make([]byte, FrameHeaderSize+len(payload)+16)
	n := 0
	for n < len(buf) {
		m, err := b.Read(buf[n:])
		n += m
		if err != nil {
			break
		}
	}
	return buf[:n]
}

func feed(t *testing.T, raw []byte) (MsgType, []byte, error) {
	t.Helper()
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	_ = b.SetDeadline(time.Now().Add(3 * time.Second))
	f, _ := NewFramer(b, edgeKey)
	go func() { a.Write(raw); a.Close() }()
	return f.ReadFrame()
}

func TestEdgeFramerTamperedCiphertext(t *testing.T) {
	raw := capture(t, MsgChunk, []byte("hello world"))
	raw[len(raw)-1] ^= 0xFF
	if _, _, err := feed(t, raw); err == nil {
		t.Error("tampered ciphertext accepted")
	}
}

func TestEdgeFramerTamperedMessageType(t *testing.T) {
	raw := capture(t, MsgChunk, []byte("hello world"))
	raw[2] = byte(MsgFileDone)
	if _, _, err := feed(t, raw); err == nil {
		t.Error("header (type) is not authenticated")
	}
}

func TestEdgeFramerWrongKey(t *testing.T) {
	raw := capture(t, MsgChunk, []byte("x"))
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	_ = b.SetDeadline(time.Now().Add(3 * time.Second))
	other := edgeKey
	other[0] ^= 1
	f, _ := NewFramer(b, other)
	go func() { a.Write(raw); a.Close() }()
	if _, _, err := f.ReadFrame(); err == nil {
		t.Error("wrong key accepted")
	}
}

func TestEdgeFramerReplayedFrameRejected(t *testing.T) {
	raw := capture(t, MsgChunk, []byte("chunk-data"))
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	_ = b.SetDeadline(time.Now().Add(3 * time.Second))
	f, _ := NewFramer(b, edgeKey)
	go func() { a.Write(raw); a.Write(raw); a.Close() }()
	if _, _, err := f.ReadFrame(); err != nil {
		t.Fatalf("first frame: %v", err)
	}
	if _, _, err := f.ReadFrame(); err == nil {
		t.Error("replayed identical frame was accepted twice")
	}
}

// ---------------- Wire chunk encoding ----------------

func TestEdgeChunkWireRoundTrip(t *testing.T) {
	cases := []struct {
		idx  uint64
		off  int64
		raw  uint32
		comp bool
		data []byte
	}{
		{0, 0, 0, false, nil},
		{1<<64 - 1, 1<<63 - 1, 1<<32 - 1, true, []byte("x")},
		{5, 1024, 3, false, []byte("abc")},
	}
	for _, c := range cases {
		buf := EncodeChunkWire(c.idx, c.off, c.raw, c.comp, c.data)
		idx, off, raw, comp, data, err := DecodeChunkWire(buf)
		if err != nil || idx != c.idx || off != c.off || raw != c.raw || comp != c.comp || !bytes.Equal(data, c.data) {
			t.Errorf("round trip failed for %+v: %v %v %v %v %q %v", c, idx, off, raw, comp, data, err)
		}
	}
}

func TestEdgeChunkWireShortBuffers(t *testing.T) {
	for _, n := range []int{0, 1, ChunkWireHeaderSize - 1} {
		if _, _, _, _, _, err := DecodeChunkWire(make([]byte, n)); err == nil {
			t.Errorf("len %d accepted", n)
		}
	}
	if _, _, _, _, data, err := DecodeChunkWire(make([]byte, ChunkWireHeaderSize)); err != nil || len(data) != 0 {
		t.Errorf("header-only buffer: %v %d", err, len(data))
	}
}

func TestEdgeChunkWireCompressedFlagValues(t *testing.T) {
	buf := EncodeChunkWire(0, 0, 0, false, nil)
	buf[20] = 2
	_, _, _, comp, _, err := DecodeChunkWire(buf)
	if err != nil {
		t.Fatal(err)
	}
	if comp {
		t.Error("flag value 2 treated as compressed")
	}
}

// ---------------- ReceiveFileStream ----------------

func TestEdgeReceiverZeroChecksumRejected(t *testing.T) {
	dir := t.TempDir()
	peer, c, res := startReceiver(t, dir)
	sendMeta(t, peer, transfer.FileMetadata{Name: "f", Size: 4})
	types := drain(peer, c)
	r := waitResult(t, res)
	if r.err == nil {
		t.Error("zero checksum accepted")
	}
	if !contains(types, MsgAbort) {
		t.Errorf("sender not told about abort: %v", types)
	}
}

func TestEdgeReceiverFirstFrameNotMeta(t *testing.T) {
	peer, c, res := startReceiver(t, t.TempDir())
	_ = peer.WriteFrame(MsgChunk, []byte("x"))
	drain(peer, c)
	if r := waitResult(t, res); r.err == nil {
		t.Error("expected error")
	}
}

func TestEdgeReceiverAbortBeforeMeta(t *testing.T) {
	peer, _, res := startReceiver(t, t.TempDir())
	_ = peer.WriteFrame(MsgAbort, []byte("nope"))
	if r := waitResult(t, res); r.err == nil {
		t.Error("expected error")
	}
}

func TestEdgeReceiverInvalidMetaJSON(t *testing.T) {
	peer, c, res := startReceiver(t, t.TempDir())
	_ = peer.WriteFrame(MsgFileMeta, []byte("{not json"))
	types := drain(peer, c)
	if r := waitResult(t, res); r.err == nil {
		t.Error("expected error")
	}
	if !contains(types, MsgAbort) {
		t.Errorf("no abort sent: %v", types)
	}
}

func TestEdgeReceiverOnAcceptRejects(t *testing.T) {
	dir := t.TempDir()
	peer, c, res := startReceiver(t, dir, func(transfer.FileMetadata) bool { return false })
	sendMeta(t, peer, metaFor("f", []byte("data")))
	types := drain(peer, c)
	if r := waitResult(t, res); r.err == nil {
		t.Error("expected rejection error")
	}
	if !contains(types, MsgAbort) {
		t.Errorf("no abort: %v", types)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("files created despite rejection: %v", entries)
	}
}

func TestEdgeReceiverNilOnAcceptHook(t *testing.T) {
	data := []byte("hello")
	dir := t.TempDir()
	peer, _, res := startReceiver(t, dir, nil)
	sendMeta(t, peer, metaFor("f", data))
	awaitAck(t, peer)
	_ = peer.WriteFrame(MsgChunk, EncodeChunkWire(0, 0, uint32(len(data)), false, data))
	_ = peer.WriteFrame(MsgFileDone, nil)
	awaitAck(t, peer)
	if r := waitResult(t, res); r.err != nil {
		t.Fatal(r.err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "f"))
	if !bytes.Equal(got, data) {
		t.Errorf("got %q", got)
	}
}

func TestEdgeReceiverShortChunkWire(t *testing.T) {
	peer, c, res := startReceiver(t, t.TempDir())
	sendMeta(t, peer, metaFor("f", []byte("abcd")))
	awaitAck(t, peer)
	_ = peer.WriteFrame(MsgChunk, []byte("tiny"))
	drain(peer, c)
	if r := waitResult(t, res); r.err == nil {
		t.Error("expected decode error")
	}
}

func TestEdgeReceiverUnexpectedMessageMidStream(t *testing.T) {
	peer, c, res := startReceiver(t, t.TempDir())
	sendMeta(t, peer, metaFor("f", []byte("abcd")))
	awaitAck(t, peer)
	_ = peer.WriteFrame(MsgFileMeta, []byte("{}"))
	drain(peer, c)
	if r := waitResult(t, res); r.err == nil {
		t.Error("expected error")
	}
}

func TestEdgeReceiverChunkBeyondDeclaredSize(t *testing.T) {
	dir := t.TempDir()
	peer, c, res := startReceiver(t, dir)
	sendMeta(t, peer, metaFor("f", []byte("abcd")))
	awaitAck(t, peer)
	_ = peer.WriteFrame(MsgChunk, EncodeChunkWire(0, 1<<20, 4, false, []byte("zzzz")))
	drain(peer, c)
	if r := waitResult(t, res); r.err == nil {
		t.Error("out-of-range chunk accepted")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("leftover files: %v", entries)
	}
}

func TestEdgeReceiverDoneWithMissingChunks(t *testing.T) {
	dir := t.TempDir()
	data := []byte("AAAABBBB")
	peer, c, res := startReceiver(t, dir)
	sendMeta(t, peer, metaFor("f", data))
	awaitAck(t, peer)
	_ = peer.WriteFrame(MsgChunk, EncodeChunkWire(0, 0, 4, false, []byte("AAAA")))
	_ = peer.WriteFrame(MsgFileDone, nil)
	types := drain(peer, c)
	if r := waitResult(t, res); r.err == nil {
		t.Error("incomplete file accepted")
	}
	if !contains(types, MsgAbort) {
		t.Errorf("no abort: %v", types)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("leftover files: %v", entries)
	}
}

func TestEdgeReceiverWrongChecksum(t *testing.T) {
	dir := t.TempDir()
	data := []byte("AAAA")
	m := metaFor("f", data)
	m.Checksum[0] ^= 1
	peer, c, res := startReceiver(t, dir)
	sendMeta(t, peer, m)
	awaitAck(t, peer)
	_ = peer.WriteFrame(MsgChunk, EncodeChunkWire(0, 0, 4, false, data))
	_ = peer.WriteFrame(MsgFileDone, nil)
	drain(peer, c)
	if r := waitResult(t, res); r.err == nil {
		t.Error("bad checksum accepted")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("leftover files: %v", entries)
	}
}

func TestEdgeReceiverPeerDisconnectsMidStream(t *testing.T) {
	dir := t.TempDir()
	peer, c, res := startReceiver(t, dir)
	sendMeta(t, peer, metaFor("f", []byte("AAAABBBB")))
	awaitAck(t, peer)
	_ = peer.WriteFrame(MsgChunk, EncodeChunkWire(0, 0, 4, false, []byte("AAAA")))
	c.Close()
	if r := waitResult(t, res); r.err == nil {
		t.Error("expected error")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("staging file leaked: %v", entries)
	}
}

func TestEdgeReceiverTraversalNameStaysInDir(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "save")
	data := []byte("x")
	peer, c, res := startReceiver(t, dir)
	sendMeta(t, peer, metaFor("../escape.txt", data))
	mt, _, err := peer.ReadFrame()
	if err == nil && mt == MsgFileAck {
		_ = peer.WriteFrame(MsgChunk, EncodeChunkWire(0, 0, 1, false, data))
		_ = peer.WriteFrame(MsgFileDone, nil)
	}
	drain(peer, c)
	waitResult(t, res)
	if _, err := os.Stat(filepath.Join(parent, "escape.txt")); err == nil {
		t.Error("file escaped save directory")
	}
}

func TestEdgeReceiverContextCancelled(t *testing.T) {
	a, b := net.Pipe()
	_ = b.SetDeadline(time.Now().Add(5 * time.Second))
	defer b.Close()
	ctx, cancel := context.WithCancel(context.Background())
	res := make(chan edgeResult, 1)
	go func() {
		m, err := ReceiveFileStream(ctx, a, edgeKey, t.TempDir(), nil)
		a.Close()
		res <- edgeResult{m, err}
	}()
	peer, _ := NewFramer(b, edgeKey)
	sendMeta(t, peer, metaFor("f", []byte("abcd")))
	awaitAck(t, peer)
	cancel()
	_ = peer.WriteFrame(MsgChunk, EncodeChunkWire(0, 0, 1, false, []byte("a")))
	drain(peer, b)
	if r := waitResult(t, res); r.err == nil {
		t.Error("expected ctx error")
	}
}

// A peer that goes silent must not be able to hold the receiver past context cancellation.
func TestEdgeReceiverContextCancelWithSilentPeer(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	ctx, cancel := context.WithCancel(context.Background())
	res := make(chan edgeResult, 1)
	go func() {
		m, err := ReceiveFileStream(ctx, a, edgeKey, t.TempDir(), nil)
		a.Close()
		res <- edgeResult{m, err}
	}()
	peer, _ := NewFramer(b, edgeKey)
	sendMeta(t, peer, metaFor("f", []byte("abcd")))
	awaitAck(t, peer)
	cancel()
	select {
	case r := <-res:
		if r.err == nil {
			t.Error("expected ctx error")
		}
	case <-time.After(1500 * time.Millisecond):
		t.Error("receiver still blocked 1.5s after context cancel while peer is silent")
	}
}

func TestEdgeReceiverHugeChunkSizeDoesNotAllocateUpfront(t *testing.T) {
	enc, _ := zstd.NewWriter(nil)
	comp := enc.EncodeAll([]byte("AAAA"), nil)
	data := []byte("AAAA")
	m := metaFor("f", data)
	m.ChunkSize = 1 << 28 // attacker-controlled, 256 MB
	peer, c, res := startReceiver(t, t.TempDir())
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	sendMeta(t, peer, m)
	awaitAck(t, peer)
	_ = peer.WriteFrame(MsgChunk, EncodeChunkWire(0, 0, 4, true, comp))
	_ = peer.WriteFrame(MsgFileDone, nil)
	drain(peer, c)
	waitResult(t, res)
	runtime.ReadMemStats(&after)
	if delta := after.TotalAlloc - before.TotalAlloc; delta > 64<<20 {
		t.Errorf("receiver allocated %d MB because of a peer-declared ChunkSize", delta>>20)
	}
}

// ---------------- SendFile ----------------

func TestEdgeSendFileNilConnection(t *testing.T) {
	if err := SendFile(context.Background(), nil, nil, nil); err == nil {
		t.Error("expected error")
	}
	if err := SendFile(context.Background(), &conn.Connection{}, nil, nil); err == nil {
		t.Error("expected error for nil Conn")
	}
}

func TestEdgeSendFileStreamNilReader(t *testing.T) {
	a, _ := net.Pipe()
	defer a.Close()
	if err := SendFileStream(context.Background(), a, edgeKey, nil, nil); err == nil {
		t.Error("expected error")
	}
}

func newReader(t *testing.T, data []byte) *transfer.Reader {
	t.Helper()
	p := filepath.Join(t.TempDir(), "src.bin")
	_ = os.WriteFile(p, data, 0644)
	r, err := transfer.NewReader(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func startSender(t *testing.T, data []byte) (*Framer, net.Conn, chan error) {
	t.Helper()
	a, b := net.Pipe()
	_ = b.SetDeadline(time.Now().Add(5 * time.Second))
	r := newReader(t, data)
	res := make(chan error, 1)
	go func() {
		err := SendFileStream(context.Background(), a, edgeKey, r, nil)
		a.Close()
		res <- err
	}()
	f, _ := NewFramer(b, edgeKey)
	t.Cleanup(func() { b.Close() })
	return f, b, res
}

func waitErr(t *testing.T, res chan error) error {
	t.Helper()
	select {
	case e := <-res:
		return e
	case <-time.After(6 * time.Second):
		t.Fatal("sender did not finish")
		return nil
	}
}

func TestEdgeSenderReceiverRejects(t *testing.T) {
	peer, c, res := startSender(t, []byte("data"))
	if mt, _, _ := peer.ReadFrame(); mt != MsgFileMeta {
		t.Fatalf("first frame %d", mt)
	}
	_ = peer.WriteFrame(MsgAbort, []byte("no thanks"))
	drain(peer, c)
	if waitErr(t, res) == nil {
		t.Error("expected error")
	}
}

func TestEdgeSenderUnexpectedAckType(t *testing.T) {
	peer, c, res := startSender(t, []byte("data"))
	peer.ReadFrame()
	_ = peer.WriteFrame(MsgChunk, nil)
	drain(peer, c)
	if waitErr(t, res) == nil {
		t.Error("expected error")
	}
}

func TestEdgeSenderFinalAckAbort(t *testing.T) {
	peer, c, res := startSender(t, []byte("data"))
	peer.ReadFrame()
	_ = peer.WriteFrame(MsgFileAck, nil)
	for {
		mt, _, err := peer.ReadFrame()
		if err != nil {
			t.Fatal(err)
		}
		if mt == MsgFileDone {
			break
		}
	}
	_ = peer.WriteFrame(MsgAbort, []byte("integrity failed"))
	drain(peer, c)
	if waitErr(t, res) == nil {
		t.Error("expected error")
	}
}

func TestEdgeSenderPeerClosesAfterMeta(t *testing.T) {
	peer, c, res := startSender(t, []byte("data"))
	peer.ReadFrame()
	c.Close()
	if waitErr(t, res) == nil {
		t.Error("expected error")
	}
}

func TestEdgeSenderEmptyFileFullRoundTrip(t *testing.T) {
	dir := t.TempDir()
	a, b := net.Pipe()
	_ = a.SetDeadline(time.Now().Add(5 * time.Second))
	_ = b.SetDeadline(time.Now().Add(5 * time.Second))
	r := newReader(t, nil)
	errc := make(chan error, 1)
	go func() { errc <- SendFileStream(context.Background(), a, edgeKey, r, nil) }()
	m, err := ReceiveFileStream(context.Background(), b, edgeKey, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if e := <-errc; e != nil {
		t.Fatal(e)
	}
	if m.Size != 0 {
		t.Errorf("size %d", m.Size)
	}
}

func TestEdgeRoundTripLargerThanOneChunkWithProgress(t *testing.T) {
	data := bytes.Repeat([]byte("0123456789abcdef"), 300000) // ~4.8MB, compressible
	dir := t.TempDir()
	a, b := net.Pipe()
	_ = a.SetDeadline(time.Now().Add(10 * time.Second))
	_ = b.SetDeadline(time.Now().Add(10 * time.Second))
	r := newReader(t, data)
	var last int64
	errc := make(chan error, 1)
	go func() {
		errc <- SendFileStream(context.Background(), a, edgeKey, r, func(cur, total int64, _, _ uint64) { last = cur })
	}()
	if _, err := ReceiveFileStream(context.Background(), b, edgeKey, dir, nil); err != nil {
		t.Fatal(err)
	}
	if e := <-errc; e != nil {
		t.Fatal(e)
	}
	if last != int64(len(data)) {
		t.Errorf("final progress %d want %d", last, len(data))
	}
	got, _ := os.ReadFile(filepath.Join(dir, "src.bin"))
	if !bytes.Equal(got, data) {
		t.Error("content mismatch")
	}
}
