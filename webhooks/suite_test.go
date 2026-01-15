/*
SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and project-operator contributors
SPDX-License-Identifier: Apache-2.0
*/

package webhooks_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	corev1alpha1 "github.com/sap/project-operator/api/v1alpha1"
	"github.com/sap/project-operator/webhooks"
)

const (
	namespacePrefix = "project-"
	someUser        = "someUser"
	powerUser       = "powerUser"
	user1           = "user1"
	user2           = "user2"
	someGroup       = "someGroup"
	group1          = "group1"
	group2          = "group2"
)

func TestOperator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Operator")
}

var testEnv *envtest.Environment
var cfg *rest.Config
var cli client.Client
var saCli client.Client
var someCli client.Client
var powerCli client.Client
var user1Cli client.Client
var user2Cli client.Client
var group1Cli client.Client
var group2Cli client.Client
var ctx context.Context
var cancel context.CancelFunc
var threads sync.WaitGroup
var tmpdir string

var _ = BeforeSuite(func() {
	var err error

	By("initializing")
	log.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	ctx, cancel = context.WithCancel(context.TODO())
	tmpdir, err = os.MkdirTemp("", "")
	Expect(err).NotTo(HaveOccurred())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{"../crds"},
		WebhookInstallOptions: envtest.WebhookInstallOptions{
			ValidatingWebhooks: []*admissionv1.ValidatingWebhookConfiguration{
				buildValidatingWebhookConfiguration(),
			},
		},
	}
	testEnv.ControlPlane.GetAPIServer().Configure().
		Disable("disable-admission-plugins").
		Append("api-audiences", "kubernetes")
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())
	webhookInstallOptions := &testEnv.WebhookInstallOptions

	err = clientcmd.WriteToFile(*kubeConfigFromRestConfig(cfg), fmt.Sprintf("%s/kubeconfig", tmpdir))
	Expect(err).NotTo(HaveOccurred())
	fmt.Printf("A temporary kubeconfig for the envtest environment can be found here: %s/kubeconfig\n", tmpdir)

	By("populating scheme")
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(corev1alpha1.AddToScheme(scheme))

	By("initializing client")
	cli, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())

	By("creating manager")
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme,
		Client: client.Options{
			Cache: &client.CacheOptions{
				DisableFor: []client.Object{&corev1alpha1.Project{}},
			},
		},
		WebhookServer: webhook.NewServer(webhook.Options{
			Host:    webhookInstallOptions.LocalServingHost,
			Port:    webhookInstallOptions.LocalServingPort,
			CertDir: webhookInstallOptions.LocalServingCertDir,
		}),
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
		HealthProbeBindAddress: "0",
	})
	Expect(err).NotTo(HaveOccurred())

	err = webhooks.NewProjectWebhook(
		mgr.GetClient(),
		webhooks.ProjectReconcilerOptions{
			NamespacePrefix: namespacePrefix,
		},
	).SetupWebhookWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	By("starting manager")
	threads.Add(1)
	go func() {
		defer threads.Done()
		defer GinkgoRecover()
		err := mgr.Start(ctx)
		Expect(err).NotTo(HaveOccurred())
	}()

	By("waiting for operator to become ready")
	Eventually(func() error { return mgr.GetWebhookServer().StartedChecker()(nil) }, "10s", "100ms").Should(Succeed())

	By("authorizing service accounts, users and groups")
	projetAdminServiceAccount := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "project-admin",
		},
	}
	err = cli.Create(ctx, projetAdminServiceAccount)
	Expect(err).NotTo(HaveOccurred())

	projectAdminClusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: "project-admin",
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{corev1alpha1.GroupVersion.Group},
				Resources: []string{"projects"},
				Verbs:     []string{"*"},
			},
		},
	}
	err = cli.Create(ctx, projectAdminClusterRole)
	Expect(err).NotTo(HaveOccurred())

	namespaceAdminClusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: "namespace-admin",
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"namespaces"},
				Verbs:     []string{"*"},
			},
		},
	}
	err = cli.Create(ctx, namespaceAdminClusterRole)
	Expect(err).NotTo(HaveOccurred())

	projectAdminClusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "project-admin",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Namespace: "default",
				Name:      "project-admin",
			},
			{
				APIGroup: rbacv1.GroupName,
				Kind:     rbacv1.UserKind,
				Name:     someUser,
			},
			{
				APIGroup: rbacv1.GroupName,
				Kind:     rbacv1.UserKind,
				Name:     powerUser,
			},
			{
				APIGroup: rbacv1.GroupName,
				Kind:     rbacv1.UserKind,
				Name:     user1,
			},
			{
				APIGroup: rbacv1.GroupName,
				Kind:     rbacv1.UserKind,
				Name:     user2,
			},
			{
				APIGroup: rbacv1.GroupName,
				Kind:     rbacv1.GroupKind,
				Name:     someGroup,
			},
			{
				APIGroup: rbacv1.GroupName,
				Kind:     rbacv1.GroupKind,
				Name:     group1,
			},
			{
				APIGroup: rbacv1.GroupName,
				Kind:     rbacv1.GroupKind,
				Name:     group2,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     "project-admin",
		},
	}
	err = cli.Create(ctx, projectAdminClusterRoleBinding)
	Expect(err).NotTo(HaveOccurred())

	namespaceAdminClusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "namespace-admin",
		},
		Subjects: []rbacv1.Subject{
			{
				APIGroup: rbacv1.GroupName,
				Kind:     rbacv1.UserKind,
				Name:     powerUser,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     "namespace-admin",
		},
	}
	err = cli.Create(ctx, namespaceAdminClusterRoleBinding)
	Expect(err).NotTo(HaveOccurred())

	By("initializing service account clients")
	serviceAccountClient := func(namespace string, name string, audiences []string) (client.Client, error) {
		serviceAccount := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      name,
			},
		}
		tokenRequest := &authenticationv1.TokenRequest{
			Spec: authenticationv1.TokenRequestSpec{
				Audiences: audiences,
			},
		}
		err := cli.SubResource("token").Create(ctx, serviceAccount, tokenRequest)
		if err != nil {
			return nil, err
		}
		token := tokenRequest.Status.Token
		cfg := rest.AnonymousClientConfig(cfg)
		cfg.BearerToken = token
		return client.New(cfg, client.Options{Scheme: scheme})
	}
	saCli, err = serviceAccountClient("default", "project-admin", []string{"kubernetes"})
	Expect(err).NotTo(HaveOccurred())

	By("initializing impersonated clients")
	impersonatedClient := func(user string, groups []string) (client.Client, error) {
		cfg := rest.CopyConfig(cfg)
		cfg.Impersonate.UserName = user
		cfg.Impersonate.Groups = groups
		return client.New(cfg, client.Options{Scheme: scheme})
	}
	someCli, err = impersonatedClient(someUser, []string{someGroup})
	Expect(err).NotTo(HaveOccurred())
	validateClient(someCli, []client.ObjectList{&corev1alpha1.ProjectList{}}, []client.ObjectList{&corev1.ConfigMapList{}, &corev1.NamespaceList{}})
	powerCli, err = impersonatedClient(powerUser, []string{someGroup})
	Expect(err).NotTo(HaveOccurred())
	validateClient(powerCli, []client.ObjectList{&corev1alpha1.ProjectList{}, &corev1.NamespaceList{}}, []client.ObjectList{&corev1.ConfigMapList{}})
	user1Cli, err = impersonatedClient(user1, []string{someGroup})
	Expect(err).NotTo(HaveOccurred())
	validateClient(user1Cli, []client.ObjectList{&corev1alpha1.ProjectList{}}, []client.ObjectList{&corev1.ConfigMapList{}, &corev1.NamespaceList{}})
	user2Cli, err = impersonatedClient(user2, []string{someGroup})
	Expect(err).NotTo(HaveOccurred())
	validateClient(user2Cli, []client.ObjectList{&corev1alpha1.ProjectList{}}, []client.ObjectList{&corev1.ConfigMapList{}, &corev1.NamespaceList{}})
	group1Cli, err = impersonatedClient(someUser, []string{group1})
	Expect(err).NotTo(HaveOccurred())
	validateClient(group1Cli, []client.ObjectList{&corev1alpha1.ProjectList{}}, []client.ObjectList{&corev1.ConfigMapList{}, &corev1.NamespaceList{}})
	group2Cli, err = impersonatedClient(someUser, []string{group2})
	Expect(err).NotTo(HaveOccurred())
	validateClient(group2Cli, []client.ObjectList{&corev1alpha1.ProjectList{}}, []client.ObjectList{&corev1.ConfigMapList{}, &corev1.NamespaceList{}})
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	cancel()
	threads.Wait()
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
	err = os.RemoveAll(tmpdir)
	Expect(err).NotTo(HaveOccurred())
})

