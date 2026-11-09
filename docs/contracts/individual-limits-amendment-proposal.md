# Individual limits amendment proposal

Status: draft of November 9, 2026.
On November 9, 2026, the maintainer decided to keep the current individual limits for `D`, and to fold the findings of the external review into this draft.
The draft changes no limit, and no run uses the shapes below.

## Problem

The [numerical limits](../design/experimental-adoption.md#numerical-limits) of the adoption gates have two individual limits for each matched request.
The additional pickup wait is at most the larger of 30 s and 5 percent of the baseline wait.
The additional journey is at most the larger of 60 s and 5 percent of the baseline journey.
The design says that these limits "permit small ordering changes while rejecting the hundreds-of-seconds regressions already observed".

In a saturated fleet, a small change can move many requests past these limits:

- In `LF` on the screen workload of `pm-peak`, one move of one idle pod for 200 s puts 364 to 438 requests over the wait limit at a queue limit of 200 ([sensitivity of the individual limits](express-london-adoption-screen-proposal.md#sensitivity-of-the-individual-limits-to-one-pod-move)).
  With no queue limit, it puts 151 to 294 over the limit, and 0 to 16 more do not board, 153 to 310 in total.
  With no hold for a finishing pod, it puts 339 to 405 over the limit.
- Option B of the [positioning pilot](loaded-positioning-proposal.md#pilot-result) puts 355 to 441 requests of `LF` over the wait limit.
  Its mean wait changes by -2.7 to +3.2 percent, so one seed also fails the aggregate limit of 2 percent.
- In the [default flips](../studies/default-flips.md) study, each flip had 37 to 241 requests over an individual limit in each seed, and about as many that improved by the same limit.
  A null change of one tick did not spread.

So in a saturated fleet, a candidate that changes dispatch at all can fail the individual limits, and the count alone does not show if it harms service.
The size of each excess is also necessary.
`D` against `LF` has 16 to 33 requests over the wait limit in each pair.
From the stations other than Bank and Waterloo, `D` gives a shorter pickup wait to 85 to 92 percent of the matched requests, but its requests over the limit have a median excess of 86 to 250 s in each pair, and up to 1,151 s ([study of the failures at the other stations](express-london-adoption-screen-proposal.md#study-of-the-failures-at-the-other-stations)).
These are regressions of the size that the limits are made to reject.

The record does not give a noise floor.
The move of a pod is a physical change, and counts from a change to `LF` are not a null for `D` against `LF`.

## Constraints

- The design says not to extend the observation cap after a failed result, and the same reason applies to the limits.
  So an amendment is only for later candidates.
  It cannot reassess `D` as it is, also with new runs.
  A changed `D`, for example with a new policy, is a new candidate.
- The maintainer sets each tolerance before any validation run, and the validation uses seeds that did not set it.
- The amendment applies to each candidate, not to `D` alone.
- The check stays for each arm pair, and a pooled result cannot hide the failure of one pair.
- The safety, live queue loss, finite recovery, aggregate service, backlog, and capacity gates do not change.

## Counting rule

Each shape counts by offered-event identity in each arm pair, as `screenCompare` does now:

- The eligible events are the background events that the candidate admits in full.
  A hub event is not eligible.
  An event that the candidate refuses or splits counts in the live queue loss gate, not here.
- For the wait, the denominator is the eligible events that boarded in the baseline.
  An event that only the candidate admits is not in the denominator, so it cannot dilute the fraction.
  The numerator is the events of the denominator that boarded in the candidate with an added wait over the limit, and the events that did not board in the candidate.
- For the journey, the rule is the same with finished in place of boarded.
- The report also gives the events that improve by the same limit, and the distribution of the excess.

A count of requests is a measure inside one pair.
Requests in one run are not independent, so the evidence for a rule is the set of run pairs, not the set of requests.

## Shapes

| Shape | Rule | For | Against | Evidence before adoption |
| --- | --- | --- | --- | --- |
| 1. Limits from perturbations | For each pair, compare the count and size of the exceedances with a set of predefined perturbations of the baseline. | Measures how robust the result is to a specified uncertainty of state. | A physical perturbation can itself harm service, so a wide allowance can accept a regression. A true null gives zero change, so it cannot set the allowance. | Bounds on each perturbation, several types and places, both configurations, seeds held out, and harmful controls that the rule rejects. |
| 2. Tail and station limits | Permit a bounded fraction of the eligible requests over the individual limit, with a limit on the size of each excess and on the harm at one station or in one time window. | Keeps protection against harm that falls on one place or time, and permits some reordering of requests. | Relaxes the protection of each request explicitly. Stations with few requests need a minimum sample rule. | The full distribution of excess, a breakdown by station and time window, minimum sample rules, and tolerances that the maintainer sets before validation. |
| 3. Paired distributional non-inferiority | Compare the tails of wait and journey across paired runs, with the admission and recovery gates separate. | Tests for a systematic loss of service without the need for the same order of requests. | Can hide the losses of individual requests. | Non-inferiority margins, enough independent run pairs, uncertainty by run, checks by subgroup, and a rule for censored requests. |
| 4. Balance of harm and benefit | For each pair, compare the requests over the limit with the requests that improve by the same limit, with limits on the size of each excess and by subgroup. | Uses both directions of a reordering, as the default flips study reports them. | Permits the loss of one request when another request gains, so it needs the limits on size and by subgroup. | The paired counts in both directions, the distribution of excess and gain, checks by station and time window, and harmful controls. |

## Recommendation

Keep the current individual limits for the decision on `D`.
The maintainer decided this on November 9, 2026, as both external reviews recommended.

If the maintainer wants to permit some individual harm in later candidates, shape 2 is the clearest change of policy.
The record does not show that it is better than the other shapes.
Its parameters are these, each with a value for the wait and a value for the journey, set by the maintainer before validation:

- The excess: the added time over the current limit, or the added time itself.
- The maximum fraction of the eligible requests over the limit in the pair, by the counting rule above.
- The maximum excess of one request, in seconds or as a fraction of its baseline.
  A baseline of zero keeps the floor of 30 s or 60 s, as now.
- The station of a request, which is its origin station, and the time window, which is a fixed number of minutes from the start of arrivals.
- The maximum fraction over the limit at one station and in one window, and the minimum number of eligible requests for each check.
  Both checks apply.
  Below the minimum, the group has no fraction check, but the maximum excess and the pair fraction still apply to its requests.
- The treatment of a censored request.
  The recommendation is that a censored request fails the pair, as now, because its excess has no measured size.

## Evidence for a decision

The evidence comes in this order:

1. Diagnosis.
   Scratch probes show where the sensitivity comes from, and if a change to a policy is better than a change to the limits.
2. Choice.
   The maintainer chooses a shape and sets each value, before any validation run.
3. Validation.
   The rule runs on seeds that did not set the values, with the full coverage of the adoption gates, and with harmful controls that it must reject, for example fewer pods or a longer dwell at one station.
   Probes of 6 arms and 3 seeds do not replace this coverage.

The diagnosis has these parts:

- Done: the [trace of the first divergence](express-london-adoption-screen-proposal.md#trace-of-the-first-divergence) after the move.
  In each seed of `LF`, the first change is a choice of dispatch.
- Done: the [move with no hold for a finishing pod](express-london-adoption-screen-proposal.md#move-with-no-hold-for-a-finishing-pod).
  The hold is not the source of the sensitivity.
- Open: arms with and without the move, under each variant of the dispatch tie breaks, in both configurations.
  The trace records the dispatch scores, the ties, the holds, the berth claims, and the later assignments up to the first changed outcome.
- Open: how one changed choice grows to about 1,000 changed waits.
- Open: the same move at other loads, bands, and queue limits, to show where the sensitivity starts.

Each diagnosis probe is a scratch probe of at most 6 arms and 3 seeds.

## Decisions for the maintainer

1. Decided on November 9, 2026: keep the current individual limits for `D`.
2. Open an amendment for later candidates, or close this draft.
3. If an amendment opens, choose a shape (2 recommended), and approve the open diagnosis probes before any value is set.

## External review

A blind review by Codex `gpt-6.1-sol` on November 9, 2026, found the recommendation to keep the failure of `D` supported, and shape 2 defensible but not shown better than the others.
Each finding was checked against the draft, the records, and the code, and each was accepted.
The maintainer approved on November 9, 2026, to fold each finding into the draft, and the sections above include them.

1. Blocker: the constraints permit a new decision on `D` under amended limits, with new runs.
   New runs of an unchanged candidate are not prospective.
   The proposed change is that an amendment cannot reassess `D` as it is, also with new runs.
2. Major: shape 2 does not define its denominator.
   `screenCompare` counts each offered background event that the candidate admits, and it skips hub events and the events that the candidate refuses.
   Admission depends on the queue, so a denominator with events that only the candidate admits can dilute the fraction.
   The rule must define the eligible sets, numerators, and denominators for wait and journey by event identity, with censored and refused events.
3. Major: the other parameters of shape 2 are not complete.
   The rule must say if an excess is the added time or the time over the current allowance, how a station and a time window are defined, what happens below the minimum sample, the values for wait and journey, and the case of a zero baseline.
   The check stays for each pair, and small groups must not pool away a failure of a pair.
4. Major: the evidence plan needs a cause, not only a time.
   The second probe gives the offer time of the first request with a changed wait, not the first change of a dispatch choice, and it stops the trace of the pod at its first return to idle.
   The study of the hold changed only `D`.
   Each policy variant needs arms with and without the move in both configurations, with the dispatch scores, ties, holds, berth claims, and later assignments up to the first changed outcome.
5. Major: the problem omits the size of the harm.
   The failures of `D` away from the hubs have a median excess of 86 to 250 s in each pair and up to 1,151 s, and `LF` with option B has up to +3.2 percent mean wait, over the aggregate limit of 2 percent.
   The draft must give these sizes beside the counts of requests that improve.
6. Minor: two numbers need a qualification.
   With no queue limit, the wait gate of `LF` counts 151 to 294 requests over the limit and 0 to 16 that did not board, 153 to 310 in total.
   The 85 to 92 percent of faster requests of `D` is the pickup wait of the matched requests from the stations other than Bank and Waterloo.
7. Minor: the shapes omit a rule that counts the requests that improve beside those that are worse, as the default flips study reported.
   Such a rule permits individual losses by compensation, so it needs limits on size and by subgroup.
   Each shape must also separate the measure for each request from the evidence for each run.
8. Minor: the order of the studies is not consistent.
   One sequence is needed: diagnosis, then the choice of policy and fixed values, then an independent validation.
   Probes of 6 arms and 3 seeds do not replace the full adoption coverage.
