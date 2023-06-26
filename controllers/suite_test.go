/*
SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and project-operator contributors
SPDX-License-Identifier: Apache-2.0
*/

package controllers_test

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sap/go-generics/sets"
	"github.com/sap/go-generics/slices"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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

	corev1alpha1 "github.com/sap/project-operator/api/v1alpha1"
	"github.com/sap/project-operator/controllers"
	"github.com/sap/project-operator/webhooks"
)

const (
	namespacePrefix   = "project-"
	adminClusterRole  = "cluster-admin"
	viewerClusterRole = "view"
	enableClusterView = true
)

func TestOperator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Operator")
}

var testEnv *envtest.Environment
var cfg *rest.Config
var cli client.Client
var ctx context.Context
var cancel context.CancelFunc
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
		Scheme:                 scheme,
		MetricsBindAddress:     "0",
		HealthProbeBindAddress: "0",
		Host:                   webhookInstallOptions.LocalServingHost,
		Port:                   webhookInstallOptions.LocalServingPort,
		CertDir:                webhookInstallOptions.LocalServingCertDir,
		ClientDisableCacheFor:  []client.Object{&corev1alpha1.Project{}}})
	Expect(err).NotTo(HaveOccurred())

	err = controllers.NewProjectReconciler(
		mgr.GetClient(),
		mgr.GetScheme(),
		controllers.ProjectReconcilerOptions{
			NamespacePrefix:   namespacePrefix,
			AdminClusterRole:  adminClusterRole,
			ViewerClusterRole: viewerClusterRole,
			EnableClusterView: enableClusterView,
		},
	).SetupWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	err = webhooks.NewProjectWebhook(
		mgr.GetClient(),
		webhooks.ProjectReconcilerOptions{
			NamespacePrefix: namespacePrefix,
		},
	).SetupWebhookWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	By("starting dummy controller-manager")
	go func() {
		defer GinkgoRecover()
		// since there is no controller-manager in envtest, we have to remove the 'kubernetes' finalizer from namespaces when being deleted
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
				namespaceList := &corev1.NamespaceList{}
				err := cli.List(ctx, namespaceList)
				Expect(err).NotTo(HaveOccurred())
				for _, namespace := range namespaceList.Items {
					if !namespace.DeletionTimestamp.IsZero() && slices.Contains(namespace.Spec.Finalizers, "kubernetes") {
						namespace.Spec.Finalizers = slices.Remove(namespace.Spec.Finalizers, "kubernetes")
						err := cli.SubResource("finalize").Update(ctx, &namespace)
						Expect(err).NotTo(HaveOccurred())
					}
					Expect(err).NotTo(HaveOccurred())
				}
			}
		}
	}()

	By("starting manager")
	go func() {
		defer GinkgoRecover()
		err := mgr.Start(ctx)
		Expect(err).NotTo(HaveOccurred())
	}()

	By("waiting for operator to become ready")
	Eventually(func() error { return mgr.GetWebhookServer().StartedChecker()(nil) }, "10s", "100ms").Should(Succeed())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	cancel()
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
	err = os.RemoveAll(tmpdir)
	Expect(err).NotTo(HaveOccurred())
})

