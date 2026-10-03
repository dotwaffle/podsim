package sim

import (
	"errors"
	"testing"
)

func TestVehicleClassRegistry(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		class VehicleClass
		want  VehicleClassSpec
		ok    bool
	}{
		{"", VehicleClassSpec{LegacyClass, 8, 1, 4, true}, true},
		{LegacyClass, VehicleClassSpec{LegacyClass, 8, 1, 4, true}, true},
		{CompactClass, VehicleClassSpec{CompactClass, 4, 4, 4, true}, true},
		{GroupClass, VehicleClassSpec{GroupClass, 8, 8, 6, true}, true},
		{ExpressClass, VehicleClassSpec{ExpressClass, 20, 8, 0, false}, true},
		{"large", VehicleClassSpec{}, false},
		{"Legacy", VehicleClassSpec{}, false},
		{" ", VehicleClassSpec{}, false},
	} {
		t.Run(string(test.class), func(t *testing.T) {
			t.Parallel()
			got, ok := LookupVehicleClass(test.class)
			if ok != test.ok || got != test.want {
				t.Fatalf("LookupVehicleClass(%q) = %+v, %t, want %+v, %t", test.class, got, ok, test.want, test.ok)
			}
			got.Seats, got.MaxNewPartySize, got.BodyLengthMeters, got.PhysicalSupported = 99, 99, 99, !got.PhysicalSupported
			again, ok := LookupVehicleClass(test.class)
			if ok != test.ok || again != test.want {
				t.Fatalf("caller changed registry for %q: %+v", test.class, again)
			}
		})
	}
}

func TestVehicleClassPhysicalValidation(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		class VehicleClass
		want  error
	}{
		{"", nil},
		{LegacyClass, nil},
		{CompactClass, nil},
		{GroupClass, nil},
		{ExpressClass, ErrUnsupportedVehicleProfile},
		{"large", ErrUnknownVehicleClass},
	} {
		t.Run(string(test.class), func(t *testing.T) {
			t.Parallel()
			if err := ValidateVehicleClassProfile(test.class); !errors.Is(err, test.want) {
				t.Fatalf("ValidateVehicleClassProfile(%q) = %v, want %v", test.class, err, test.want)
			}
		})
	}
}
