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
package utils

import (
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Clones the given map and returns a new map with the given key and value added.
// Returns the given map, if labelKey is empty.
func CloneAndAddLabel(labels map[string]string, labelKey, labelValue string) map[string]string {
	newLabels := map[string]string{}
	for key, value := range labels {
		newLabels[key] = value
	}
	if labelKey != "" {
		newLabels[labelKey] = labelValue
	}
	return newLabels
}

// CloneAndRemoveLabel clones the given map and returns a new map with the given key removed.
// Returns the given map, if labelKey is empty.
func CloneAndRemoveLabel(labels map[string]string, labelKey string) map[string]string {
	newLabels := map[string]string{}
	for key, value := range labels {
		newLabels[key] = value
	}
	if labelKey != "" {
		delete(newLabels, labelKey)
	}
	return newLabels
}

// Clones the given selector and returns a new selector with the given key and value added.
// Returns the given selector, if labelKey is empty.
func CloneSelectorAndAddLabel(selector *metav1.LabelSelector, labelKey, labelValue string) *metav1.LabelSelector {
	if labelKey == "" {
		// Don't need to add a label.
		return selector
	}

	// Clone.
	newSelector := new(metav1.LabelSelector)

	// TODO(madhusudancs): Check if you can use deepCopy_extensions_LabelSelector here.
	newSelector.MatchLabels = make(map[string]string)
	if selector.MatchLabels != nil {
		for key, val := range selector.MatchLabels {
			newSelector.MatchLabels[key] = val
		}
	}
	newSelector.MatchLabels[labelKey] = labelValue

	if selector.MatchExpressions != nil {
		newMExps := make([]metav1.LabelSelectorRequirement, len(selector.MatchExpressions))
		for i, me := range selector.MatchExpressions {
			newMExps[i].Key = me.Key
			newMExps[i].Operator = me.Operator
			if me.Values != nil {
				newMExps[i].Values = make([]string, len(me.Values))
				copy(newMExps[i].Values, me.Values)
			} else {
				newMExps[i].Values = nil
			}
		}
		newSelector.MatchExpressions = newMExps
	} else {
		newSelector.MatchExpressions = nil
	}

	return newSelector
}

func CloneNodeAffinityAndAdd(nodeAffinity *corev1.NodeAffinity, matchLabels map[string]string) *corev1.NodeAffinity {
	if nodeAffinity == nil {
		nodeAffinity = &corev1.NodeAffinity{}
	} else {
		nodeAffinity = nodeAffinity.DeepCopy()
	}
	if len(matchLabels) == 0 {
		return nodeAffinity
	}
	if nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution = &corev1.NodeSelector{}
	}
	nodeSelectorRequirements := make([]corev1.NodeSelectorRequirement, 0)
	for k, v := range matchLabels {
		nodeSelectorRequirements = append(nodeSelectorRequirements, corev1.NodeSelectorRequirement{
			Key:      k,
			Operator: corev1.NodeSelectorOpIn,
			Values:   []string{v},
		})
	}
	// issue #15 多个label顺序不一致导致导致比对修改失败，对nodeSelectorRequirements进行排序
	sort.Slice(nodeSelectorRequirements, func(i, j int) bool {
		return nodeSelectorRequirements[i].Key < nodeSelectorRequirements[j].Key
	})
	nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms =
		append(nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms, corev1.NodeSelectorTerm{
			MatchExpressions: nodeSelectorRequirements,
		})
	return nodeAffinity
}