var _ = Describe("Create projects", func() {
	It("should create the project namespace as specified (with fixed name, without any spec)", func() {
		project := &corev1alpha1.Project{
			ObjectMeta: metav1.ObjectMeta{
				Name: randomString(8),
			},
		}

		err := cli.Create(ctx, project)
		Expect(err).NotTo(HaveOccurred())
		waitForProjectReady(project)

		namespace := namespacePrefix + project.Name
		ensureNamespace(namespace, nil, nil)
		ensureNoRoleBinding(namespace, "project:admin")
		ensureNoRoleBinding(namespace, "project:viewer")
		ensureNoClusterRoleBinding(fmt.Sprintf("project:%s:view", project.Name))
	})

	It("should create the project namespace as specified (with generated name, with full spec)", func() {
		adminUsers := []string{"admin-user-1", "admin-user-2"}
		adminGroups := []string{"admin-group-1"}
		viewerUsers := []string{"viewer-user-1"}
		viewerGroups := []string{"viewer-group-1", "viewer-group-2"}
		labels := map[string]string{"label-key-1": "label-value-1", "label-key-2": "label-value-2"}
		annotations := map[string]string{"annotation-key-1": "annotation-value-1", "annotation-key-2": "annotation-value-2"}

		project := &corev1alpha1.Project{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-",
			},
			Spec: corev1alpha1.ProjectSpec{
				AdminUsers:   adminUsers,
				AdminGroups:  adminGroups,
				ViewerUsers:  viewerUsers,
				ViewerGroups: viewerGroups,
				Labels:       labels,
				Annotations:  annotations,
			},
		}

		err := cli.Create(ctx, project)
		Expect(err).NotTo(HaveOccurred())
		waitForProjectReady(project)

		namespace := namespacePrefix + project.Name
		ensureNamespace(namespace, labels, annotations)
		ensureRoleBinding(namespace, "project:admin", adminUsers, adminGroups, adminClusterRole)
		ensureRoleBinding(namespace, "project:viewer", viewerUsers, viewerGroups, viewerClusterRole)
		ensureClusterRoleBinding(fmt.Sprintf("project:%s:view", project.Name), append(adminUsers, viewerUsers...), append(adminGroups, viewerGroups...), "view")
	})
})

var _ = Describe("Update projects", func() {
	var project *corev1alpha1.Project
	var namespace string

	BeforeEach(func() {
		project = &corev1alpha1.Project{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-",
			},
		}
		err := cli.Create(ctx, project)
		Expect(err).NotTo(HaveOccurred())
		waitForProjectReady(project)

		namespace = namespacePrefix + project.Name
	})

	It("should update the project namespace as specified", func() {
		adminUsers := []string{"admin-user-1", "admin-user-2"}
		viewerGroups := []string{"viewer-group-1", "viewer-group-2"}

		// update: set some update users
		project.Spec.AdminUsers = adminUsers

		err := cli.Update(ctx, project)
		Expect(err).NotTo(HaveOccurred())
		waitForProjectReady(project)

		ensureRoleBinding(namespace, "project:admin", adminUsers, nil, adminClusterRole)
		ensureNoRoleBinding(namespace, "project:viewer")
		ensureClusterRoleBinding(fmt.Sprintf("project:%s:view", project.Name), adminUsers, nil, "view")

		// update: remove admin users, set some viewer groups
		project.Spec.AdminUsers = nil
		project.Spec.ViewerGroups = viewerGroups

		err = cli.Update(ctx, project)
		Expect(err).NotTo(HaveOccurred())
		waitForProjectReady(project)

		ensureNoRoleBinding(namespace, "project:admin")
		ensureRoleBinding(namespace, "project:viewer", nil, viewerGroups, viewerClusterRole)
		ensureClusterRoleBinding(fmt.Sprintf("project:%s:view", project.Name), nil, viewerGroups, "view")

		// update: remove viewer groups
		project.Spec.ViewerGroups = nil

		err = cli.Update(ctx, project)
		Expect(err).NotTo(HaveOccurred())
		waitForProjectReady(project)

		ensureNoRoleBinding(namespace, "project:admin")
		ensureNoRoleBinding(namespace, "project:viewer")
		ensureNoClusterRoleBinding(fmt.Sprintf("project:%s:view", project.Name))
	})
})

var _ = Describe("Delete projects", func() {
	var project *corev1alpha1.Project
	var namespace string

	BeforeEach(func() {
		project = &corev1alpha1.Project{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-",
			},
		}
		err := cli.Create(ctx, project)
		Expect(err).NotTo(HaveOccurred())
		waitForProjectReady(project)

		namespace = namespacePrefix + project.Name
	})

	It("should delete the project namespace", func() {
		err := cli.Delete(ctx, project)
		Expect(err).NotTo(HaveOccurred())
		waitForProjectGone(project)

		ensureNoNamespace(namespace)
		ensureNoClusterRoleBinding(fmt.Sprintf("project:%s:view", project.Name))
	})
})

func waitForProjectReady(project *corev1alpha1.Project) {
	Eventually(func() error {
		if err := cli.Get(ctx, types.NamespacedName{Name: project.Name}, project); err != nil {
			return err
		}
		if project.Status.ObservedGeneration != project.Generation || project.Status.State != corev1alpha1.StateReady {
			return fmt.Errorf("again")
		}
		return nil
	}, "10s", "100ms").Should(Succeed())
}

