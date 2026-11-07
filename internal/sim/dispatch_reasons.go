package sim

// The dispatch reasons of a waiting trip. A reason that names a pod puts
// the pod ID between a prefix and a suffix.
const (
	reasonAvailablePod      = "Waiting for an available pod"
	reasonCertifiedVehicle  = "Waiting for a certified vehicle that fits this party and route"
	reasonPickupAccess      = "Waiting for pickup access"
	reasonDestinationAccess = "Waiting for destination access"
	reasonPodPrefix         = "Pod "
	reasonTravelingSuffix   = " traveling to pickup"
	reasonTrafficSuffix     = " waiting in traffic"
	reasonFinishingPrefix   = "Waiting for pod "
	reasonFinishingSuffix   = " to finish"
)

// MaxExpressDispatchReasonBytes is the length of the longest dispatch
// reason under the Express order contract, in which a pod ID has at most
// expressMaxIDBytes bytes. A restore of an Express state refuses a longer
// saved reason, so that the stream of the state stays inside its byte cap.
const MaxExpressDispatchReasonBytes = max(
	len(reasonAvailablePod), len(reasonCertifiedVehicle), len(reasonPickupAccess), len(reasonDestinationAccess),
	len(reasonPodPrefix)+expressMaxIDBytes+max(len(reasonTravelingSuffix), len(reasonTrafficSuffix)),
	len(reasonFinishingPrefix)+expressMaxIDBytes+len(reasonFinishingSuffix),
)

// finishingPodReason is the reason of a trip that waits for pod podID to
// finish.
func finishingPodReason(podID string) string {
	return reasonFinishingPrefix + podID + reasonFinishingSuffix
}