var _ = Describe("Additional authorization checks when updating/deleting projects", func() {
	var project *corev1alpha1.Project

	BeforeEach(func() {
		project = &corev1alpha1.Project{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-",
			},
			Spec: corev1alpha1.ProjectSpec{
				AdminUsers:   []string{user1},
				AdminGroups:  []string{group1},
				ViewerUsers:  []string{user2},
				ViewerGroups: []string{group2},
			},
		}
		err := cli.Create(ctx, project)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should allow update/delete for service accounts", func() {
		project.Spec.ViewerUsers = append(project.Spec.ViewerUsers, "john-doe")
		err := saCli.Update(ctx, project)
		Expect(err).NotTo(HaveOccurred())
		err = saCli.Delete(ctx, project)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should allow update/delete for admin users", func() {
		project.Spec.ViewerUsers = append(project.Spec.ViewerUsers, "john-doe")
		err := user1Cli.Update(ctx, project)
		Expect(err).NotTo(HaveOccurred())
		err = user1Cli.Delete(ctx, project)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should allow update/delete for admin groups", func() {
		project.Spec.ViewerUsers = append(project.Spec.ViewerUsers, "john-doe")
		err := group1Cli.Update(ctx, project)
		Expect(err).NotTo(HaveOccurred())
		err = group1Cli.Delete(ctx, project)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should allow update/delete for power users (having namespace admin rights)", func() {
		project.Spec.ViewerUsers = append(project.Spec.ViewerUsers, "john-doe")
		err := powerCli.Update(ctx, project)
		Expect(err).NotTo(HaveOccurred())
		err = powerCli.Delete(ctx, project)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should disallow update/delete for viewer users", func() {
		project.Spec.ViewerUsers = append(project.Spec.ViewerUsers, "john-doe")
		err := user2Cli.Update(ctx, project)
		Expect(ignoreForbidden(err)).NotTo(HaveOccurred())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("validate-project.test.local"))
		err = user2Cli.Delete(ctx, project)
		Expect(ignoreForbidden(err)).NotTo(HaveOccurred())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("validate-project.test.local"))
	})

	It("should disallow update/delete for viewer groups", func() {
		project.Spec.ViewerUsers = append(project.Spec.ViewerUsers, "john-doe")
		err := group2Cli.Update(ctx, project)
		Expect(ignoreForbidden(err)).NotTo(HaveOccurred())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("validate-project.test.local"))
		err = group2Cli.Delete(ctx, project)
		Expect(ignoreForbidden(err)).NotTo(HaveOccurred())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("validate-project.test.local"))
	})

	It("should disallow update/delete for other users", func() {
		project.Spec.ViewerUsers = append(project.Spec.ViewerUsers, "john-doe")
		err := someCli.Update(ctx, project)
		Expect(ignoreForbidden(err)).NotTo(HaveOccurred())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("validate-project.test.local"))
		err = someCli.Delete(ctx, project)
		Expect(ignoreForbidden(err)).NotTo(HaveOccurred())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("validate-project.test.local"))
	})
})

