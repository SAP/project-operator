/*
SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and project-operator contributors
SPDX-License-Identifier: Apache-2.0
*/

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	ProjectReconcileErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "project_reconcile_errors_total",
			Help: "Total number of reconciliation errors per project.",
		},
		[]string{"project"},
	)
	ProjectTTLSecondsInitial = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "project_ttl_seconds_initial",
			Help: "Initial TTL per project. Zero means that the project has no TTL, i.e. does not expire.",
		},
		[]string{"project"},
	)
	ProjectTTLSecondsRemaining = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "project_ttl_seconds_remaining",
			Help: "Remaining TTL per project. Zero, and therefore not meaningful, if the project has no TTL set.",
		},
		[]string{"project"},
	)
	ProjectCreationTime = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "project_creation_time",
			Help: "Creation time of the project in epoch seconds. Zero means that the project has not been created yet.",
		},
		[]string{"project"},
	)
	ProjectExpired = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "project_expired",
			Help: "Whether project is expired. Because it has no TTL or TTL is expired. One means true, zero means false.",
		},
		[]string{"project"},
	)
)

func init() {
	metrics.Registry.MustRegister(
		ProjectReconcileErrors,
		ProjectTTLSecondsInitial,
		ProjectTTLSecondsRemaining,
		ProjectCreationTime,
		ProjectExpired,
	)
}
