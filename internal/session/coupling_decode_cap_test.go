package session

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"hash/crc32"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// padJSON returns data with JSON whitespace after the root value, so that
// it has size bytes. Each scan and the typed decode accept the padding, so
// only a byte cap refuses the padded document.
func padJSON(t *testing.T, data []byte, size int) []byte {
	t.Helper()
	if len(data) > size {
		t.Fatalf("document has %d bytes, more than %d", len(data), size)
	}
	padded := make([]byte, size)
	copy(padded, data)
	for i := len(data); i < size; i++ {
		padded[i] = ' '
	}
	return padded
}

// gzipJSON compresses data in one gzip member.
func gzipJSON(t *testing.T, data []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer, err := gzip.NewWriterLevel(&buffer, gzip.BestSpeed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// storedGzip returns data in one gzip member of blocks stored deflate
// blocks. The data fills the first blocks, and the other blocks are
// empty. The member has 18 + len(data) + 5*blocks bytes.
func storedGzip(t *testing.T, data []byte, blocks int) []byte {
	t.Helper()
	const maxBlock = 1<<16 - 1
	if blocks < (len(data)+maxBlock-1)/maxBlock {
		t.Fatalf("%d blocks cannot hold %d bytes", blocks, len(data))
	}
	member := make([]byte, 0, 18+len(data)+5*blocks)
	checksum, size := crc32.ChecksumIEEE(data), uint32(len(data))
	// The header: magic, deflate, no flags, no time, no extra flags, and
	// an unknown operating system.
	member = append(member, 0x1f, 0x8b, 8, 0, 0, 0, 0, 0, 0, 0xff)
	for i := range blocks {
		chunk := data[:min(len(data), maxBlock)]
		data = data[len(chunk):]
		var final byte
		if i == blocks-1 {
			final = 1
		}
		n := uint16(len(chunk))
		member = append(member, final)
		member = binary.LittleEndian.AppendUint16(member, n)
		member = binary.LittleEndian.AppendUint16(member, ^n)
		member = append(member, chunk...)
	}
	member = binary.LittleEndian.AppendUint32(member, checksum)
	return binary.LittleEndian.AppendUint32(member, size)
}

// wantError stops the test when err is not an error with the text want.
func wantError(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || err.Error() != want {
		t.Fatalf("got %v, want %q", err, want)
	}
}

// TestCouplingDecodeCapsAtCallers sends a committed coupling frame at each
// byte cap of the stream and HTTP decode chain, and one byte over it. The
// padding is JSON whitespace, so a document over a cap passes each check
// before that cap. A cap that a later check repeats has a different text,
// so the test asserts the text of the cap that it is at.
func TestCouplingDecodeCapsAtCallers(t *testing.T) {
	if testing.Short() || raceEnabled {
		t.Skip("the cap documents run without -short and without the race detector")
	}
	data := couplingPhaseFixtures(t)
	_, topology, frame := couplingStreamFixture(t, data.Frames[0], sim.ExpressOrderContract)
	envelope, err := EncodeStreamJSON(couplingFullEnvelope(frame))
	if err != nil {
		t.Fatal(err)
	}
	httpState, err := EncodeStateJSON(topology, frame)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("HTTP state", func(t *testing.T) {
		state, err := DecodeStateJSON(padJSON(t, httpState, MaxStreamJSON))
		if err != nil || len(state.Simulation.CouplingGroups) != 1 {
			t.Fatal("refused the HTTP state at the cap", err)
		}
		_, err = DecodeStateJSON(padJSON(t, httpState, MaxStreamJSON+1))
		wantError(t, err, "HTTP state exceeds supported limit")
	})

	t.Run("stream envelope", func(t *testing.T) {
		decoded, err := DecodeStreamJSON(padJSON(t, envelope, MaxStreamJSON))
		if err != nil {
			t.Fatal("refused the envelope at the cap", err)
		}
		applied, err := ApplyStream(StreamFrame{}, "", 0, decoded)
		if err != nil || len(applied.State.Simulation.CouplingGroups) != 1 {
			t.Fatal("refused the frame of the envelope at the cap", err)
		}
		_, err = DecodeStreamJSON(padJSON(t, envelope, MaxStreamJSON+1))
		wantError(t, err, "state JSON too large")
	})

	t.Run("nested decode", func(t *testing.T) {
		// No caller gives decodeStreamJSON more than the cap: the hello
		// has a 4 KiB cap, a control has the 1 KiB read limit, and a member
		// is part of a document that decodeMarkedJSON bounds.
		var hello StreamHello
		raw, err := json.Marshal(StreamHello{Kind: "hello", Version: StreamVersion, ServerStart: "cap", CouplingContract: sim.CompactPairV1CouplingContract})
		if err != nil {
			t.Fatal(err)
		}
		if err := decodeStreamJSON(padJSON(t, raw, MaxStreamJSON), &hello); err != nil || hello.CouplingContract != sim.CompactPairV1CouplingContract {
			t.Fatal("refused the document at the cap", err)
		}
		wantError(t, decodeStreamJSON(padJSON(t, raw, MaxStreamJSON+1), &hello), "state JSON too large")
	})

	t.Run("inflated message", func(t *testing.T) {
		inflated, err := InflateStream(gzipJSON(t, padJSON(t, envelope, MaxStreamJSON)))
		if err != nil {
			t.Fatal("refused the message at the cap", err)
		}
		decoded, err := DecodeStreamJSON(inflated)
		if err != nil || len(decoded.Full.State.Simulation.CouplingGroups) != 1 {
			t.Fatal("refused the inflated envelope at the cap", err)
		}
		inflated, err = InflateStream(gzipJSON(t, padJSON(t, envelope, MaxStreamJSON+1)))
		if inflated != nil {
			t.Fatalf("inflated %d bytes over the cap", len(inflated))
		}
		wantError(t, err, "invalid state gzip size or trailing member")
	})

	t.Run("compressed message", func(t *testing.T) {
		// The remote client reads at most MaxStreamMessage bytes from the
		// websocket, so only a direct caller reaches this cap. Each message
		// is one gzip member of stored blocks, with empty blocks that set
		// its size. The member at the cap and the member one byte over it
		// are both valid, and they inflate to at most MaxStreamJSON bytes.
		const blockHeader, wrapper = 5, 18
		size := MaxStreamJSON
		for (MaxStreamMessage-wrapper-size)%blockHeader != 0 {
			size--
		}
		blocks := (MaxStreamMessage - wrapper - size) / blockHeader
		message := storedGzip(t, padJSON(t, envelope, size), blocks)
		if len(message) != MaxStreamMessage {
			t.Fatalf("the message has %d bytes, want %d", len(message), MaxStreamMessage)
		}
		inflated, err := InflateStream(message)
		if err != nil || len(inflated) != size {
			t.Fatal("refused the message at the cap", err)
		}
		decoded, err := DecodeStreamJSON(inflated)
		if err != nil || len(decoded.Full.State.Simulation.CouplingGroups) != 1 {
			t.Fatal("refused the envelope of the message at the cap", err)
		}
		message = storedGzip(t, padJSON(t, envelope, size+1), blocks)
		if len(message) != MaxStreamMessage+1 {
			t.Fatalf("the message has %d bytes, want %d", len(message), MaxStreamMessage+1)
		}
		inflated, err = InflateStream(message)
		if inflated != nil {
			t.Fatalf("inflated %d bytes from a message over the cap", len(inflated))
		}
		wantError(t, err, "compressed state too large")
		// A valid member with zero bytes after it.
		padded := make([]byte, MaxStreamMessage+1)
		copy(padded, gzipJSON(t, envelope))
		inflated, err = InflateStream(padded)
		if inflated != nil {
			t.Fatalf("inflated %d bytes from a message over the cap", len(inflated))
		}
		wantError(t, err, "compressed state too large")
	})
}

// TestCouplingHTTPMarkersRefuseAlone checks each half of the double
// refusal of a coupling HTTP state with an unmarked topology. Through
// DecodeStateJSON, the root markers of the envelope must be the markers of
// the topology, and that check refuses first. Through FrameState and the
// assembler, which do not read the root markers, the frame binding refuses
// the same topology and frame.
func TestCouplingHTTPMarkersRefuseAlone(t *testing.T) {
	t.Parallel()
	data := couplingPhaseFixtures(t)
	_, topology, frame := couplingStreamFixture(t, data.Frames[0], "")
	unmarked := topology
	unmarked.CouplingContract, unmarked.CouplingEnabled, unmarked.CouplingSites, unmarked.CouplingCorridors = "", false, nil, nil

	t.Run("envelope", func(t *testing.T) {
		t.Parallel()
		envelope := StateEnvelope{CouplingContract: topology.CouplingContract, OrderContract: topology.OrderContract, Topology: unmarked, Frame: frame}
		raw, err := jsonv2.Marshal(envelope, json.DefaultOptionsV1(), packedRequestOptions())
		if err != nil {
			t.Fatal(err)
		}
		_, err = DecodeStateJSON(raw)
		wantError(t, err, "HTTP coupling or order contracts disagree")
	})

	t.Run("frame binding", func(t *testing.T) {
		t.Parallel()
		const want = "topology coupling contract does not match state"
		_, err := FrameState(unmarked, frame.State)
		wantError(t, err, want)
		assembler, err := NewStreamAssembler(unmarked)
		if err != nil {
			t.Fatal(err)
		}
		_, err = assembler.State(frame)
		wantError(t, err, want)
	})
}