func validateClient(cli client.Client, accpetedObjects []client.ObjectList, rejectedObjects []client.ObjectList) {
	// TODO: if this is called too fast after setting up rbac objects, things may fail (probably, because kubernetes needs a little while until rbac changes are effective ..)
	for _, objectList := range accpetedObjects {
		err := cli.List(ctx, objectList)
		Expect(err).NotTo(HaveOccurred())
	}
	for _, objectList := range rejectedObjects {
		err := cli.List(ctx, objectList)
		Expect(ignoreForbidden(err)).NotTo(HaveOccurred())
		Expect(err).To(HaveOccurred())
	}
}

// assemble validatingwebhookconfiguration descriptor
func buildValidatingWebhookConfiguration() *admissionv1.ValidatingWebhookConfiguration {
	return &admissionv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "validate-project",
		},
		Webhooks: []admissionv1.ValidatingWebhook{{
			Name:                    "validate-project.test.local",
			AdmissionReviewVersions: []string{"v1"},
			ClientConfig: admissionv1.WebhookClientConfig{
				Service: &admissionv1.ServiceReference{
					Path: &[]string{fmt.Sprintf("/validate-%s-%s-%s", strings.ReplaceAll(corev1alpha1.GroupVersion.Group, ".", "-"), corev1alpha1.GroupVersion.Version, "project")}[0],
				},
			},
			Rules: []admissionv1.RuleWithOperations{{
				Operations: []admissionv1.OperationType{
					admissionv1.Create,
					admissionv1.Update,
					admissionv1.Delete,
				},
				Rule: admissionv1.Rule{
					APIGroups:   []string{corev1alpha1.GroupVersion.Group},
					APIVersions: []string{corev1alpha1.GroupVersion.Version},
					Resources:   []string{"projects"},
				},
			}},
			SideEffects: &[]admissionv1.SideEffectClass{admissionv1.SideEffectClassNone}[0],
		}},
	}
}

// convert rest.Config into kubeconfig
func kubeConfigFromRestConfig(restConfig *rest.Config) *clientcmdapi.Config {
	apiConfig := clientcmdapi.NewConfig()

	apiConfig.Clusters["envtest"] = clientcmdapi.NewCluster()
	cluster := apiConfig.Clusters["envtest"]
	cluster.Server = restConfig.Host
	cluster.CertificateAuthorityData = restConfig.CAData

	apiConfig.AuthInfos["envtest"] = clientcmdapi.NewAuthInfo()
	authInfo := apiConfig.AuthInfos["envtest"]
	authInfo.ClientKeyData = restConfig.KeyData
	authInfo.ClientCertificateData = restConfig.CertData

	apiConfig.Contexts["envtest"] = clientcmdapi.NewContext()
	context := apiConfig.Contexts["envtest"]
	context.Cluster = "envtest"
	context.AuthInfo = "envtest"

	apiConfig.CurrentContext = "envtest"

	return apiConfig
}

// ignore 403 errors (return nil if passed error is a 403 error, otherwise return it unchanged)
func ignoreForbidden(err error) error {
	if apierrors.IsForbidden(err) {
		return nil
	}
	return err
}
