package reschedule

import (
	"errors"
	appsv1 "k8s.io/api/apps/v1"
)

var (
	ErrNotSynced                      error = RetryError{error: errors.New("global cache is not synced"), After: 3}
	ErrPendingNotTimeout              error = RetryError{error: errors.New("waiting pending pod timeout or ready"), After: 3}
	ErrWithoutAvailableRegions              = errors.New("without available region for reschedule")
	ErrAllRegionWithoutEnoughResource       = errors.New("all regions don't have enough resource")
)

const (
	DeploymentReschedule appsv1.DeploymentConditionType = "Reschedule"
)

type RescheduleType int

const (
	RescheduleNever RescheduleType = iota
	RescheduleUnknown
	RescheduleForFailedRegion
	RescheduleForResourceInsufficient
	RescheduleForFailedRegionAndResourceInsufficient
)

type RetryError struct {
	error
	After int32
}
