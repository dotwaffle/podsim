package sim

import (
	"maps"
	"math"
	"testing"
)

func TestCouplingNativeMileageClock(t *testing.T) {
	t.Parallel()
	for _, occupied := range []bool{false, true} {
		name := "empty"
		if occupied {
			name = "occupied"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			input, _ := remainingTestContext(t, occupied)
			var bases [2]float64
			for i := range input.Members {
				m := &input.Members[i]
				bases[i] = m.Vehicle.RiddenMeters
				if occupied {
					// Large cumulative histories expose intermediate snapshot rounding.
					bases[i] = math.Ldexp(1, 52) + float64(i)*100
				}
				m.nativeRiddenBase = new(bases[i])
				m.Vehicle.RiddenMeters = m.cabinMeters(m.Distance)
			}
			plan, err := planCouplingReservation(input)
			if err != nil {
				t.Fatal(err)
			}
			c, err := prepareCouplingMotionContext(couplingMotionContextInput{Reservation: plan, Current: input, GroupID: "clock-pair"})
			if err != nil {
				t.Fatal(err)
			}
			contexts := []*couplingMotionContext{c}
			if occupied {
				state, err := c.stateAt(1)
				if err != nil {
					t.Fatal(err)
				}
				cold := remainingTestSnapshot(t, input, c, state)
				cold.GroupID = c.owner.id
				rebased, _, err := prepareRemainingCouplingMotion(cold)
				if err != nil {
					t.Fatal(err)
				}
				contexts = append(contexts, rebased)
			}
			for i := range input.Members {
				if c.reservation.members[i].nativeRiddenBase == input.Members[i].nativeRiddenBase {
					t.Fatal("native history baseline shares mutable input storage")
				}
				*input.Members[i].nativeRiddenBase++
			}
			orderDifferences := 0
			for _, context := range contexts {
				for elapsed := uint64(0); elapsed <= context.ticks; elapsed++ {
					state, err := context.stateAt(elapsed)
					if err != nil {
						t.Fatal(err)
					}
					members := context.membersAt(state)
					for i, member := range members {
						want := bases[i]
						if occupied {
							want += state.Distances[i]
							original := context.reservation.members[i]
							if original.Vehicle.RiddenMeters+(state.Distances[i]-original.Distance) != want {
								orderDifferences++
							}
						} else if len(member.Boardings) == 0 {
							want = 0
						}
						if math.Float64bits(member.RiddenMeters) != math.Float64bits(want) {
							t.Fatalf("tick %d member %d changed native mileage bits: %.17g != %.17g", elapsed, i, member.RiddenMeters, want)
						}
					}
				}
			}
			if occupied && orderDifferences == 0 {
				t.Fatal("native clock control did not distinguish snapshot-plus-delta arithmetic")
			}
			t.Logf("native clock ticks=%d, differing snapshot arithmetic=%d", c.ticks+1, orderDifferences)
		})
	}
}

func TestCouplingNativeMileageBaselineRejects(t *testing.T) {
	t.Parallel()
	for _, baseline := range []float64{1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		input := couplingMotionFixture(t, true, false)
		input.Members[0].nativeRiddenBase = new(baseline)
		before := maps.Clone(input.Owners)
		if _, err := planCouplingReservation(input); err == nil || !maps.Equal(before, input.Owners) {
			t.Fatal("invalid native history baseline changed owners or admitted a pair", err)
		}
	}
}
