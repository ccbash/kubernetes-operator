// SPDX-License-Identifier: BSD-3-Clause

package v1alpha1

const ReadyCondition = "Ready"

const (
	ReconciledReason = "Reconciled"
	DependencyReason = "Dependency"
	// InvalidSpecReason marks a terminal configuration error in the object's
	// spec — reconciling again won't fix it until the spec (or a referenced
	// object) is corrected.
	InvalidSpecReason = "InvalidSpec"
)
