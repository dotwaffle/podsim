package session

// deliverInterruptions sends the orders that the simulation interrupted
// since the last call to the rail ledger, and refreshes the rail counts of
// the demand state (incident contract, section 8.5). The caller holds mu.
//
// The session calls it right after each Simulation.Step and at the end of
// each command, so the simulation holds no undelivered interruption when
// the session releases mu. A save and a state read also call it before
// they read the simulation, as a second guard. A rail record of an
// interrupted order is then never pending in a save or a publication.
func (s *Session) deliverInterruptions() {
	interrupted := s.simulation.DrainInterruptions()
	if len(interrupted) == 0 || s.demand.connections == nil {
		return
	}
	s.demand.connections.Interrupt(s.simulation.Tick(), interrupted)
	s.demand.refreshConnections()
}

// refreshConnections copies the outcome counts of the rail ledger to the
// demand state.
func (d *demandRun) refreshConnections() {
	if d.connections != nil {
		d.state.Connections = d.connections.Counts()
	}
}
