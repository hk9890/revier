package core

// Phase is the part of a shutdown or a restore a Progress reports.
type Phase uint8

const (
	// PhaseRecheck is a shutdown reading the agents of its plan again.
	PhaseRecheck Phase = iota
	// PhaseSave is a shutdown saving the session it ends.
	PhaseSave
	// PhaseClose is one step of a shutdown.
	PhaseClose
	// PhaseWait is a shutdown waiting for what it closed to go.
	PhaseWait
	// PhaseSurvey is the survey a restore is planned from. It runs before
	// Restore, so the caller reports it itself.
	PhaseSurvey
	// PhaseLaunch is one step of a restore.
	PhaseLaunch
	// PhaseHome is a restore returning to the project the session was left
	// on.
	PhaseHome
)

// Progress is one report of a shutdown or a restore while it runs. A step is
// reported twice: before it starts, so a host that hangs on it is the one
// named, and once it is done, with what came of it.
type Progress struct {
	Phase Phase
	// N is the step's place in the walk, counted from 1, and Of how many
	// steps the walk has. Of is set on every phase of a shutdown.
	N, Of int
	// Label names the step: a close step's Name, a restore step's target, or
	// the project a restore returns to.
	Label string
	// Done is the report after the step. Closed and Restored then carry what
	// came of it; before the step they carry the step alone.
	Done     bool
	Closed   CloseResult
	Restored RestoreResult
}
