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
	"github.com/dotwaffle/podsim/internal/rail"
	"github.com/dotwaffle/podsim/internal/sim"
)

const (
	// stateFormat identifies the state file. Until the first release, an
	// added optional member with a safe zero value keeps the version. Each
	// other change to the members of the file needs a new version.
	stateFormat = "podsim-session"
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
	// only to a queue of fewer than QueueLimit orders, and a pod carries at
	// most sim.MaxSharedRideParties orders. A restore can put each order of
	// each pod back in the queue. Thus the queue and the pods hold at most
	// maxSavedTrips orders, also after repeated restores. The traffic demo
	// adds orders to a full queue, but its fleet has only 4 pods.
	maxSavedTrips = QueueLimit + sim.MaxSharedRideParties*maxSavedPods
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
	errJSONStringTooLong = errors.New("JSON string is too long")
	errProjectTooLarge   = errors.New("saved project is too large")
)

// jsonLimits bound the shape of JSON data. depth is the largest number of
// nested objects and arrays. elements is the largest number of values in
// one array, and members is the largest number of members in one object.
// arrays holds the element limit of the arrays at some paths, in place of
// elements. A path is a JSON Pointer in which "*" stands for each array
// index.
//
// stringBytes is the largest size of a string or a name in the input,
// with its quotes and escapes. When it is 0, strings have no limit.
//
// foldNames matches the names of a path without case, as encoding/json
// matches the member names of a struct. allowInvalidUTF8 lets the scan go
// on past a string that is not valid UTF-8, so that the scan checks the
// whole input.
type jsonLimits struct {
	depth            int
	elements         int64
	members          int64
	arrays           map[string]int64
	stringBytes      int
	foldNames        bool
	allowInvalidUTF8 bool
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
	depth: 64, elements: 65_536, members: 256,
	arrays: map[string]int64{
		"/project/network/Nodes":                       project.MaxNodes,
		"/project/network/Lanes":                       project.MaxLanes,
		"/project/network/Stations":                    project.MaxStations,
		"/project/network/Stations/*/Berths":           project.MaxBerths,
		"/project/network/Stations/*/Banks":            sim.MaxStationBanks,
		"/project/network/Stations/*/Banks/*/BerthIDs": project.MaxBerths,
		"/project/fleet":                               maxSavedPods,
		"/railConnections":                             project.MaxRailDeparturePassengers,
		"/project/railArrivals":                        project.MaxRailArrivals,
		"/project/railArrivals/*/destinations":         project.MaxRailDestinations,
		"/project/railDepartures":                      project.MaxRailArrivals,
		"/project/railDepartures/*/origins":            project.MaxRailDestinations,
		"/project/demandProfiles":                      project.MaxProfiles,
		"/project/demandProfiles/*/bands":              project.MaxBands,
		"/project/demandProfiles/*/flows":              project.MaxFlows,
		"/project/demandProfiles/*/flows/*/weights":    project.MaxBands,
		"/simulation/pods":                             maxSavedPods,
		// A saved pod route has at most as many lanes as the network has
		// lanes and nodes.
		"/simulation/pods/*/route": project.MaxLanes + project.MaxNodes,
		// A pod has at most one rider and one stop for each party.
		"/simulation/pods/*/riders": sim.MaxSharedRideParties,
		"/simulation/pods/*/stops":  sim.MaxSharedRideParties,
		"/simulation/waiting":       maxSavedTrips,
		// A saved trip route has at most as many lanes as the network has
		// nodes.
		"/simulation/waiting/*/route": project.MaxNodes,
		// A session records the sequences of at most clientLimit clients.
		"/sequences": clientLimit,
	},
}

