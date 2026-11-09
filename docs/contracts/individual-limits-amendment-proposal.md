# Individual limits amendment proposal

Status: draft of November 9, 2026, for the maintainer.
It changes no limit, and no run uses the shapes below.

## Problem

The [numerical limits](../design/experimental-adoption.md#numerical-limits) of the adoption gates have two individual limits for each matched request.
The additional pickup wait is at most the larger of 30 s and 5 percent of the baseline wait.
The additional journey is at most the larger of 60 s and 5 percent of the baseline journey.
The design says that these limits "permit small ordering changes while rejecting the hundreds-of-seconds regressions already observed".

In a saturated fleet, a small change can move many requests past these limits:

- In `LF` on the screen workload of `pm-peak`, one move of one idle pod for 200 s puts 364 to 438 requests over the wait limit at a queue limit of 200, and 151 to 294 with no queue limit ([sensitivity of the individual limits](express-london-adoption-screen-proposal.md#sensitivity-of-the-individual-limits-to-one-pod-move)).
- Option B of the [positioning pilot](loaded-positioning-proposal.md#pilot-result) puts 355 to 441 requests of `LF` over the wait limit, with a change of mean wait of -2.7 to +3.2 percent.
- In the [default flips](../studies/default-flips.md) study, each flip had 37 to 241 requests over an individual limit in each seed, and about as many that improved by the same limit.
  A null change of one tick did not spread.

So in a saturated fleet, a candidate that changes dispatch at all can fail the individual limits, and the counts alone do not show if it harms service.
`D` against `LF` has 16 to 33 requests over the wait limit in each pair, while `D` serves 85 to 92 percent of the matched requests faster.

The record does not give a noise floor.
The move of a pod is a physical change, and counts from a change to `LF` are not a null for `D` against `LF`.

## Constraints

- The design says not to extend the observation cap after a failed result, and the same reason applies to the limits.
  So any amendment is for later candidates.
  The screen result of `D` stands under the current limits.
  A new decision on `D` under amended limits is a separate decision of the maintainer, with new runs.
- Tolerances are set before any validation run, and the validation uses seeds that did not set them.
- The amendment applies to each candidate, not to `D` alone.
- The safety, live queue loss, finite recovery, aggregate service, backlog, and capacity gates do not change.

## Shapes

| Shape | Rule | For | Against | Evidence before adoption |
| --- | --- | --- | --- | --- |
| 1. Limits from perturbations | For each pair, compare the count and size of the exceedances with a set of predefined perturbations of the baseline. | Measures how robust the result is to a specified uncertainty of state. | A physical perturbation can itself harm service, so a wide allowance can accept a regression. A true null gives zero change, so it cannot set the allowance. | Bounds on each perturbation, several types and places, both configurations, seeds held out, and harmful controls that the rule rejects. |
| 2. Tail and station limits | Permit a bounded fraction of the matched requests over the individual limit, with a limit on the size of each excess and on the harm at one station or in one time window. | Keeps protection against harm that falls on one place or time, and permits some reordering of requests. | Relaxes the protection of each request explicitly. Stations with few requests need a minimum sample rule. | The full distribution of excess, a breakdown by station and time window, minimum sample rules, and tolerances that the maintainer sets before validation. |
| 3. Paired distributional non-inferiority | Compare the tails of wait and journey across paired runs, with the admission and recovery gates separate. | Tests for a systematic loss of service without the need for the same order of requests. | Can hide the losses of individual requests. Requests in one run are not independent, so the run is the unit of evidence. | Non-inferiority margins, enough independent run pairs, uncertainty by run, checks by subgroup, and a rule for censored requests. |

## Recommendation

Keep the current individual limits for the decision on `D`.
This is also the recommendation of the external review of the noise-floor record.

If the maintainer wants to permit some individual harm in later candidates, shape 2 is the clearest change of policy.
Its parameters are these, each set by the maintainer before validation:

- The maximum fraction of the matched background requests over the individual limit.
- The maximum excess of one request, in seconds or as a fraction of its baseline.
- The maximum fraction over the limit at one station, or in one time window, and the minimum number of requests for that check.
- The treatment of censored requests, which count as over the limit now.

Perturbation studies then validate the chosen values, and they do not waive a failure.

## Evidence for a decision

Before the maintainer opens an amendment, these studies can show if the sensitivity comes from the limits or from a policy:

1. The trace of the first divergence in `LF` after the move: which request changes first, and which pod serves it in each arm.
2. The effect of the dispatch tie breaks and of the 30 s hold for a finishing pod, with the same move.
   If the sensitivity comes from one of these rules, a change to the rule can be better than a change to the limits.
3. The same move at other loads, bands, and queue limits, to show where the sensitivity starts.
4. Harmful controls that the amended rule must reject, for example fewer pods, or a longer dwell at one station.

Each study is a scratch probe of at most 6 arms and 3 seeds.

## Decisions for the maintainer

1. Keep the current individual limits for `D` (recommended), or open an amendment for later candidates.
2. If an amendment opens, choose a shape (2 recommended), and approve the studies above before any tolerance is set.
3. Confirm that the result of `D` under the current limits stands, and that any later decision on `D` needs new runs.
