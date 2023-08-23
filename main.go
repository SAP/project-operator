/*
SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and project-operator contributors
SPDX-License-Identifier: Apache-2.0
*/

package main

import (
	"flag"
	"net"
	"os"
	"strconv"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	corev1alpha1 "github.com/sap/project-operator/api/v1alpha1"
	"github.com/sap/project-operator/controllers"
	"github.com/sap/project-operator/webhooks"
	// +kubebuilder:scaffold:imports
)

const (
	LeaderElectionID = "project-operator.cs.sap.com"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(corev1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var probeAddr string
	var webhookAddr string
	var webhookCertDir string
	var enableLeaderElection bool
	var namespacePrefix string
	var adminClusterRole string
	var viewerClusterRole string
	var enableClusterView bool
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.StringVar(&webhookAddr, "webhook-bind-address", ":9443", "The address the webhook endpoint binds to.")
	flag.StringVar(&webhookCertDir, "webhook-tls-directory", "", "The directory containing tls server key and certificate, as tls.key and tls.crt; defaults to $TMPDIR/k8s-webhook-server/serving-certs.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false, "Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&namespacePrefix, "namespace-prefix", "project-", "Prefix of generated namespaces.")
	flag.StringVar(&adminClusterRole, "admin-cluster-role", "cluster-admin", "Cluster role that admin users/groups will be granted on project namespace level.")
	flag.StringVar(&viewerClusterRole, "viewer-cluster-role", "view", "Cluster role that viewer users/groups will be granted on project namespace level.")
	flag.BoolVar(&enableClusterView, "enable-cluster-view", false, "Automatically grant cluster view authorizations to all referenced users/groups.")

	opts := zap.Options{
		Development: false,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	if namespacePrefix == "" {
		setupLog.Error(nil, "flag missing or empty: --namespace-prefix")
		os.Exit(1)
	}
	if adminClusterRole == "" {
		setupLog.Error(nil, "flag missing or empty: --admin-cluster-role")
		os.Exit(1)
	}
	if viewerClusterRole == "" {
		setupLog.Error(nil, "flag missing or empty: --viewer-cluster-role")
		os.Exit(1)
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	webhookHost, webhookPort, err := parseAddress(webhookAddr)
	if err != nil {
		setupLog.Error(err, "unable to parse webhook bind address")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Client: client.Options{
			Cache: &client.CacheOptions{
				DisableFor: []client.Object{&corev1alpha1.Project{}},
			},
		},
		LeaderElection:                enableLeaderElection,
		LeaderElectionID:              LeaderElectionID,
		LeaderElectionReleaseOnCancel: true,
		WebhookServer: webhook.NewServer(webhook.Options{
			Host:    webhookHost,
			Port:    webhookPort,
			CertDir: webhookCertDir,
		}),
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: probeAddr,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = controllers.NewProjectReconciler(
		mgr.GetClient(),
		mgr.GetScheme(),
		controllers.ProjectReconcilerOptions{
			NamespacePrefix:   namespacePrefix,
			AdminClusterRole:  adminClusterRole,
			ViewerClusterRole: viewerClusterRole,
			EnableClusterView: enableClusterView,
		},
	).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Namespace")
		os.Exit(1)
	}

	if err = webhooks.NewProjectWebhook(
		mgr.GetClient(),
		webhooks.ProjectReconcilerOptions{
			NamespacePrefix: namespacePrefix,
		},
	).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "Project")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func parseAddress(address string) (string, int, error) {
	host, p, err := net.SplitHostPort(address)
	if err != nil {
		return "", -1, err
	}
	port, err := strconv.Atoi(p)
	if err != nil {
		return "", -1, err
	}
	return host, port, nil
}
