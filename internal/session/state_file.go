package session

import (
	"bytes"
	"compress/gzip"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

const (
	// stateFormat and stateVersion identify the state file. Each change to
	// the members of the file needs a new version.
	stateFormat  = "podsim-session"
	stateVersion = 1
	// maxEpochBytes is the largest saved epoch.
	maxEpochBytes = 100
	// buildIDLength is the number of lowercase hex digits in a build ID.
	buildIDLength = 16
	// maxDemandErrorBytes is the largest saved demand error.
	maxDemandErrorBytes = 1 << 10
	// demandBudgetLimit is the demand budget that makes one order.
	// demandRun.step keeps the budget below it.
	demandBudgetLimit = 60 * sim.TicksPerSecond
	// maxSavedPods is the project fleet limit.
	maxSavedPods = project.MaxPods
	// maxSavedTrips is the largest saved queue. The session adds an order
	// only to a queue of fewer than QueueLimit orders. A restore can put the
	// order of each pod back in the queue. The traffic demo adds orders to a
	// full queue, but its fleet has only 4 pods.
	maxSavedTrips = QueueLimit + maxSavedPods
)

// Reason codes tell why the session cannot use a saved state.
const (
	reasonTooLarge           = "too_large"
	reasonInvalidState       = "invalid_state"
	reasonUnsupportedVersion = "unsupported_version"
)

var (
	errJSONTooDeep       = errors.New("JSON nesting is too deep")
	errJSONArrayTooLong  = errors.New("JSON array has too many elements")
	errJSONObjectTooLong = errors.New("JSON object has too many members")
	errProjectTooLarge   = errors.New("saved project is too large")
)

// jsonLimits bound the shape of JSON data. depth is the largest number of
// nested objects and arrays. elements is the largest number of values in
// one array, and members is the largest number of members in one object.
// arrays holds the element limit of the arrays at some paths, in place of
// elements. A path is a JSON Pointer in which "*" stands for each array
// index.
type jsonLimits struct {
	depth    int
	elements int64
	members  int64
	arrays   map[string]int64
}

// stateJSONLimits bound a state file before it is decoded, so that a small
// compressed file cannot decode into very large values. One limit for all
// arrays is not sufficient, because the sizes of nested arrays multiply.
// Thus each array of a state file has the limit that project.Validate, the
// simulation or the session applies to it. The project limits come from
// the project package. An array at another path is not valid. Its limit is
// more than the largest valid array. No member of a state file is a map, so a
// valid object has only a few tens of members. The member limit also bounds
// the memory that the decoder uses to find duplicate names.
var stateJSONLimits = jsonLimits{
	depth: 64, elements: 32_768, members: 256,
	arrays: map[string]int64{
		"/project/network/Nodes":                    project.MaxNodes,
		"/project/network/Lanes":                    project.MaxLanes,
		"/project/network/Stations":                 project.MaxStations,
		"/project/network/Stations/*/Berths":        project.MaxBerths,
		"/project/fleet":                            maxSavedPods,
		"/project/demandProfiles":                   project.MaxProfiles,
		"/project/demandProfiles/*/bands":           project.MaxBands,
		"/project/demandProfiles/*/flows":           project.MaxFlows,
		"/project/demandProfiles/*/flows/*/weights": project.MaxBands,
		"/simulation/pods":                          maxSavedPods,
		// A saved pod route has at most as many lanes as the network has
		// lanes and nodes.
		"/simulation/pods/*/route": project.MaxLanes + project.MaxNodes,
		"/simulation/waiting":      maxSavedTrips,
		// A saved trip route has at most as many lanes as the network has
		// nodes.
		"/simulation/waiting/*/route": project.MaxNodes,
	},
}

// stateFile is version 1 of the saved session state. The file on disk is
// the JSON form of stateFile, compressed with gzip. Each change to a member,
// also in the simulation and in the project, needs a new version.
// testdata/state_v1_members.txt lists the members.
//
// A saver can copy the values into a stateFile while it holds the session
// lock, and encode the stateFile after it releases the lock. ExportState
// returns a simulation that shares no storage with the session. The session
// replaces its project whole and does not change it in place.
type stateFile struct {
	Format  string `json:"format"`
	Version int    `json:"version"`
	// Final is true for a file that the server saved when it stopped.
	Final   bool      `json:"final"`
	SavedAt time.Time `json:"savedAt"`
	// Build is the build ID of the server that saved the file.
	Build           string `json:"build,omitempty"`
	Epoch           string `json:"epoch"`
	Revision        uint64 `json:"revision"`
	ProjectRevision uint64 `json:"projectRevision"`
	Generation      uint64 `json:"generation"`
	LastCheckpoint  uint64 `json:"lastCheckpoint,omitzero"`
	Speed           int    `json:"speed"`
	// RestoreAttempts counts the restores of the saved state since the last
	// periodic or final save.
	RestoreAttempts int            `json:"restoreAttempts,omitzero"`
	Demand          savedDemand    `json:"demand"`
	Simulation      sim.SavedState `json:"simulation"`
	Project         project.Config `json:"project"`
}

// savedDemand is the saved demand stream.
type savedDemand struct {
	State DemandState `json:"state"`
	// Random is the state of the random source, from rand.PCG.MarshalBinary.
	Random []byte `json:"random"`
	// Budget is the progress to the next generated order.
	Budget int `json:"budget"`
}

// stateError tells why the session cannot use a saved state. reason is the
// code that a restore reports, for example invalid_state.
type stateError struct {
	reason string
	err    error
}

func (e *stateError) Error() string { return e.reason + ": " + e.err.Error() }

func (e *stateError) Unwrap() error { return e.err }

func invalidState(err error) error {
	return &stateError{reason: reasonInvalidState, err: err}
}

// stateEncoder encodes state files. It keeps its gzip writer and its buffers
// for the next call. The zero value is ready to use. A stateEncoder is not
// safe for concurrent use.
type stateEncoder struct {
	zw     *gzip.Writer
	buffer bytes.Buffer
	// project holds the JSON form of the project member.
	project bytes.Buffer
}

// encode returns the deterministic JSON form of file, compressed with gzip
// BestSpeed. The result is valid until the next call. The error wraps
// ErrStateTooLarge when the JSON form or the compressed form has more than
// MaxStateBytes, or the project member has more than project.MaxFileBytes,
// because decodeStateFile rejects such a file.
func (e *stateEncoder) encode(file stateFile) ([]byte, error) {
	e.buffer.Reset()
	if e.zw == nil {
		zw, err := gzip.NewWriterLevel(&e.buffer, gzip.BestSpeed)
		if err != nil {
			return nil, fmt.Errorf("start session state compression: %w", err)
		}
		e.zw = zw
	} else {
		e.zw.Reset(&e.buffer)
	}
	limited := &limitedWriter{
		writer: e.zw, limit: MaxStateBytes,
		err: fmt.Errorf("session state has more than %d bytes: %w", MaxStateBytes, ErrStateTooLarge),
	}
	options := json.JoinOptions(json.Deterministic(true), json.WithMarshalers(json.MarshalToFunc(e.encodeProject)))
	if err := json.MarshalWrite(limited, file, options); err != nil {
		return nil, fmt.Errorf("encode session state: %w", err)
	}
	if err := e.zw.Close(); err != nil {
		return nil, fmt.Errorf("compress session state: %w", err)
	}
	if e.buffer.Len() > MaxStateBytes {
		return nil, fmt.Errorf("compressed session state has %d bytes: %w", e.buffer.Len(), ErrStateTooLarge)
	}
	return e.buffer.Bytes(), nil
}

// encodeProject writes the project member of a state file. The member has
// the size limit of decodeSavedProject. A project that came from a smaller
// file can have a larger member, because the member writes each number in
// full. For example, 1e20 has 21 digits.
func (e *stateEncoder) encodeProject(encoder *jsontext.Encoder, config project.Config) error {
	e.project.Reset()
	limited := &limitedWriter{
		writer: &e.project, limit: project.MaxFileBytes,
		err: fmt.Errorf("%w: more than %d bytes: %w", errProjectTooLarge, project.MaxFileBytes, ErrStateTooLarge),
	}
	if err := json.MarshalWrite(limited, config, json.Deterministic(true)); err != nil {
		return err
	}
	return encoder.WriteValue(e.project.Bytes())
}

// limitedWriter passes at most limit bytes to writer. A write past the
// limit fails with err.
type limitedWriter struct {
	writer         io.Writer
	limit, written int
	err            error
}

func (w *limitedWriter) Write(data []byte) (int, error) {
	if len(data) > w.limit-w.written {
		return 0, w.err
	}
	n, err := w.writer.Write(data)
	w.written += n
	return n, err
}

// decodeStateFile decodes a compressed state file. It checks the sizes, the
// version and the member names, but not the values. The caller checks the
// project, then the session members with validate, and then the simulation.
// Each error is a *stateError.
func decodeStateFile(data []byte) (stateFile, error) {
	if len(data) > MaxStateBytes {
		return stateFile{}, &stateError{
			reason: reasonTooLarge,
			err:    fmt.Errorf("compressed session state has %d bytes: %w", len(data), ErrStateTooLarge),
		}
	}
	raw, err := decompressState(data)
	if err != nil {
		return stateFile{}, err
	}
	if err := prescanJSON(raw, stateJSONLimits); err != nil {
		return stateFile{}, invalidState(err)
	}
	// The header decode ignores the other members, so that a file from a
	// later version gets the right reason.
	var header struct {
		Format  string `json:"format"`
		Version int    `json:"version"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return stateFile{}, invalidState(fmt.Errorf("decode session state header: %w", err))
	}
	if header.Format != stateFormat || header.Version != stateVersion {
		return stateFile{}, &stateError{
			reason: reasonUnsupportedVersion,
			err: fmt.Errorf("session state format %.20q version %d is not %q version %d",
				header.Format, header.Version, stateFormat, stateVersion),
		}
	}
	// The strict decode rejects unknown members, and a project member that
	// is larger than a project file can be.
	strict := json.JoinOptions(
		json.RejectUnknownMembers(true),
		json.WithUnmarshalers(json.UnmarshalFromFunc(decodeSavedProject)),
	)
	var file stateFile
	if err := json.Unmarshal(raw, &file, strict); err != nil {
		return stateFile{}, invalidState(fmt.Errorf("decode session state: %w", err))
	}
	return file, nil
}

// decompressState returns the JSON form of a state file. It reads at most
// MaxStateBytes+1 bytes, so a small file that expands to a very large one
// uses little memory.
func decompressState(data []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, invalidState(fmt.Errorf("decompress session state: %w", err))
	}
	raw, err := io.ReadAll(io.LimitReader(zr, MaxStateBytes+1))
	if err != nil {
		return nil, invalidState(fmt.Errorf("decompress session state: %w", err))
	}
	if len(raw) > MaxStateBytes {
		return nil, &stateError{
			reason: reasonTooLarge,
			err:    fmt.Errorf("decompressed session state has more than %d bytes: %w", MaxStateBytes, ErrStateTooLarge),
		}
	}
	return raw, nil
}

// prescanJSON checks data against limits. It reads the tokens only and
// makes no values. It does not look for duplicate names, because that needs
// memory for each name.
func prescanJSON(data []byte, limits jsonLimits) error {
	decoder := jsontext.NewDecoder(bytes.NewBuffer(data), jsontext.AllowDuplicateNames(true))
	// arrayLimits holds the element limit of the array at each depth.
	arrayLimits := make([]int64, limits.depth+1)
	for {
		token, err := decoder.ReadToken()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("scan session state: %w", err)
		}
		// Only the innermost object or array grows with a token. Each array
		// and object is innermost when its last value ends.
		depth := decoder.StackDepth()
		if depth > limits.depth {
			return fmt.Errorf("%w: more than %d levels at byte %d", errJSONTooDeep, limits.depth, decoder.InputOffset())
		}
		if token.Kind() == jsontext.KindBeginArray {
			arrayLimits[depth] = limits.arrayLimit(decoder)
		}
		switch kind, length := decoder.StackIndex(depth); {
		case kind == jsontext.KindBeginArray && length > arrayLimits[depth]:
			return fmt.Errorf("%w: more than %d at byte %d", errJSONArrayTooLong, arrayLimits[depth], decoder.InputOffset())
		case kind == jsontext.KindBeginObject && (length+1)/2 > limits.members:
			// The length counts each name and each value.
			return fmt.Errorf("%w: more than %d at byte %d", errJSONObjectTooLong, limits.members, decoder.InputOffset())
		}
	}
}

// arrayLimit returns the element limit of the array that the last token of
// decoder started.
func (limits jsonLimits) arrayLimit(decoder *jsontext.Decoder) int64 {
	// Each path in limits.arrays ends with a name. Thus an array that is not
	// the value of an object member has no path limit.
	depth := decoder.StackDepth()
	if parent, _ := decoder.StackIndex(depth - 1); parent != jsontext.KindBeginObject || len(limits.arrays) == 0 {
		return limits.elements
	}
	// tokens[level] is the name or the index of a value in the object or
	// the array at level.
	tokens := strings.Split(string(decoder.StackPointer()), "/")
	for level := 1; level < len(tokens); level++ {
		if kind, _ := decoder.StackIndex(level); kind == jsontext.KindBeginArray {
			tokens[level] = "*"
		}
	}
	if limit, ok := limits.arrays[strings.Join(tokens, "/")]; ok {
		return limit
	}
	return limits.elements
}

// decodeSavedProject decodes the project member of a state file. The member
// has the size limit of a project file. stateEncoder.encodeProject applies
// the same limit, so the server does not save a file that it cannot read.
func decodeSavedProject(decoder *jsontext.Decoder, config *project.Config) error {
	value, err := decoder.ReadValue()
	if err != nil {
		return err
	}
	if len(value) > project.MaxFileBytes {
		return fmt.Errorf("%w: %d bytes, more than %d", errProjectTooLarge, len(value), project.MaxFileBytes)
	}
	return json.Unmarshal(value, config, json.RejectUnknownMembers(true))
}

// validate checks the session members. The demand check uses the saved
// project, so the caller checks the project first. The revision can be 0,
// because a new session has revision 0 until its first change.
func (file *stateFile) validate() error {
	switch {
	case file.Epoch == "" || len(file.Epoch) > maxEpochBytes:
		return fmt.Errorf("epoch has %d bytes, not 1 to %d", len(file.Epoch), maxEpochBytes)
	case file.Build != "" && !isBuildID(file.Build):
		return fmt.Errorf("build %.20q is not %d lowercase hex digits", file.Build, buildIDLength)
	case file.ProjectRevision == 0 || file.Generation == 0:
		return fmt.Errorf("project revision %d and generation %d must be 1 or more",
			file.ProjectRevision, file.Generation)
	case file.Revision == math.MaxUint64 || file.Generation == math.MaxUint64:
		// A restore adds 1 to both.
		return fmt.Errorf("revision %d or generation %d is at the largest value", file.Revision, file.Generation)
	case file.RestoreAttempts < 0:
		return fmt.Errorf("restore attempts %d is negative", file.RestoreAttempts)
	case !slices.Contains([]int{1, 2, 4, 8}, file.Speed):
		return fmt.Errorf("speed %d is not 1, 2, 4 or 8", file.Speed)
	}
	return file.Demand.validate(project.DemandContext{Network: file.Project.Network, Profiles: file.Project.DemandProfiles})
}

// validate checks the saved demand stream against the saved project.
func (demand *savedDemand) validate(demandContext project.DemandContext) error {
	if err := project.ValidateDemand(demand.State.Config, demandContext); err != nil {
		return fmt.Errorf("saved demand: %w", err)
	}
	switch state := demand.State; {
	case state.Generated < 0 || state.Skipped < 0:
		return fmt.Errorf("demand counts %d generated and %d skipped must not be negative", state.Generated, state.Skipped)
	case len(state.Error) > maxDemandErrorBytes:
		return fmt.Errorf("demand error has %d bytes, more than %d", len(state.Error), maxDemandErrorBytes)
	case demand.Budget < 0 || demand.Budget >= demandBudgetLimit:
		return fmt.Errorf("demand budget %d is not 0 to %d", demand.Budget, demandBudgetLimit-1)
	}
	if err := new(rand.PCG).UnmarshalBinary(demand.Random); err != nil {
		return fmt.Errorf("demand random source: %w", err)
	}
	return nil
}

// isBuildID reports whether id has the form of a build ID.
func isBuildID(id string) bool {
	if len(id) != buildIDLength {
		return false
	}
	for _, c := range []byte(id) {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
