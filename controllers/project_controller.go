/*
SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and project-operator contributors
SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"time"

	"github.com/pkg/errors"
	"github.com/sap/go-generics/slices"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	corev1alpha1 "github.com/sap/project-operator/api/v1alpha1"
	"github.com/sap/project-operator/internal/deployer"
	"github.com/sap/project-operator/internal/metrics"
)

const (
	projectFinalizer = "project-operator.cs.sap.com"
)

// ProjectReconcilerOptions defines options for a ProjectReconciler
type ProjectReconcilerOptions struct {
	NamespacePrefix   string
	AdminClusterRole  string
	ViewerClusterRole string
	EnableClusterView bool
}

// ProjectReconciler reconciles a Project object
type ProjectReconciler struct {
	client   client.Client
	scheme   *runtime.Scheme
	deployer *deployer.Deployer
	options  ProjectReconcilerOptions
}

// NewProjectReconciler creates a ProjectReconciler object
func NewProjectReconciler(client client.Client, scheme *runtime.Scheme, options ProjectReconcilerOptions) *ProjectReconciler {
	managedTypes := []schema.GroupVersionKind{
		mustGVKForObject(&corev1.Namespace{}, scheme),
		mustGVKForObject(&rbacv1.RoleBinding{}, scheme),
		mustGVKForObject(&rbacv1.ClusterRoleBinding{}, scheme),
	}
	return &ProjectReconciler{
		client:   client,
		scheme:   scheme,
		deployer: deployer.New(client, scheme, managedTypes, LabelKeyOwner, AnnotationKeyDigest),
		// TODO: whenever options will contain deep fields or pointers, we should deep-clone it here
		options: options,
	}
}

// +kubebuilder:rbac:groups=core.cs.sap.com,resources=projects,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.cs.sap.com,resources=projects/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.cs.sap.com,resources=projects/finalizers,verbs=update

func (r *ProjectReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	log := log.FromContext(ctx)
	log.V(1).Info("running reconcile")

	now := metav1.Now()

	// fetch reconciled object
	project := &corev1alpha1.Project{}
	if err := r.client.Get(ctx, req.NamespacedName, project); err != nil {
		if apierrors.IsNotFound(err) {
			log.V(1).Info("not found; ignoring")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, errors.Wrap(err, "unexpected get error")
	}

	// convenience accessors
	status := &project.Status

	// always attempt to update the status
	skipStatusUpdate := false
	defer func() {
		if project.DeletionTimestamp.IsZero() || controllerutil.ContainsFinalizer(project, projectFinalizer) {
			if err != nil {
				metrics.ProjectReconcileErrors.WithLabelValues(project.Name).Inc()
			}
			if value, ok := project.Annotations[AnnotationKeyTTL]; ok {
				ttl, parseErr := time.ParseDuration(value)
				if parseErr != nil {
					// this should not happen since the annotation was validated in the validation webhook
					err = utilerrors.NewAggregate([]error{err, parseErr})
				}
				initialSeconds := ttl.Round(time.Second).Seconds()
				remainingSeconds := project.CreationTimestamp.Add(ttl).Sub(now.Time).Round(time.Second).Seconds()
				metrics.ProjectTTLSecondsInitial.WithLabelValues(project.Name).Set(initialSeconds)
				metrics.ProjectTTLSecondsRemaining.WithLabelValues(project.Name).Set(remainingSeconds)
				if remainingSeconds >= 0 {
					metrics.ProjectExpired.WithLabelValues(project.Name).Set(0)
				} else {
					metrics.ProjectExpired.WithLabelValues(project.Name).Set(1)
				}
			} else {
				metrics.ProjectTTLSecondsInitial.WithLabelValues(project.Name).Set(0)
				metrics.ProjectTTLSecondsRemaining.WithLabelValues(project.Name).Set(0)
				metrics.ProjectExpired.WithLabelValues(project.Name).Set(0)
			}
			metrics.ProjectCreationTime.WithLabelValues(project.Name).Set(float64(project.CreationTimestamp.Unix()))
		} else {
			metrics.ProjectReconcileErrors.DeleteLabelValues(project.Name)
			metrics.ProjectTTLSecondsInitial.DeleteLabelValues(project.Name)
			metrics.ProjectTTLSecondsRemaining.DeleteLabelValues(project.Name)
			metrics.ProjectCreationTime.DeleteLabelValues(project.Name)
			metrics.ProjectExpired.DeleteLabelValues(project.Name)
		}
		if skipStatusUpdate {
			return
		}
		status.ObservedGeneration = project.GetGeneration()
		status.LastObservedAt = &now
		if err != nil {
			status.SetReadyCondition(corev1alpha1.ConditionFalse, corev1alpha1.StateError, err.Error())
		}
		if updateErr := r.client.Status().Update(ctx, project); updateErr != nil {
			err = utilerrors.NewAggregate([]error{err, updateErr})
			result = ctrl.Result{}
		}
	}()

	// set a first status (and requeue, because the status update itself will not trigger another reconciliation because of the event filter set)
	if status.ObservedGeneration <= 0 {
		status.SetReadyCondition(corev1alpha1.ConditionUnknown, corev1alpha1.StateProcessing, "First seen")
		return ctrl.Result{Requeue: true}, nil
	}

	// store name of project namespace in status
	status.Namespace = r.options.NamespacePrefix + project.Name

	// do the reconciliation
	if project.GetDeletionTimestamp().IsZero() {
		// create/update case
		if added := controllerutil.AddFinalizer(project, projectFinalizer); added {
			if err := r.client.Update(ctx, project); err != nil {
				return ctrl.Result{}, errors.Wrap(err, "error adding finalizer")
			}
			// trigger another round trip
			// this is necessary because the update call invalidates potential changes done by the post-read hook above
			// in the following round trip, the finalizer will already be there, and the update will not happen again
			return ctrl.Result{Requeue: true}, nil
		}

		dependentObjects, err := r.buildDependentObjects(project)
		if err != nil {
			log.V(1).Info("error while building dependent resources")
			return ctrl.Result{}, errors.Wrap(err, "error building dependent resources")
		}
		ok, err := r.deployer.ReconcileObjects(ctx, dependentObjects, string(project.UID))
		if err != nil {
			log.V(1).Info("error while reconciling dependent resources")
			return ctrl.Result{}, errors.Wrap(err, "error reconciling dependent resources")
		}
		if ok {
			log.V(1).Info("all dependent resources successfully reconciled")
			status.SetReadyCondition(corev1alpha1.ConditionTrue, corev1alpha1.StateReady, "Dependent resources successfully reconciled")
			return ctrl.Result{RequeueAfter: 10 * time.Minute}, nil
		} else {
			log.V(1).Info("not all dependent resources successfully reconciled")
			status.SetReadyCondition(corev1alpha1.ConditionUnknown, corev1alpha1.StateProcessing, "Reconcilation of dependent resources triggered; waiting until all dependent resources are ready")
			return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
		}
	} else if len(slices.Remove(project.GetFinalizers(), projectFinalizer)) > 0 {
		// deletion is blocked because of foreign finalizers
		log.V(1).Info("deleted blocked due to existence of foreign finalizers")
		status.SetReadyCondition(corev1alpha1.ConditionUnknown, corev1alpha1.StateDeletionBlocked, "Deletion blocked due to existing foreign finalizers")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	} else {
		// deletion case
		ok, err := r.deployer.ReconcileObjects(ctx, nil, string(project.UID))
		if err != nil {
			log.V(1).Info("error while deleting dependent resources")
			return ctrl.Result{}, errors.Wrap(err, "error deleting dependent resources")
		}
		if ok {
			// all dependent resources are already gone, so that's it
			log.V(1).Info("all dependent resources are successfully deleted; removing finalizer")
			if removed := controllerutil.RemoveFinalizer(project, projectFinalizer); removed {
				if err := r.client.Update(ctx, project); err != nil {
					return ctrl.Result{}, errors.Wrap(err, "error removing finalizer")
				}
			}
			// skip status update, since the instance will anyway deleted timely by the API server
			// this will avoid unnecessary ugly 409'ish error messages in the logs
			// (occurring in the case that API server would delete the resource in the course of the subsequent reconciliation)
			skipStatusUpdate = true
			return ctrl.Result{}, nil
		} else {
			// deletion triggered for dependent resources, but some are not yet gone
			log.V(1).Info("not all dependent resources are successfully deleted")
			status.SetReadyCondition(corev1alpha1.ConditionUnknown, corev1alpha1.StateDeleting, "Deletion of dependent resources triggered; waiting until dependent resources are deleted")
			return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
		}
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *ProjectReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1alpha1.Project{}).
		WithEventFilter(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.AnnotationChangedPredicate{})).
		Complete(r)
}

func (r *ProjectReconciler) buildDependentObjects(project *corev1alpha1.Project) ([]client.Object, error) {
	var objects []client.Object
	var allSubjects []rbacv1.Subject

	// TODO: should we set project.Spec.Labels and project.Spec.Annotations on all cluster-scope resources ?
	// or just on namespaces (as currently); in that case, shouldn't the fields be better called
	// .Spec.NamespaceLabels and .Spec.NamespaceAnnotations ?

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:        r.options.NamespacePrefix + project.Name,
			Labels:      project.Spec.Labels,
			Annotations: project.Spec.Annotations,
		},
	}
	objects = append(objects, namespace)

	if len(project.Spec.AdminUsers) > 0 || len(project.Spec.AdminGroups) > 0 {
		var subjects []rbacv1.Subject
		for _, user := range project.Spec.AdminUsers {
			subject := rbacv1.Subject{
				Kind: rbacv1.UserKind,
				Name: user,
			}
			subjects = append(subjects, subject)
		}
		for _, group := range project.Spec.AdminGroups {
			subject := rbacv1.Subject{
				Kind: rbacv1.GroupKind,
				Name: group,
			}
			subjects = append(subjects, subject)
		}
		rolebinding := &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "project:admin",
				Namespace: namespace.Name,
			},
			Subjects: subjects,
			RoleRef: rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "ClusterRole",
				Name:     r.options.AdminClusterRole,
			},
		}
		objects = append(objects, rolebinding)
		allSubjects = append(allSubjects, subjects...)
	}

	if len(project.Spec.ViewerUsers) > 0 || len(project.Spec.ViewerGroups) > 0 {
		var subjects []rbacv1.Subject
		for _, user := range project.Spec.ViewerUsers {
			subject := rbacv1.Subject{
				Kind: rbacv1.UserKind,
				Name: user,
			}
			subjects = append(subjects, subject)
		}
		for _, group := range project.Spec.ViewerGroups {
			subject := rbacv1.Subject{
				Kind: rbacv1.GroupKind,
				Name: group,
			}
			subjects = append(subjects, subject)
		}
		rolebinding := &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "project:viewer",
				Namespace: namespace.Name,
			},
			Subjects: subjects,
			RoleRef: rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "ClusterRole",
				Name:     r.options.ViewerClusterRole,
			},
		}
		objects = append(objects, rolebinding)
		allSubjects = append(allSubjects, subjects...)
	}

	if r.options.EnableClusterView && len(allSubjects) > 0 {
		clusterrolebinding := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: "project:" + project.Name + ":view",
			},
			Subjects: slices.Uniq(allSubjects),
			RoleRef: rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "ClusterRole",
				Name:     "view",
			},
		}
		objects = append(objects, clusterrolebinding)
	}

	return objects, nil
}