// stateFile holds versions 6 through 8 of the saved session state.
// Version 6 has service metadata, boarding references, and retained compact
// queue certificates.
// Version 7 packs order text under the Express contract.
// Version 8 preserves physical groups under an independent coupling contract.
// Earlier versions reject later fields, including explicit empty or null values.
// The decoder rejects versions 2 through 5 before it reads their members.
// The file on disk is
// the JSON form of stateFile, compressed with gzip. Each change to a member,
// also in the simulation and in the project, needs a new version. Until the
// first release, an added optional member with a safe zero value is an
// exception. It keeps the version.
// testdata/state_v6_members.txt lists the members.
//
// A saver can copy the values into a stateFile while it holds the session
// lock, and encode the stateFile after it releases the lock. ExportState
// returns a simulation that shares no storage with the session. The session
// replaces its project whole and does not change it in place.
type stateFile struct {
	CouplingContract sim.CouplingContract `json:"couplingContract,omitzero"`
	// protected records a complete new-family recovery classification.
	protected     bool
	OrderContract sim.OrderContract `json:"orderContract,omitzero"`
	TextEncoding  string            `json:"textEncoding,omitzero"`
	// boardingTuples retains unresolved references in saved pod order.
	boardingTuples  [][]boardingTuple
	RailConnections []rail.Connection `json:"railConnections,omitempty"`
	Format          string            `json:"format"`
	Version         int               `json:"version"`
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
	RestoreAttempts int `json:"restoreAttempts,omitzero"`
	// Sequences holds the last command sequence of each client, in
	// increasing order of client ID.
	Sequences  []savedSequence `json:"sequences,omitempty"`
	Demand     savedDemand     `json:"demand"`
	Simulation sim.SavedState  `json:"simulation"`
	Project    project.Config  `json:"project"`
}

