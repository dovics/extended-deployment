/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
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
