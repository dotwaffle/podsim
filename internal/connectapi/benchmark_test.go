package connectapi

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"testing"

	"google.golang.org/protobuf/proto"

	podsimv1 "github.com/dotwaffle/podsim/internal/gen/podsim/v1"
	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/session"
)

type protocolFixture struct {
	jsonFrame  []byte
	protoFrame []byte
}

func BenchmarkProtocolCodecs(b *testing.B) {
	for _, fixture := range []struct {
		name   string
		config project.Config
	}{{"scale100", scenarios.Scale100()}, {"london", scenarios.London()}} {
		b.Run(fixture.name, func(b *testing.B) {
			measureProtocolFixture(b, newProtocolFixture(b, fixture.config))
		})
	}
}

func measureProtocolFixture(b *testing.B, fixture protocolFixture) {
	b.Helper()
	b.Run("json_marshal", func(b *testing.B) {
		frame := fixtureJSONFrame(b, fixture.jsonFrame)
		b.ReportAllocs()
		for b.Loop() {
			if _, err := json.Marshal(frame); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("protobuf_marshal", func(b *testing.B) {
		frame := new(podsimv1.GetStateResponse)
		if err := proto.Unmarshal(fixture.protoFrame, frame); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		for b.Loop() {
			if _, err := proto.Marshal(frame); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("json_unmarshal", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			var frame session.StateFrame
			if err := json.Unmarshal(fixture.jsonFrame, &frame); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("protobuf_unmarshal", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			frame := new(podsimv1.GetStateResponse)
			if err := proto.Unmarshal(fixture.protoFrame, frame); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func newProtocolFixture(tb testing.TB, config project.Config) protocolFixture {
	tb.Helper()
	shared, err := session.NewWithProject(config)
	if err != nil {
		tb.Fatal(err)
	}
	stations := project.PassengerStations(config.Network)
	epoch := shared.State().Epoch
	for index := range 200 {
		origin := stations[index%len(stations)].ID
		destination := stations[(index*7+1)%len(stations)].ID
		if destination == origin {
			destination = stations[(index+1)%len(stations)].ID
		}
		reply := shared.Apply(session.Command{
			Client: fmt.Sprintf("measurement-%03d", index), Sequence: 1, Epoch: epoch,
			Action: "trip", Origin: origin, Destination: destination,
		})
		if reply.Error != "" {
			tb.Fatalf("submit measurement request %d: %s", index, reply.Error)
		}
	}
	frame := shared.Frame()
	frame.Epoch = "measurement-epoch"
	jsonFrame, err := json.Marshal(frame)
	if err != nil {
		tb.Fatal(err)
	}
	protoFrame, err := proto.Marshal(stateToProto(frame))
	if err != nil {
		tb.Fatal(err)
	}
	legacyState := shared.State()
	legacyState.Epoch = frame.Epoch
	legacyJSON, err := json.Marshal(legacyState)
	if err != nil {
		tb.Fatal(err)
	}
	topology := shared.Topology()
	topology.Epoch = frame.Epoch
	topologyJSON, topologyProto := mustJSON(tb, topology), mustProto(tb, topologyToProto(topology))
	projectJSON, projectProto := mustJSON(tb, shared.Project()), mustProto(tb, projectToProto(shared.Project()))
	tb.Logf("payload legacy_json=%d legacy_json_gzip_1=%d json=%d json_gzip_1=%d json_gzip_6=%d protobuf=%d protobuf_gzip_1=%d topology_json=%d topology_json_gzip_1=%d topology_protobuf=%d topology_protobuf_gzip_1=%d project_json=%d project_protobuf=%d",
		len(legacyJSON), gzipSize(tb, legacyJSON, gzip.BestSpeed),
		len(jsonFrame), gzipSize(tb, jsonFrame, gzip.BestSpeed), gzipSize(tb, jsonFrame, gzip.DefaultCompression),
		len(protoFrame), gzipSize(tb, protoFrame, gzip.BestSpeed),
		len(topologyJSON), gzipSize(tb, topologyJSON, gzip.BestSpeed),
		len(topologyProto), gzipSize(tb, topologyProto, gzip.BestSpeed),
		len(projectJSON), len(projectProto),
	)
	return protocolFixture{jsonFrame: jsonFrame, protoFrame: protoFrame}
}

func fixtureJSONFrame(tb testing.TB, encoded []byte) session.StateFrame {
	tb.Helper()
	var frame session.StateFrame
	if err := json.Unmarshal(encoded, &frame); err != nil {
		tb.Fatal(err)
	}
	return frame
}

func mustJSON(tb testing.TB, value any) []byte {
	tb.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		tb.Fatal(err)
	}
	return encoded
}

func mustProto(tb testing.TB, value proto.Message) []byte {
	tb.Helper()
	encoded, err := proto.Marshal(value)
	if err != nil {
		tb.Fatal(err)
	}
	return encoded
}

func gzipSize(tb testing.TB, payload []byte, level int) int {
	tb.Helper()
	var output bytes.Buffer
	writer, err := gzip.NewWriterLevel(&output, level)
	if err != nil {
		tb.Fatal(err)
	}
	if _, err := writer.Write(payload); err != nil {
		tb.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		tb.Fatal(err)
	}
	return output.Len()
}

func BenchmarkProtocolGzip(b *testing.B) {
	fixture := newProtocolFixture(b, scenarios.London())
	for _, input := range []struct {
		name    string
		payload []byte
		level   int
	}{
		{"json_level_1", fixture.jsonFrame, gzip.BestSpeed},
		{"json_level_6", fixture.jsonFrame, gzip.DefaultCompression},
		{"protobuf_level_1", fixture.protoFrame, gzip.BestSpeed},
	} {
		b.Run(input.name, func(b *testing.B) {
			writer, err := gzip.NewWriterLevel(io.Discard, input.level)
			if err != nil {
				b.Fatal(err)
			}
			defer func() { _ = writer.Close() }()
			b.ReportAllocs()
			for b.Loop() {
				writer.Reset(io.Discard)
				if _, err := writer.Write(input.payload); err != nil {
					b.Fatal(err)
				}
				if err := writer.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