// savedSequence is the last command sequence of one client. A restore that
// keeps the epoch refuses a command with this sequence or a lower one,
// because the reply of the command is lost.
type savedSequence struct {
	Client   string `json:"client"`
	Sequence uint64 `json:"sequence"`
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
	if err := file.validateWireContract(); err != nil {
		return nil, err
	}
	if err := validateCouplingRoutes(file); err != nil {
		return nil, err
	}
	source := bindBoardingSource(file.Project)
	options := json.JoinOptions(json.Deterministic(true), json.WithMarshalers(json.JoinMarshalers(
		json.MarshalToFunc(e.encodeProject), json.MarshalToFunc(source.encodePod))))
	if file.packedOrders() {
		options = json.JoinOptions(json.Deterministic(true), json.WithMarshalers(json.JoinMarshalers(json.MarshalToFunc(e.encodeProject), json.MarshalToFunc(source.encodeExpressPod), json.MarshalToFunc(encodePackedSavedRequest))))
	}
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
// the size limit of decodeSavedProject. project.Validate measures the same
// encoding (see encodedSize in internal/project), so each valid project
// fits. Keep the options of the two encodings the same.
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

// strictStateOptions reject unknown members, and a project member that is
// larger than a project file can be.
var strictStateOptions = json.JoinOptions(
	json.RejectUnknownMembers(true),
	json.WithUnmarshalers(json.UnmarshalFromFunc(decodeSavedProject)),
)

// decodeStateFile decodes a compressed state file. It checks the sizes, the
// version and the member names, but not the values. The caller checks the
// project, then the session members with validate, and then the simulation.
// Each error is a *stateError.
func decodeStateFile(data []byte) (result stateFile, err error) {
	if len(data) > MaxStateBytes {
		return stateFile{}, preserveStateError(&stateError{
			reason: reasonTooLarge,
			err:    fmt.Errorf("compressed session state has %d bytes: %w", len(data), ErrStateTooLarge),
		})
	}
	raw, err := decompressStatePrefix(data)
	defer func() {
		if err != nil && len(raw) != 0 {
			err = protectCouplingDecodeError(raw, err)
		}
	}()
	if err != nil {
		if len(raw) > MaxStateBytes {
			err = preserveStateError(err)
		} else if len(raw) != 0 {
			err = protectCouplingDecodeError(raw, err)
		}
		if errors.Is(err, ErrStateTooLarge) {
			err = preserveStateError(err)
		}
		return stateFile{}, err
	}
	// Check the largest recognized shapes before even the small header decode.
	if err := prescanJSON(raw, expressSavedLimits()); err != nil {
		return stateFile{}, protectCouplingDecodeError(raw, invalidState(fmt.Errorf("scan session state: %w", err)))
	}
	// The header decode ignores the other members, so that a file from a
	// later version gets the right reason.
	var header struct {
		Format  string `json:"format"`
		Version int    `json:"version"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return stateFile{}, protectCouplingDecodeError(raw, invalidState(fmt.Errorf("decode session state header: %w", err)))
	}
	if header.Format == stateFormat && header.Version == couplingStateVersion {
		return decodeCouplingState(raw)
	}
	// Versions 2 through 5 came before the service metadata of version 6.
	if header.Format == stateFormat && header.Version >= 2 && header.Version < serviceStateVersion {
		return stateFile{}, &stateError{
			reason: reasonUnsupportedVersion,
			err:    fmt.Errorf("session state version %d is older than version %d and is not supported", header.Version, serviceStateVersion),
		}
	}
	if header.Format != stateFormat || header.Version < serviceStateVersion || header.Version > expressStateVersion {
		versionErr := &stateError{
			reason: reasonUnsupportedVersion,
			err: fmt.Errorf("session state format %.20q version %d is not %q version %d to %d",
				header.Format, header.Version, stateFormat, serviceStateVersion, couplingStateVersion),
		}
		if header.Version == couplingStateVersion {
			return stateFile{}, protectCouplingDecodeError(raw, versionErr)
		}
		return stateFile{}, versionErr
	}
	if err := scanContractMarkers(raw, header.Version == expressStateVersion, header.Version == expressStateVersion); err != nil {
		return stateFile{}, invalidState(err)
	}
	if header.Version == expressStateVersion {
		if err := scanPackedOrders(raw); err != nil {
			return stateFile{}, invalidState(err)
		}
	}
	limits := boardingStateLimits(compactStateLimits(serviceStateLimits()))
	if header.Version == expressStateVersion {
		limits = expressSavedLimits()
	}
	if err := prescanJSON(raw, limits); err != nil {
		return stateFile{}, invalidState(fmt.Errorf("scan session state: %w", err))
	}
	if err := scanStateCompactFields(raw); err != nil {
		return stateFile{}, invalidState(err)
	}
	if err := scanStateOrderFields(raw, header.Version); err != nil {
		return stateFile{}, invalidState(err)
	}
	var boardingTuples [][]boardingTuple
	decodePod := func(decoder *jsontext.Decoder, pod *sim.SavedPod) error {
		var tuples []boardingTuple
		var err error
		if header.Version == expressStateVersion {
			tuples, err = decodeExpressBoardingPod(decoder, pod)
		} else {
			tuples, err = decodeBoardingPod(decoder, pod)
		}
		if err == nil {
			boardingTuples = append(boardingTuples, tuples)
		}
		return err
	}
	options := strictStateOptions
	switch header.Version {
	case expressStateVersion:
		options = json.JoinOptions(options, json.WithUnmarshalers(json.JoinUnmarshalers(json.UnmarshalFromFunc(decodeSavedProject), json.UnmarshalFromFunc(decodeV6Platoon), json.UnmarshalFromFunc(decodeCompactQueue), json.UnmarshalFromFunc(decodePod), json.UnmarshalFromFunc(decodePackedSavedRequest))))
	default:
		options = json.JoinOptions(options, json.WithUnmarshalers(json.JoinUnmarshalers(
			json.UnmarshalFromFunc(decodeSavedProject), json.UnmarshalFromFunc(decodeV6Platoon), json.UnmarshalFromFunc(decodeCompactQueue), json.UnmarshalFromFunc(decodePod))))
	}
	var file stateFile
	if err := json.Unmarshal(raw, &file, options); err != nil {
		return stateFile{}, invalidState(fmt.Errorf("decode session state: %w", err))
	}
	if slices.ContainsFunc(boardingTuples, func(tuples []boardingTuple) bool { return len(tuples) != 0 }) {
		file.boardingTuples = boardingTuples
	}
	if err := validateSavedCompactMembers(file.Simulation); err != nil {
		return stateFile{}, invalidState(err)
	}
	if err := file.validateProjectVersion(); err != nil {
		return stateFile{}, invalidState(err)
	}
	return file, nil
}

func (file *stateFile) validateProjectVersion() error {
	if err := file.validateWireContract(); err != nil {
		return err
	}
	switch file.Version {
	case couplingStateVersion:
		return nil
	case expressStateVersion:
		if file.Project.Version != project.ExpressVersion {
			return errors.New("saved version 7 requires project 4")
		}
		return nil
	case serviceStateVersion:
		if file.Project.Version < 1 || file.Project.Version > project.ServiceVersion {
			return errors.New("saved version 6 requires project version 1, 2, or 3")
		}
		return nil
	default:
		return fmt.Errorf("saved version %d is not supported", file.Version)
	}
}

type savedPlatoonFields sim.SavedPlatoonLink

// decodePlatoonFields retains field presence, including explicit empty and null values.
func decodePlatoonFields(decoder *jsontext.Decoder) (sim.SavedPlatoonLink, jsontext.Value, jsontext.Value, error) {
	var saved struct {
		savedPlatoonFields
		Kind     jsontext.Value `json:"kind"`
		Terminal jsontext.Value `json:"terminalCell"`
	}
	value, err := decoder.ReadValue()
	if err != nil {
		return sim.SavedPlatoonLink{}, nil, nil, err
	}
	if err := json.Unmarshal(value, &saved, json.RejectUnknownMembers(true)); err != nil {
		return sim.SavedPlatoonLink{}, nil, nil, err
	}
	return sim.SavedPlatoonLink(saved.savedPlatoonFields), saved.Kind, saved.Terminal, nil
}

func decodeV6Platoon(decoder *jsontext.Decoder, link *sim.SavedPlatoonLink) error {
	value, err := decoder.ReadValue()
	if err != nil {
		return err
	}
	var fields map[string]jsontext.Value
	if err := json.Unmarshal(value, &fields); err != nil {
		return err
	}
	var kind string
	if err := json.Unmarshal(fields["kind"], &kind); err != nil && fields["kind"] != nil {
		return err
	}
	if kind == "compact-buffer-v1" {
		for _, field := range fields {
			if bytes.Equal(bytes.TrimSpace(field), []byte("null")) {
				return errors.New("compact platoon link contains null")
			}
		}
	}
	return decodeFixedPlatoon(jsontext.NewDecoder(bytes.NewReader(value)), link)
}

func decodeFixedPlatoon(decoder *jsontext.Decoder, link *sim.SavedPlatoonLink) error {
	saved, kind, terminal, err := decodePlatoonFields(decoder)
	if err != nil {
		return err
	}
	if kind != nil {
		if bytes.Equal(bytes.TrimSpace(kind), []byte("null")) {
			return errors.New("platoon kind is null")
		}
		if err := json.Unmarshal(kind, &saved.Kind); err != nil {
			return err
		}
	}
	switch saved.Kind {
	case "":
		if terminal != nil {
			return errors.New("complete-lane platoon contains terminalCell")
		}
	case "buffer", "compact-buffer-v1":
		if saved.Kind == "compact-buffer-v1" && (saved.Turn != 0 || saved.Draining) {
			return errors.New("invalid compact platoon link")
		}
		if terminal == nil || bytes.Equal(bytes.TrimSpace(terminal), []byte("null")) || saved.Lanes != 1 {
			return errors.New("buffer platoon has no integer terminalCell or is not one lane")
		}
		var cell int
		if err := json.Unmarshal(terminal, &cell); err != nil {
			return err
		}
		if cell < 0 {
			return errors.New("buffer platoon terminalCell is negative")
		}
		saved.TerminalCell = new(cell)
	default:
		return errors.New("unknown platoon kind")
	}
	*link = saved
	return nil
}

// decompressState returns the JSON form of a state file. It reads at most
// MaxStateBytes+1 bytes, so a small file that expands to a very large one
// uses little memory.
func decompressState(data []byte) ([]byte, error) {
	raw, err := decompressStatePrefix(data)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

// decompressStatePrefix retains bounded JSON evidence when gzip fails after output.
func decompressStatePrefix(data []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, invalidState(fmt.Errorf("decompress session state: %w", err))
	}
	raw, err := io.ReadAll(io.LimitReader(zr, MaxStateBytes+1))
	if err != nil {
		return raw, invalidState(fmt.Errorf("decompress session state: %w", err))
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
	decoder := jsontext.NewDecoder(bytes.NewBuffer(data),
		jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(limits.allowInvalidUTF8))
	// arrayLimits holds the element limit of the array at each depth.
	arrayLimits := make([]int64, limits.depth+1)
	for {
		kind, err := limits.readToken(decoder)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		// Only the innermost object or array grows with a token. Each array
		// and object is innermost when its last value ends.
		depth := decoder.StackDepth()
		if depth > limits.depth {
			return fmt.Errorf("%w: more than %d levels at byte %d", errJSONTooDeep, limits.depth, decoder.InputOffset())
		}
		if kind == jsontext.KindBeginArray {
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

// readToken reads the next token of decoder and returns its kind. It
// checks the size of a string or a name against limits.stringBytes.
func (limits jsonLimits) readToken(decoder *jsontext.Decoder) (jsontext.Kind, error) {
	if limits.stringBytes == 0 || decoder.PeekKind() != jsontext.KindString {
		token, err := decoder.ReadToken()
		return token.Kind(), err
	}
	// ReadValue gives the input bytes of the string and makes no copy.
	value, err := decoder.ReadValue()
	if err == nil && len(value) > limits.stringBytes {
		err = fmt.Errorf("%w: more than %d bytes at byte %d", errJSONStringTooLong, limits.stringBytes, decoder.InputOffset())
	}
	return jsontext.KindString, err
}

// arrayLimit returns the element limit of the array that the last token of
// decoder started.
func (limits jsonLimits) arrayLimit(decoder *jsontext.Decoder) int64 {
	// Paths can name object members or nested array items.
	if len(limits.arrays) == 0 {
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
	path := strings.Join(tokens, "/")
	if limit, ok := limits.arrays[path]; ok {
		return limit
	}
	if limits.foldNames {
		for name, limit := range limits.arrays {
			if strings.EqualFold(name, path) {
				return limit
			}
		}
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
	if err := file.validateProjectVersion(); err != nil {
		return err
	}
	switch {
	case file.Epoch == "" || len(file.Epoch) > maxEpochBytes:
		return fmt.Errorf("epoch has %d bytes, not 1 to %d", len(file.Epoch), maxEpochBytes)
	case file.Build != "" && !isBuildID(file.Build):
		return fmt.Errorf("build %.20q is not %d lowercase hex digits", file.Build, buildIDLength)
	case file.ProjectRevision == 0 || file.Generation == 0:
		return fmt.Errorf("project revision %d and generation %d must be 1 or more",
			file.ProjectRevision, file.Generation)
	case file.Revision == math.MaxUint64 || file.ProjectRevision == math.MaxUint64 || file.Generation == math.MaxUint64:
		// A restore adds 1 to the revision and the generation. It also adds
		// 1 to the project revision when it applies the demand settings of
		// the project file.
		return fmt.Errorf("revision %d, project revision %d or generation %d is at the largest value",
			file.Revision, file.ProjectRevision, file.Generation)
	case file.RestoreAttempts < 0:
		return fmt.Errorf("restore attempts %d is negative", file.RestoreAttempts)
	case !validSpeed(file.Speed):
		return fmt.Errorf("speed %d is not 1, 2, 5, 15 or 60", file.Speed)
	}
	if err := validateSequences(file.Sequences); err != nil {
		return err
	}
	if err := file.Demand.validate(project.DemandContext{Network: file.Project.Network, Profiles: file.Project.DemandProfiles, RailArrivals: file.Project.RailArrivals, RailDepartures: file.Project.RailDepartures}); err != nil {
		return err
	}
	if len(file.RailConnections) == 0 && file.Demand.State.Connections == (rail.Counts{}) {
		return nil
	}
	_, err := rail.RestoreConnections(file.Project.RailDepartures, file.RailConnections, file.Simulation, file.Demand.State.Connections)
	return err
}

// validateSequences checks the saved command sequences. The session applies
// the same limits to commands. The client IDs must increase, so that each
// client has one sequence. The prescan limits the number of sequences.
func validateSequences(sequences []savedSequence) error {
	for index, saved := range sequences {
		switch {
		case saved.Client == "" || len(saved.Client) > maxClientBytes:
			return fmt.Errorf("client ID has %d bytes, not 1 to %d", len(saved.Client), maxClientBytes)
		case saved.Sequence == 0:
			return fmt.Errorf("client %.20q has sequence 0", saved.Client)
		case index > 0 && saved.Client <= sequences[index-1].Client:
			return fmt.Errorf("client %.20q is not after client %.20q", saved.Client, sequences[index-1].Client)
		}
	}
	return nil
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
