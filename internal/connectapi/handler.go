// Package connectapi exposes the session through the experimental typed protocol.
package connectapi

import (
	"compress/gzip"
	"context"
	"net/http"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectgzip"
	"connectrpc.com/connect/v2/connecthttp"

	podsimv1 "github.com/dotwaffle/podsim/internal/gen/podsim/v1"
	"github.com/dotwaffle/podsim/internal/gen/podsim/v1/podsimv1connect"
	"github.com/dotwaffle/podsim/internal/session"
)

type service struct{ session *session.Session }

// Register returns a route extension for Session.HandlerFS.
func Register(shared *session.Session) func(*http.ServeMux) {
	return func(mux *http.ServeMux) {
		server := connect.NewServer()
		podsimv1connect.RegisterPodsimServiceHandler(server, service{session: shared})
		connecthttp.Mount(mux, server,
			connecthttp.WithCompressors(connectgzip.New(connectgzip.WithLevel(gzip.BestSpeed))),
			connecthttp.WithCompressMinBytes(1024),
		)
	}
}

// GetTopology returns the current immutable network revision.
func (s service) GetTopology(context.Context, *podsimv1.GetTopologyRequest) (*podsimv1.GetTopologyResponse, error) {
	return topologyToProto(s.session.Topology()), nil
}

// GetState returns the current normalized dynamic frame.
func (s service) GetState(context.Context, *podsimv1.GetStateRequest) (*podsimv1.GetStateResponse, error) {
	return stateToProto(s.session.Frame()), nil
}

// GetProject returns the complete editable project.
func (s service) GetProject(context.Context, *podsimv1.GetProjectRequest) (*podsimv1.GetProjectResponse, error) {
	return projectToProto(s.session.Project()), nil
}

// Command applies one retry-safe session mutation.
func (s service) Command(_ context.Context, request *podsimv1.CommandRequest) (*podsimv1.CommandResponse, error) {
	command, err := commandFromProto(request)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err.Error())
	}
	return replyToProto(s.session.Apply(command)), nil
}