func waitForProjectGone(project *corev1alpha1.Project) {
	Eventually(func() error {
		err := cli.Get(ctx, types.NamespacedName{Name: project.Name}, project)
		if client.IgnoreNotFound(err) != nil {
			return err
		}
		if err == nil {
			return fmt.Errorf("again")
		}
		return nil
	}, "10s", "100ms").Should(Succeed())
}

func ensureNamespace(name string, labels map[string]string, annotations map[string]string) {
	namespace := &corev1.Namespace{}
	err := cli.Get(ctx, types.NamespacedName{Name: name}, namespace)
	Expect(err).NotTo(HaveOccurred())
	for key, value := range labels {
		Expect(namespace.Labels).To(HaveKeyWithValue(key, value))
	}
	for key, value := range annotations {
		Expect(namespace.Annotations).To(HaveKeyWithValue(key, value))
	}
}

func ensureRoleBinding(namespace string, name string, users []string, groups []string, clusterRole string) {
	roleBinding := &rbacv1.RoleBinding{}
	err := cli.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, roleBinding)
	Expect(err).NotTo(HaveOccurred())
	var subjects []rbacv1.Subject
	for _, user := range users {
		subjects = append(subjects, rbacv1.Subject{Kind: rbacv1.UserKind, APIGroup: rbacv1.GroupName, Name: user})
	}
	for _, group := range groups {
		subjects = append(subjects, rbacv1.Subject{Kind: rbacv1.GroupKind, APIGroup: rbacv1.GroupName, Name: group})
	}
	Expect(sets.New(roleBinding.Subjects...)).To(Equal(sets.New(subjects...)))
	roleRef := rbacv1.RoleRef{
		Kind:     "ClusterRole",
		APIGroup: rbacv1.GroupName,
		Name:     clusterRole,
	}
	Expect(roleBinding.RoleRef).To(Equal(roleRef))
}

func ensureClusterRoleBinding(name string, users []string, groups []string, clusterRole string) {
	clusterRoleBinding := &rbacv1.ClusterRoleBinding{}
	err := cli.Get(ctx, types.NamespacedName{Name: name}, clusterRoleBinding)
	Expect(err).NotTo(HaveOccurred())
	var subjects []rbacv1.Subject
	for _, user := range users {
		subjects = append(subjects, rbacv1.Subject{Kind: rbacv1.UserKind, APIGroup: rbacv1.GroupName, Name: user})
	}
	for _, group := range groups {
		subjects = append(subjects, rbacv1.Subject{Kind: rbacv1.GroupKind, APIGroup: rbacv1.GroupName, Name: group})
	}
	Expect(sets.New(clusterRoleBinding.Subjects...)).To(Equal(sets.New(subjects...)))
	roleRef := rbacv1.RoleRef{
		Kind:     "ClusterRole",
		APIGroup: rbacv1.GroupName,
		Name:     clusterRole,
	}
	Expect(clusterRoleBinding.RoleRef).To(Equal(roleRef))
}

func ensureNoNamespace(name string) {
	err := cli.Get(ctx, types.NamespacedName{Name: name}, &corev1.Namespace{})
	Expect(client.IgnoreNotFound(err)).NotTo(HaveOccurred())
	Expect(err).To(HaveOccurred())
}

func ensureNoRoleBinding(namespace string, name string) {
	err := cli.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &rbacv1.RoleBinding{})
	Expect(client.IgnoreNotFound(err)).NotTo(HaveOccurred())
	Expect(err).To(HaveOccurred())
}

func ensureNoClusterRoleBinding(name string) {
	err := cli.Get(ctx, types.NamespacedName{Name: name}, &rbacv1.ClusterRoleBinding{})
	Expect(client.IgnoreNotFound(err)).NotTo(HaveOccurred())
	Expect(err).To(HaveOccurred())
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

// create random lowercase character string of given length
func randomString(n int) string {
	charset := []byte("abcdefghijklmnopqrstuvwxyz")
	b := make([]byte, n)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}
