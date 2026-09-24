package main

import (
	"cmp"
	"slices"
	"sync"
)

// rateGroup holds the arms that differ only in offered rate and seed. It
// applies the -adaptive-limit rule to them.
//
// The rule runs the rates in ascending offered rate order, up to and
// including the first rate at which a seed does not drain. Then it runs
// pastLimit more rates and skips the rest. All seeds of a rate run together.
//
// Let drained be the number of leading rates at which every seed finished
// and drained. The first failing rate is then drained or higher, so every
// rate below drained+1+pastLimit runs, whatever the results of the rates
// that are still running. The group starts only these rates. Thus it never
// starts an arm that the rule skips, and the order in which arms finish
// does not change which arms run. With pastLimit 1, two rates of a group
// can run at the same time.
type rateGroup struct {
	// rates holds the input indices of each rate, lowest offered rate first.
	rates     [][]int
	pastLimit int
	// waiting counts the seeds of each rate that have no result yet.
	waiting []int
	// failed marks each rate at which a seed did not drain.
	failed []bool
	// started counts the rates that have started. drained counts the
	// leading rates at which every seed finished and drained.
	started, drained int
}

func newRateGroup(rates [][]int, pastLimit int) *rateGroup {
	waiting := make([]int, len(rates))
	for rate, indices := range rates {
		waiting[rate] = len(indices)
	}
	return &rateGroup{
		// A limit larger than the number of rates has the same effect, and
		// the smaller value cannot overflow in release.
		rates: rates, pastLimit: min(pastLimit, len(rates)),
		waiting: waiting, failed: make([]bool, len(rates)),
	}
}

// release starts each rate that must run and has not started. It returns
// the input indices of these rates, or nil when no rate can start.
func (group *rateGroup) release() []int {
	end := min(len(group.rates), group.drained+1+group.pastLimit)
	var indices []int
	for ; group.started < end; group.started++ {
		indices = append(indices, group.rates[group.started]...)
	}
	return indices
}

// finish records the result of one seed at the given rate.
func (group *rateGroup) finish(rate int, drained bool) {
	group.waiting[rate]--
	group.failed[rate] = group.failed[rate] || !drained
	for group.drained < len(group.rates) && group.waiting[group.drained] == 0 && !group.failed[group.drained] {
		group.drained++
	}
}

// rateGroupKey holds every dimension of an arm except the offered rate and
// the seed. A new dimension in compare must also go here.
type rateGroupKey struct {
	pattern, profile, band  string
	sharingLimit            int
	routingPolicy, waitRule string
	enabled                 bool
}

func rateGroupKeyOf(input *runInput) rateGroupKey {
	return rateGroupKey{
		pattern: input.pattern, profile: input.profile, band: input.band,
		sharingLimit: input.sharingLimit, routingPolicy: input.routingPolicy,
		waitRule: input.waitRule, enabled: input.enabled,
	}
}

// armPlace gives the group and the rate of one input.
type armPlace struct {
	group, rate int
}

// armScheduler selects the arms that run with -adaptive-limit.
type armScheduler struct {
	groups []*rateGroup
	// places holds the group and the rate of each input.
	places []armPlace
}

func newArmScheduler(inputs []runInput, pastLimit int) *armScheduler {
	scheduler := &armScheduler{places: make([]armPlace, len(inputs))}
	for _, members := range groupMembers(inputs) {
		rates := splitRates(inputs, members)
		for rate, indices := range rates {
			for _, index := range indices {
				scheduler.places[index] = armPlace{group: len(scheduler.groups), rate: rate}
			}
		}
		scheduler.groups = append(scheduler.groups, newRateGroup(rates, pastLimit))
	}
	return scheduler
}

// groupMembers returns the input indices of each group, in the order of
// the first input of each group.
func groupMembers(inputs []runInput) [][]int {
	var groups [][]int
	positions := make(map[rateGroupKey]int)
	for index := range inputs {
		key := rateGroupKeyOf(&inputs[index])
		position, ok := positions[key]
		if !ok {
			position = len(groups)
			positions[key] = position
			groups = append(groups, nil)
		}
		groups[position] = append(groups[position], index)
	}
	return groups
}

// splitRates sorts the members of one group into rates, lowest offered rate
// first. A longer request interval gives a lower offered rate. The seeds of
// a rate keep their input order.
func splitRates(inputs []runInput, members []int) [][]int {
	slices.SortStableFunc(members, func(left, right int) int {
		return cmp.Compare(inputs[right].requestEvery, inputs[left].requestEvery)
	})
	var rates [][]int
	for position, index := range members {
		if position == 0 || inputs[index].requestEvery != inputs[members[position-1]].requestEvery {
			rates = append(rates, nil)
		}
		rates[len(rates)-1] = append(rates[len(rates)-1], index)
	}
	return rates
}

// start returns the input indices of the arms that can run first.
func (scheduler *armScheduler) start() []int {
	var indices []int
	for _, group := range scheduler.groups {
		indices = append(indices, group.release()...)
	}
	return indices
}

// finish records the result of one arm. It returns the input indices of the
// arms that can run now.
func (scheduler *armScheduler) finish(index int, drained bool) []int {
	place := scheduler.places[index]
	group := scheduler.groups[place.group]
	group.finish(place.rate, drained)
	return group.release()
}

type adaptiveRun struct {
	inputs    []runInput
	workers   int
	pastLimit int
	run       func(runInput) (result, error)
}

type armDone struct {
	index  int
	result result
	err    error
}

// runAdaptive runs the arms that armScheduler selects on a pool of workers.
// The pool takes arms in the order that they become ready, so the groups
// progress together. The results keep the input order, without the arms
// that did not run. An arm that fails with an error counts as not drained,
// and runAdaptive returns the error of the first such arm in input order.
func runAdaptive(input adaptiveRun) ([]result, error) {
	scheduler := newArmScheduler(input.inputs, input.pastLimit)
	jobs := make(chan int, len(input.inputs))
	done := make(chan armDone)
	var workers sync.WaitGroup
	for range min(input.workers, len(input.inputs)) {
		workers.Go(func() {
			for index := range jobs {
				outcome, err := input.run(input.inputs[index])
				done <- armDone{index: index, result: outcome, err: err}
			}
		})
	}
	finished := make([]*armDone, len(input.inputs))
	running := queueArms(jobs, scheduler.start())
	for running > 0 {
		arm := <-done
		running--
		finished[arm.index] = &arm
		running += queueArms(jobs, scheduler.finish(arm.index, arm.err == nil && arm.result.Drained))
	}
	close(jobs)
	workers.Wait()
	return finishedResults(finished)
}

// queueArms sends indices to jobs and returns their number. The jobs buffer
// holds every input, so the send does not block.
func queueArms(jobs chan<- int, indices []int) int {
	for _, index := range indices {
		jobs <- index
	}
	return len(indices)
}

// finishedResults returns the results of the arms that ran, in input order.
func finishedResults(finished []*armDone) ([]result, error) {
	results := make([]result, 0, len(finished))
	for _, arm := range finished {
		if arm == nil {
			continue
		}
		if arm.err != nil {
			return nil, arm.err
		}
		results = append(results, arm.result)
	}
	return results, nil
}
