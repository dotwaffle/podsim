# Offline energy estimate

Status: implemented and checked on October 3, 2026, under the October 2 compatible-contract grant.
The [qualification record](measurements/energy-model-qualification.json) identifies the source and verification evidence.
The comparison command gains an opt-in `flat-v1` estimate.
Energy does not control native physics, routes, or dispatch.
Project, save, snapshot, session, and stream schemas stay unchanged.

## Authored model

An energy file supplies one profile for each normalized class in the fleet.
It must name the `flat-v1` model and explicitly supply every coefficient, including zeros.
Unknown or duplicate members, missing values, explicit null, nonfinite values, and unsupported classes fail before simulation starts.
Duplicate class profiles fail.
Unused supported profiles are permitted.
Every class used by the requested fleet must have a profile.
An omitted fleet class normalizes to `legacy`.
Unsupported physical classes, including `express`, fail.
The file uses the existing project-file size bound.

Each profile supplies fixed effective mass in kilograms, constant resistance in newtons, quadratic resistance, drive efficiency, recovery fraction, and auxiliary watts.
Quadratic resistance uses `N/(m/s)^2`.
Mass must be positive.
Resistance and auxiliary values must be nonnegative.
Drive efficiency must be greater than zero and at most one.
Recovery fraction must be between zero and one.
The author includes any assumed payload in fixed mass.
The model does not infer passenger mass or physical coefficients.

For one advanced tick, `dt = 1/60 second` and `vMean = actualDistance/dt`.
Resistance work is `(constantResistance + quadraticResistance*vMean^2)*actualDistance`.
Wheel work is `mass*(endSpeed^2-startSpeed^2)/2 + resistanceWork`.
Positive wheel work divided by drive efficiency gives gross traction draw.
Negative wheel work multiplied by recovery fraction gives recovered energy.
Resistance reduces recoverable braking work before recovery applies.
All vehicles consume authored auxiliary watts during every advanced tick, including idle and blocked vehicles.
Net joules equal traction draw plus auxiliary energy minus recovered energy.
Net energy can be negative in a window that starts with kinetic energy.

This is a discrete flat-track estimate.
It has no grade, battery, charging, drafting, or empirical efficiency claim.

## Motion recording

Native simulation gains an optional latest-tick motion frame.
Recording is disabled by default and allocates no recorder when disabled.
Each successful advanced tick updates the frame clock, even when no vehicle moves.
Movement records contain vehicle identity, normalized class, exact traveled distance, and initial and final speed.
`moveAndMeasure` supplies ordinary and compact path distance.
The recorder captures initial speed before `move` and final speed after it returns.
Terminal arrival sets final speed to zero, so that frame includes braking.
It retains zero-distance speed changes.

The accessor returns owned storage.
Clone copies recording storage independently.
Reset clears it, and restore starts with recording disabled.
No energy state or callback enters the simulation.
First enable and disable/re-enable clear the frame and start at the current tick.
Repeated enable preserves the current frame.
Reset requires a fresh meter window.
The recorder publishes only successful advanced ticks and retains its previous frame after a failed compact tick.

The comparison meter consumes every successful step.
It accepts only the next contiguous tick and rejects missed frames or clock regression.
Duplicate reads of the current valid frame add nothing.
It uses normalized static fleet counts for full-fleet auxiliary energy.
Compact errors abort the comparison before the meter accepts that tick.
Every sample and prospective total must be finite.
Distance and speeds must be nonnegative, and each class must have an authored profile.
Arithmetic errors leave both totals and the consumed clock unchanged.

## Report and gates

The enabled report identifies the model, authored profiles, fixed-mass assumption, class counts, and measurement window.
It reports gross traction draw, recovered energy, auxiliary energy, and signed net energy in joules.
Conditional JSON members and CSV columns preserve existing disabled output.
Energy adds no matrix dimension or default coefficients.

Tests cover constant speed, acceleration and braking, resistance-reduced recovery, and full-fleet idle auxiliary consumption.
They also cover empty frames, pauses, departure ticks, arrival braking, compact distance, missed frames, and arithmetic overflow.
Clone, reset, and restore tests cover ownership and window boundaries.
Disabled runs must preserve motion, ownership, saved bytes, and comparison reports.

## File and report interface

Use `compare -energy-file PATH` to enable the estimate.
The file has this shape.
The numbers below describe an analytic test, not physical defaults.

```json
{
  "model": "flat-v1",
  "profiles": {
    "legacy": {
      "mass_kg": 100,
      "constant_resistance_n": 0,
      "quadratic_resistance_n_per_mps2": 0,
      "drive_efficiency": 1,
      "recovery_fraction": 0,
      "auxiliary_watts": 0
    }
  }
}
```

Native callers use `SetMotionRecording(bool)` and `MotionFrame() (MotionFrame, bool)`.
Enable supplies an empty baseline frame at the current tick.
Reset keeps recording enabled, clears storage, and supplies a tick-zero baseline.
The caller creates a new meter window after reset.
Restore starts with recording disabled.
`StartDemo` creates a replacement simulation and drops recording.
Enable recording after creating a demo.
Duplicate frame reads still validate samples before they add no energy.

Enabled JSON reports use schema version 15 and an `energy` object in each result.
Disabled JSON reports keep their existing schema version and members.
CSV adds energy columns only when a result contains energy.
Table output adds an energy record for each enabled result.
Report windows use start and end ticks at `sim.TicksPerSecond`.
The measurement window covers successful advanced ticks through the actual end of the run.
