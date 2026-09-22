package connectapi

import (
	"net/http/httptest"
	"reflect"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectgzip"
	"connectrpc.com/connect/v2/connecthttp"
	"google.golang.org/protobuf/proto"

	podsimv1 "github.com/dotwaffle/podsim/internal/gen/podsim/v1"
	"github.com/dotwaffle/podsim/internal/gen/podsim/v1/podsimv1connect"
	"github.com/dotwaffle/podsim/internal/session"
)

func TestBinaryClientReadsEquivalentSessionAndRetriesCommand(t *testing.T) {
	t.Parallel()
	shared, err := session.New()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(shared.Handler(t.TempDir(), Register(shared)))
	defer server.Close()
	transport := connecthttp.NewTransport(server.Client(), server.URL,
		connecthttp.WithCompressors(connectgzip.New()),
	)
	client := podsimv1connect.NewPodsimServiceClient(connect.NewClient(transport))

	topologyMessage, err := client.GetTopology(t.Context(), &podsimv1.GetTopologyRequest{})
	if err != nil {
		t.Fatal(err)
	}
	frameMessage, err := client.GetState(t.Context(), &podsimv1.GetStateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	state, err := session.FrameState(topologyFromProto(topologyMessage), stateFromProto(frameMessage))
	if err != nil {
		t.Fatal(err)
	}
	wantState := shared.State()
	if !reflect.DeepEqual(state, wantState) {
		t.Fatalf("binary client changed session state: network=%v simulation=%v demand=%v metadata=%v vehicles=%v berths=%v pending=%v",
			reflect.DeepEqual(state.Network, wantState.Network),
			reflect.DeepEqual(state.Simulation, wantState.Simulation),
			reflect.DeepEqual(state.Demand, wantState.Demand),
			state.Epoch == wantState.Epoch && state.Revision == wantState.Revision && state.ProjectRevision == wantState.ProjectRevision && state.Generation == wantState.Generation && state.Redistribution == wantState.Redistribution && state.Speed == wantState.Speed,
			reflect.DeepEqual(state.Simulation.Vehicles, wantState.Simulation.Vehicles),
			reflect.DeepEqual(state.Simulation.Berths, wantState.Simulation.Berths),
			reflect.DeepEqual(state.Simulation.Pending, wantState.Simulation.Pending),
		)
	}
	projectMessage, err := client.GetProject(t.Context(), &podsimv1.GetProjectRequest{})
	if err != nil {
		t.Fatal(err)
	}
	wantProject := shared.Project()
	gotProject := configFromProto(projectMessage.GetProject())
	if projectMessage.GetRevision() != wantProject.Revision || !reflect.DeepEqual(gotProject, wantProject.Project) {
		t.Fatalf("binary client changed editable project: network=%v fleet=%v demand=%v profiles=%v",
			reflect.DeepEqual(gotProject.Network, wantProject.Project.Network),
			reflect.DeepEqual(gotProject.Fleet, wantProject.Project.Fleet),
			reflect.DeepEqual(gotProject.Demand, wantProject.Project.Demand),
			reflect.DeepEqual(gotProject.DemandProfiles, wantProject.Project.DemandProfiles),
		)
	}

	request := &podsimv1.CommandRequest{
		Client: "connect-test", Sequence: 1, Epoch: state.Epoch,
		Command: &podsimv1.CommandRequest_Trip{Trip: &podsimv1.TripCommand{Origin: "harbor", Destination: "market"}},
	}
	first, err := client.Command(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := client.Command(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.GetOrderId() != 1 || first.GetError() != "" || !proto.Equal(first, retry) || shared.State().Simulation.Submitted != 1 {
		t.Fatalf("retry mismatch: first=%v retry=%v", first, retry)
	}
}

func TestBinaryClientRejectsMissingCommandAction(t *testing.T) {
	t.Parallel()
	shared, err := session.New()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(shared.Handler(t.TempDir(), Register(shared)))
	defer server.Close()
	transport := connecthttp.NewTransport(server.Client(), server.URL)
	client := podsimv1connect.NewPodsimServiceClient(connect.NewClient(transport))
	_, err = client.Command(t.Context(), &podsimv1.CommandRequest{Client: "test", Sequence: 1, Epoch: shared.State().Epoch})
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("error = %v", err)
	}
}
